package stdlib

import (
	"errors"
	"io"
	"os"
)

const fsCopyBufferSize = 32 * 1024

// copyFileContents copies between opened files so aliases can be detected
// before the destination is truncated. os.File may use a native copy path;
// io.CopyBuffer's fallback keeps memory use independent of file size.
func copyFileContents(srcPath, dstPath string) (err error) {
	source, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, source.Close()) }()

	sourceInfo, err := source.Stat()
	if err != nil {
		return err
	}
	// Read a bounded prefix before opening the destination. This reports an
	// initial source read error, including a directory read, without changing
	// an existing destination. The same fixed buffer is reused for the copy.
	var buffer [fsCopyBufferSize]byte
	prefixSize, readErr := io.ReadFull(source, buffer[:])
	if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
		return readErr
	}

	destination, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, destination.Close()) }()

	destinationInfo, err := destination.Stat()
	if err != nil {
		return err
	}
	if os.SameFile(sourceInfo, destinationInfo) {
		return nil
	}
	if destinationInfo.Mode().IsRegular() {
		if err := destination.Truncate(0); err != nil {
			return err
		}
	}
	if prefixSize > 0 {
		written, writeErr := destination.Write(buffer[:prefixSize])
		if writeErr != nil {
			return writeErr
		}
		if written != prefixSize {
			return io.ErrShortWrite
		}
	}
	_, err = io.CopyBuffer(destination, source, buffer[:])
	return err
}
