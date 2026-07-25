package convergence

import (
	"fmt"
	"os"
	"path/filepath"
)

// ReceiptFS is the narrow filesystem seam the receipt writer uses for its
// crash-safe replacement sequence. Tests inject a fake to exercise failure at
// each step (temp write, file sync, rename, directory sync) while production uses
// the os-backed default. Keeping the seam explicit is what lets the pure package
// remain testable without a real disk under failure injection.
type ReceiptFS interface {
	// WriteTemp creates a uniquely named temp file in dir, writes data, and fsyncs
	// the file before returning its path. It never overwrites the destination.
	WriteTemp(dir, pattern string, data []byte) (string, error)
	// Rename atomically replaces newpath with oldpath.
	Rename(oldpath, newpath string) error
	// SyncDir fsyncs the directory so the rename is durable.
	SyncDir(dir string) error
	// ReadFile reads the named file.
	ReadFile(path string) ([]byte, error)
	// Remove deletes the named file, ignoring absence.
	Remove(path string) error
}

// OSReceiptFS is the production os-backed ReceiptFS.
type OSReceiptFS struct{}

var _ ReceiptFS = OSReceiptFS{}

// WriteTemp writes data to a new temp file in dir and fsyncs it.
func (OSReceiptFS) WriteTemp(dir, pattern string, data []byte) (string, error) {
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", fmt.Errorf("create receipt temp file: %w", err)
	}
	path := file.Name()
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("write receipt temp file: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("sync receipt temp file: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("close receipt temp file: %w", err)
	}
	return path, nil
}

// Rename atomically replaces newpath with oldpath.
func (OSReceiptFS) Rename(oldpath, newpath string) error {
	if err := os.Rename(oldpath, newpath); err != nil {
		return fmt.Errorf("rename receipt file: %w", err)
	}
	return nil
}

// SyncDir fsyncs dir so the rename is durable across a crash.
func (OSReceiptFS) SyncDir(dir string) error {
	handle, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open receipt dir: %w", err)
	}
	defer func() { _ = handle.Close() }()
	if err := handle.Sync(); err != nil {
		return fmt.Errorf("sync receipt dir: %w", err)
	}
	return nil
}

// ReadFile reads the named file.
func (OSReceiptFS) ReadFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// Remove deletes the named file, treating absence as success.
func (OSReceiptFS) Remove(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove receipt file: %w", err)
	}
	return nil
}

// ReceiptPath returns the receipt path within a run directory.
func ReceiptPath(runDir string) string {
	return filepath.Join(runDir, ReceiptFileName)
}
