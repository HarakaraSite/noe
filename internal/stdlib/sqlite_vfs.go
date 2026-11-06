package stdlib

import (
	"errors"
	"io"
	"net/url"
	"path/filepath"
	"strings"
	"sync"

	"forge.harakara.site/littleisland/noe/v2/internal/filesystem"
	sqlite3 "github.com/ncruces/go-sqlite3"
	"github.com/ncruces/go-sqlite3/util/vfsutil"
	"github.com/ncruces/go-sqlite3/vfs"
)

type sqliteSandboxVFS struct {
	backend *filesystem.Backend
	base    vfs.VFS
}

func newSQLiteSandboxVFS(backend *filesystem.Backend) *sqliteSandboxVFS {
	return &sqliteSandboxVFS{backend: backend, base: vfs.Find("")}
}

func registerSQLiteVFS(name string, wrapper vfs.VFS) { vfs.Register(name, wrapper) }
func unregisterSQLiteVFS(name string)                { vfs.Unregister(name) }

func (s *sqliteSandboxVFS) Open(name string, flags vfs.OpenFlag) (vfs.File, vfs.OpenFlag, error) {
	if sqliteTemporaryFile(name, flags) {
		return newSQLiteMemoryFile(), flags | vfs.OPEN_MEMORY, nil
	}
	return nil, flags, sqlite3.CANTOPEN
}

func (s *sqliteSandboxVFS) OpenFilename(name *vfs.Filename, flags vfs.OpenFlag) (vfs.File, vfs.OpenFlag, error) {
	if sqliteTemporaryFile(filenameString(name), flags) || name != nil && name.String() == ":memory:" {
		return newSQLiteMemoryFile(), flags | vfs.OPEN_MEMORY, nil
	}
	if s.backend == nil {
		return nil, flags, sqlite3.CANTOPEN
	}
	physical, err := s.validatePhysical(filenameString(name))
	if err != nil {
		return nil, flags, sqlite3.CANTOPEN
	}
	if flags&(vfs.OPEN_READWRITE|vfs.OPEN_CREATE) != 0 {
		if err := s.backend.CheckWritable(); err != nil {
			return nil, flags, sqlite3.READONLY
		}
	}
	if flags&vfs.OPEN_MAIN_DB != 0 {
		// The default OS VFS opens -shm from the main file object rather than
		// through VFS.Open. Validate all SQLite-managed siblings before it is
		// allowed to create a base file that retains those paths.
		for _, suffix := range []string{"-journal", "-wal", "-shm"} {
			if _, err := s.validatePhysical(physical + suffix); err != nil {
				return nil, flags, sqlite3.CANTOPEN
			}
		}
	}
	file, outFlags, err := vfsutil.WrapOpenFilename(s.base, name, flags)
	if err != nil {
		return nil, outFlags, err
	}
	if s.backend.CheckWritable() != nil {
		file = sqliteFileWithoutSharedMemory(file)
	}
	return file, outFlags, nil
}

func (s *sqliteSandboxVFS) Delete(name string, syncDir bool) error {
	if s.backend == nil {
		return sqlite3.CANTOPEN
	}
	if err := s.backend.CheckWritable(); err != nil {
		return sqlite3.READONLY
	}
	physical, err := s.validatePhysical(name)
	if err != nil {
		return sqlite3.CANTOPEN
	}
	return s.base.Delete(physical, syncDir)
}

func (s *sqliteSandboxVFS) Access(name string, flags vfs.AccessFlag) (bool, error) {
	if s.backend == nil {
		return false, sqlite3.CANTOPEN
	}
	physical, err := s.validatePhysical(name)
	if err != nil {
		return false, err
	}
	return s.base.Access(physical, flags)
}

func (s *sqliteSandboxVFS) FullPathname(name string) (string, error) {
	if name == ":memory:" {
		return name, nil
	}
	if s.backend == nil || name == "" {
		return "", sqlite3.CANTOPEN
	}
	return s.backend.Resolve(name, true)
}

func (s *sqliteSandboxVFS) validatePhysical(path string) (string, error) {
	if s.backend == nil || path == "" {
		return "", sqlite3.CANTOPEN
	}
	clean := filepath.Clean(path)
	root := s.backend.Root()
	if root == "" {
		return s.backend.Resolve(clean, true)
	}
	rel, err := filepath.Rel(root, clean)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", sqlite3.CANTOPEN
	}
	virtual := "/" + filepath.ToSlash(rel)
	if rel == "." {
		virtual = "/"
	}
	return s.backend.Resolve(virtual, true)
}

func sqliteDatabaseURI(path, vfsName string) string {
	if path == ":memory:" {
		return "file::memory:?vfs=" + url.QueryEscape(vfsName)
	}
	// Encode the filename as one URI path component. SQLite decodes percent
	// escapes before calling VFS.FullPathname, while URI delimiters from the
	// caller remain ordinary path characters.
	return "file:" + url.PathEscape(path) + "?vfs=" + url.QueryEscape(vfsName)
}

func filenameString(name *vfs.Filename) string {
	if name == nil {
		return ""
	}
	return name.String()
}

func sqliteTemporaryFile(name string, flags vfs.OpenFlag) bool {
	if name == "" || flags&vfs.OPEN_DELETEONCLOSE != 0 {
		return true
	}
	return flags&(vfs.OPEN_TEMP_DB|vfs.OPEN_TRANSIENT_DB|vfs.OPEN_TEMP_JOURNAL|vfs.OPEN_SUBJOURNAL|vfs.OPEN_SUPER_JOURNAL) != 0
}

// sqliteMemoryFile is used for SQLite temporary files. It intentionally has
// no path and grants locking only within the owning connection.
type sqliteMemoryFile struct {
	mu   sync.RWMutex
	data []byte
	lock vfs.LockLevel
}

func newSQLiteMemoryFile() *sqliteMemoryFile { return &sqliteMemoryFile{} }

func (f *sqliteMemoryFile) ReadAt(dst []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errors.New("negative SQLite memory file offset")
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	if off >= int64(len(f.data)) {
		return 0, io.EOF
	}
	n := copy(dst, f.data[off:])
	if n != len(dst) {
		return n, io.EOF
	}
	return n, nil
}

func (f *sqliteMemoryFile) WriteAt(src []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errors.New("negative SQLite memory file offset")
	}
	if int64(len(src)) > int64(int(^uint(0)>>1))-off {
		return 0, sqlite3.FULL
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	end := int(off) + len(src)
	if end > len(f.data) {
		f.data = append(f.data, make([]byte, end-len(f.data))...)
	}
	copy(f.data[int(off):end], src)
	return len(src), nil
}

func (f *sqliteMemoryFile) Truncate(size int64) error {
	if size < 0 || size > int64(int(^uint(0)>>1)) {
		return sqlite3.FULL
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if int(size) < len(f.data) {
		f.data = f.data[:int(size)]
	} else if int(size) > len(f.data) {
		f.data = append(f.data, make([]byte, int(size)-len(f.data))...)
	}
	return nil
}

func (f *sqliteMemoryFile) Sync(vfs.SyncFlag) error { return nil }
func (f *sqliteMemoryFile) Size() (int64, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return int64(len(f.data)), nil
}
func (f *sqliteMemoryFile) Close() error { return nil }
func (f *sqliteMemoryFile) Lock(level vfs.LockLevel) error {
	f.mu.Lock()
	if level > f.lock {
		f.lock = level
	}
	f.mu.Unlock()
	return nil
}
func (f *sqliteMemoryFile) Unlock(level vfs.LockLevel) error {
	f.mu.Lock()
	f.lock = level
	f.mu.Unlock()
	return nil
}
func (f *sqliteMemoryFile) CheckReservedLock() (bool, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.lock >= vfs.LOCK_RESERVED, nil
}
func (*sqliteMemoryFile) SectorSize() int { return 4096 }
func (*sqliteMemoryFile) DeviceCharacteristics() vfs.DeviceCharacteristic {
	return vfs.IOCAP_ATOMIC | vfs.IOCAP_SAFE_APPEND | vfs.IOCAP_POWERSAFE_OVERWRITE
}

// sqliteNoSharedMemoryFile retains the default OS file's other VFS features,
// but prevents a read-only filesystem backend from creating or modifying the
// default VFS's WAL -shm sidecar behind the sandbox wrapper.
type sqliteNoSharedMemoryFile struct {
	vfs.File
	base vfs.File
}

func sqliteFileWithoutSharedMemory(base vfs.File) vfs.File {
	return &sqliteNoSharedMemoryFile{File: base, base: base}
}

func (f *sqliteNoSharedMemoryFile) Unwrap() vfs.File             { return f.base }
func (*sqliteNoSharedMemoryFile) SharedMemory() vfs.SharedMemory { return nil }
func (f *sqliteNoSharedMemoryFile) MemoryMapper() vfs.MemoryMapper {
	return vfsutil.WrapMemoryMapper(f.base)
}
func (f *sqliteNoSharedMemoryFile) LockState() vfs.LockLevel { return vfsutil.WrapLockState(f.base) }
func (f *sqliteNoSharedMemoryFile) PersistWAL() bool         { return vfsutil.WrapPersistWAL(f.base) }
func (f *sqliteNoSharedMemoryFile) SetPersistWAL(value bool) {
	vfsutil.WrapSetPersistWAL(f.base, value)
}
func (f *sqliteNoSharedMemoryFile) PowersafeOverwrite() bool {
	return vfsutil.WrapPowersafeOverwrite(f.base)
}
func (f *sqliteNoSharedMemoryFile) SetPowersafeOverwrite(value bool) {
	vfsutil.WrapSetPowersafeOverwrite(f.base, value)
}
func (f *sqliteNoSharedMemoryFile) ChunkSize(size int) { vfsutil.WrapChunkSize(f.base, size) }
func (f *sqliteNoSharedMemoryFile) SizeHint(size int64) error {
	return vfsutil.WrapSizeHint(f.base, size)
}
func (f *sqliteNoSharedMemoryFile) HasMoved() (bool, error) { return vfsutil.WrapHasMoved(f.base) }
func (f *sqliteNoSharedMemoryFile) Overwrite() error        { return vfsutil.WrapOverwrite(f.base) }
func (f *sqliteNoSharedMemoryFile) SyncSuper(path string) error {
	return vfsutil.WrapSyncSuper(f.base, path)
}
func (f *sqliteNoSharedMemoryFile) CommitPhaseTwo() error { return vfsutil.WrapCommitPhaseTwo(f.base) }
func (f *sqliteNoSharedMemoryFile) BeginAtomicWrite() error {
	return vfsutil.WrapBeginAtomicWrite(f.base)
}
func (f *sqliteNoSharedMemoryFile) CommitAtomicWrite() error {
	return vfsutil.WrapCommitAtomicWrite(f.base)
}
func (f *sqliteNoSharedMemoryFile) RollbackAtomicWrite() error {
	return vfsutil.WrapRollbackAtomicWrite(f.base)
}
func (f *sqliteNoSharedMemoryFile) CheckpointStart() { vfsutil.WrapCheckpointStart(f.base) }
func (f *sqliteNoSharedMemoryFile) CheckpointDone()  { vfsutil.WrapCheckpointDone(f.base) }
func (f *sqliteNoSharedMemoryFile) Pragma(name, value string) (string, error) {
	return vfsutil.WrapPragma(f.base, name, value)
}
func (f *sqliteNoSharedMemoryFile) BusyHandler(handler func() bool) {
	vfsutil.WrapBusyHandler(f.base, handler)
}

var _ vfs.VFSFilename = (*sqliteSandboxVFS)(nil)
var _ vfs.File = (*sqliteMemoryFile)(nil)
var _ vfs.FileSharedMemory = (*sqliteNoSharedMemoryFile)(nil)
var _ vfs.FileMemoryMapper = (*sqliteNoSharedMemoryFile)(nil)
