package file

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type shortWriteFile struct{ *os.File }

func (f shortWriteFile) Write(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	_, err := f.File.Write(data[:1])
	return 1, err
}

func TestWriteNewRemovesPartialFileOnShortWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "partial.txt")

	originalOpen, originalRemove := openFile, removeFile
	t.Cleanup(func() {
		openFile, removeFile = originalOpen, originalRemove
	})
	openFile = func(path string, flag int, perm os.FileMode) (writableFile, error) {
		f, err := os.OpenFile(path, flag, perm)
		if err != nil {
			return nil, err
		}
		return shortWriteFile{f}, nil
	}
	removed := false
	removeFile = func(path string) error {
		removed = true
		return os.Remove(path)
	}

	err := WriteNew(context.Background(), path, []byte("data"), 0o600)
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("WriteNew() error = %v, want io.ErrShortWrite", err)
	}
	if !removed {
		t.Fatal("WriteNew() did not attempt to remove partial file")
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("partial file remains after failed write: stat error = %v", statErr)
	}
}
