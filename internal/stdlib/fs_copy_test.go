package stdlib

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/filesystem"
)

func newCopyFileTestHost(t *testing.T, cfg filesystem.Config) *fsHost {
	t.Helper()
	backend, err := filesystem.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return &fsHost{backend: backend}
}

func TestFSHostCopyFileContentOverwriteAndCreateMode(t *testing.T) {
	cwd := t.TempDir()
	source := filepath.Join(cwd, "source")
	destination := filepath.Join(cwd, "destination")
	if err := os.WriteFile(source, []byte("replacement contents"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("old contents"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "mode-reference"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	host := newCopyFileTestHost(t, filesystem.Config{Cwd: cwd})
	if err := host.copyFile("source", "destination"); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(destination); err != nil || string(got) != "replacement contents" {
		t.Fatalf("overwritten contents = %q, %v", got, err)
	}
	if info, err := os.Stat(destination); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("overwritten mode = %v, %v; want existing mode 0600", info, err)
	}

	if err := host.copyFile("source", "created"); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(cwd, "created")); err != nil || string(got) != "replacement contents" {
		t.Fatalf("created contents = %q, %v", got, err)
	}
	referenceInfo, err := os.Stat(filepath.Join(cwd, "mode-reference"))
	if err != nil {
		t.Fatal(err)
	}
	createdInfo, err := os.Stat(filepath.Join(cwd, "created"))
	if err != nil {
		t.Fatal(err)
	}
	if createdInfo.Mode().Perm() != referenceInfo.Mode().Perm() {
		t.Fatalf("created mode = %04o; os.WriteFile mode under current umask = %04o", createdInfo.Mode().Perm(), referenceInfo.Mode().Perm())
	}
}

func TestFSHostCopyFileSpecialTarget(t *testing.T) {
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "source"), []byte("discarded by null device"), 0o644); err != nil {
		t.Fatal(err)
	}
	target, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Skipf("null device is not writable: %v", err)
	}
	if err := target.Close(); err != nil {
		t.Fatal(err)
	}

	host := newCopyFileTestHost(t, filesystem.Config{Cwd: cwd})
	if err := host.copyFile("source", os.DevNull); err != nil {
		t.Fatalf("copy to %s: %v", os.DevNull, err)
	}
}

func TestFSHostCopyFileSameFileAliasesPreserveContents(t *testing.T) {
	for _, alias := range []string{"same path", "hard link", "symbolic link"} {
		t.Run(alias, func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "source")
			if err := os.WriteFile(source, []byte("must remain intact"), 0o600); err != nil {
				t.Fatal(err)
			}
			destination := "/source"
			switch alias {
			case "hard link":
				if err := os.Link(source, filepath.Join(root, "alias")); err != nil {
					t.Skipf("hard links unavailable: %v", err)
				}
				destination = "/alias"
			case "symbolic link":
				if err := os.Symlink("source", filepath.Join(root, "alias")); err != nil {
					t.Skipf("symbolic links unavailable: %v", err)
				}
				destination = "/alias"
			}

			host := newCopyFileTestHost(t, filesystem.Config{Root: root})
			if err := host.copyFile("/source", destination); err != nil {
				t.Fatal(err)
			}
			if got, err := os.ReadFile(source); err != nil || string(got) != "must remain intact" {
				t.Fatalf("source after alias copy = %q, %v", got, err)
			}
		})
	}
}

func TestFSHostCopyFileSourceFailuresPreserveDestination(t *testing.T) {
	for _, sourceKind := range []string{"missing", "directory"} {
		t.Run(sourceKind, func(t *testing.T) {
			root := t.TempDir()
			destination := filepath.Join(root, "destination")
			if err := os.WriteFile(destination, []byte("keep this"), 0o600); err != nil {
				t.Fatal(err)
			}
			if sourceKind == "directory" {
				if err := os.Mkdir(filepath.Join(root, "source"), 0o755); err != nil {
					t.Fatal(err)
				}
			}

			host := newCopyFileTestHost(t, filesystem.Config{Root: root})
			err := host.copyFile("/source", "/destination")
			if err == nil {
				t.Fatal("copy unexpectedly succeeded")
			}
			if sourceKind == "directory" && filesystemErrorCode(err) != "EISDIR" {
				t.Errorf("directory error code = %q, want EISDIR (%v)", filesystemErrorCode(err), err)
			}
			if got, readErr := os.ReadFile(destination); readErr != nil || string(got) != "keep this" {
				t.Fatalf("destination after source failure = %q, %v", got, readErr)
			}
		})
	}
}

func TestFSHostCopyFileRootCwdAndReadOnly(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	nested := filepath.Join(root, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "source"), []byte("scoped"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(base, "outside")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}

	host := newCopyFileTestHost(t, filesystem.Config{Root: root, Cwd: "/nested"})
	if err := host.copyFile("source", "copy"); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(nested, "copy")); err != nil || string(got) != "scoped" {
		t.Fatalf("cwd-relative copy = %q, %v", got, err)
	}
	if err := host.copyFile("source", "../../outside"); err == nil {
		t.Fatal("copy outside root unexpectedly succeeded")
	}
	if got, err := os.ReadFile(outside); err != nil || string(got) != "outside" {
		t.Fatalf("outside file after refused copy = %q, %v", got, err)
	}

	if err := os.WriteFile(filepath.Join(nested, "readonly-target"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	readOnlyHost := newCopyFileTestHost(t, filesystem.Config{Root: root, Cwd: "/nested", ReadOnly: true})
	err := readOnlyHost.copyFile("source", "readonly-target")
	if !errors.Is(err, filesystem.ErrReadOnly) {
		t.Fatalf("read-only copy error = %v; want filesystem.ErrReadOnly", err)
	}
	if got, err := os.ReadFile(filepath.Join(nested, "readonly-target")); err != nil || string(got) != "keep" {
		t.Fatalf("read-only target after refused copy = %q, %v", got, err)
	}
}

func TestCopyFileContentsAllocationBoundedForLargeFile(t *testing.T) {
	const size = int64(64 << 20)
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "large-source")
	source, err := os.Create(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Truncate(size); err != nil {
		_ = source.Close()
		t.Fatal(err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}

	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	if err := copyFileContents(sourcePath, filepath.Join(dir, "large-copy")); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	allocated := after.TotalAlloc - before.TotalAlloc
	if allocated >= uint64(size/8) {
		t.Fatalf("copy allocated %d bytes for a %d-byte file; want bounded allocation below %d bytes", allocated, size, size/8)
	}
	t.Logf("copy allocated %d bytes for a %d-byte file", allocated, size)
	info, err := os.Stat(filepath.Join(dir, "large-copy"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != size {
		t.Fatalf("copied size = %d; want %d", info.Size(), size)
	}
}

func TestFSHostCopyFileSyncAndPromiseEntrypoints(t *testing.T) {
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "source"), []byte("entrypoint copy"), 0o644); err != nil {
		t.Fatal(err)
	}
	rt, _, loop := fileRuntime(t, cwd)
	evalFiles(t, rt, loop, `
		import {copyFile, copyFileSync, readFileSync} from "noe:fs";
		await copyFile("source", "async-copy");
		copyFileSync("source", "sync-copy");
		if (readFileSync("async-copy", "utf8") !== "entrypoint copy" ||
		    readFileSync("sync-copy", "utf8") !== "entrypoint copy") {
			throw Error("copy entrypoint contents differ");
		}
	`)
}
