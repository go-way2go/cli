package file_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-way2go/cli/file"
	"github.com/go-way2go/cli/validate"
)

func TestValidatorsUseSharedValidatorType(t *testing.T) {
	var _ validate.Validator[string] = file.Exists()
	var _ validate.Validator[string] = file.MustNotExist()
	var _ validate.Validator[string] = file.RegularFile()
	var _ validate.Validator[string] = file.Directory()
	var _ validate.Validator[string] = file.ParentExists()
	var _ validate.Validator[string] = file.Extension(".txt")
	var _ validate.Validator[string] = file.Within("base")
}

func TestPathValidators(t *testing.T) {
	dir := t.TempDir()
	regular := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(regular, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing.txt")

	for name, validator := range map[string]validate.Validator[string]{
		"exists":        file.Exists(),
		"regular file":  file.RegularFile(),
		"directory":     file.Directory(),
		"parent exists": file.ParentExists(),
		"extension":     file.Extension(".txt"),
		"within":        file.Within(dir),
	} {
		path := regular
		if name == "directory" {
			path = dir
		}
		if err := validator(path); err != nil {
			t.Errorf("%s validator(%q) = %v, want nil", name, path, err)
		}
	}

	if err := file.MustNotExist()(missing); err != nil {
		t.Fatalf("MustNotExist(%q) = %v, want nil", missing, err)
	}
	for name, err := range map[string]error{
		"exists missing":          file.Exists()(missing),
		"must not exist existing": file.MustNotExist()(regular),
		"regular file directory":  file.RegularFile()(dir),
		"directory regular file":  file.Directory()(regular),
		"missing parent":          file.ParentExists()(filepath.Join(dir, "missing", "file.txt")),
		"wrong extension":         file.Extension(".json")(regular),
		"outside base":            file.Within(dir)(filepath.Dir(dir)),
	} {
		if err == nil {
			t.Errorf("%s = nil, want validation error", name)
		}
	}
}

// -- WriteNew -----------------------------------------------------------

func TestWriteNewCreatesFileWithExactContentAndPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.txt")
	want := []byte("hello, way2go")

	// 0o600 is used because it is unaffected by every common umask (022,
	// 002, 077, ...): none of those clear bits that 0o600 doesn't already
	// have set, so the resulting mode is deterministic without needing to
	// save/restore the process umask around the test.
	if err := file.WriteNew(context.Background(), path, want, 0o600); err != nil {
		t.Fatalf("WriteNew() error = %v, want nil", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("file content = %q, want %q", got, want)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("os.Stat() error = %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("file permissions = %v, want %v", perm, os.FileMode(0o600))
	}
}

func TestWriteNewRefusesToOverwriteExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.txt")
	original := []byte("do not touch me")

	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("os.WriteFile() setup error = %v", err)
	}

	err := file.WriteNew(context.Background(), path, []byte("clobbered"), 0o600)
	if err == nil {
		t.Fatalf("WriteNew() error = nil, want an error for an existing file")
	}
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("WriteNew() error = %v, want it to wrap os.ErrExist", err)
	}

	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("os.ReadFile() error = %v", readErr)
	}
	if string(got) != string(original) {
		t.Fatalf("existing file content = %q, want untouched %q", got, original)
	}
}

func TestWriteNewReturnsCtxErrImmediatelyWithoutCreatingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "never-created.txt")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := file.WriteNew(ctx, path, []byte("data"), 0o600)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("WriteNew() error = %v, want context.Canceled", err)
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("file exists after cancelled WriteNew(), want no file created (stat err = %v)", statErr)
	}
}

// -- ReadAll --------------------------------------------------------------

func TestReadAllRoundTripsWriteNewContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "roundtrip.txt")
	want := []byte("round trip me")

	if err := file.WriteNew(context.Background(), path, want, 0o600); err != nil {
		t.Fatalf("WriteNew() error = %v", err)
	}

	got, err := file.ReadAll(context.Background(), path)
	if err != nil {
		t.Fatalf("ReadAll() error = %v, want nil", err)
	}
	if string(got) != string(want) {
		t.Fatalf("ReadAll() = %q, want %q", got, want)
	}
}

func TestReadAllRoundTripsPlainWriteFileContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plain.txt")
	want := []byte("written the boring way")

	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatalf("os.WriteFile() setup error = %v", err)
	}

	got, err := file.ReadAll(context.Background(), path)
	if err != nil {
		t.Fatalf("ReadAll() error = %v, want nil", err)
	}
	if string(got) != string(want) {
		t.Fatalf("ReadAll() = %q, want %q", got, want)
	}
}

func TestReadAllReturnsWrappedErrorForNonexistentPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.txt")

	_, err := file.ReadAll(context.Background(), path)
	if err == nil {
		t.Fatalf("ReadAll() error = nil, want an error for a nonexistent path")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ReadAll() error = %v, want it to wrap os.ErrNotExist", err)
	}
}

func TestReadAllReturnsCtxErrImmediatelyWithoutTouchingFilesystem(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist-either.txt")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	data, err := file.ReadAll(ctx, path)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ReadAll() error = %v, want context.Canceled", err)
	}
	if data != nil {
		t.Fatalf("ReadAll() data = %v, want nil", data)
	}
}
