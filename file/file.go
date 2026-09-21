// Package file provides a small set of reusable, race-safe file primitives
// for Way2Go handlers: exclusive, no-overwrite file creation with
// cleanup-on-failure (WriteNew), and its symmetric counterpart, whole-file
// reads (ReadAll). Both take a context.Context but use it only for a single
// cooperative check — ctx.Err() is inspected once, before any I/O begins —
// because the underlying os calls this package wraps cannot be interrupted
// mid-flight once started; this mirrors how the rest of the framework
// treats ctx as advisory/cooperative rather than magically preemptive, and
// deliberately stops short of goroutines or select loops to fake real
// cancellation.
//
// Alongside those operations, this package supplies small, reusable path
// validators. They are intended to improve parameter and prompt feedback,
// not to authorize a later filesystem operation: filesystem state can change
// after validation. In particular, MustNotExist never replaces WriteNew's
// O_EXCL creation as the overwrite-protection authority.
//
// This package deliberately does NOT provide a virtual or context-bound
// filesystem abstraction: no File or FS type, no streaming Reader/Writer,
// no directory listing, automatic parent creation, or application-specific
// naming policy.
//
// Errors are wrapped with generic, domain-neutral vocabulary ("file %q
// already exists", not "output file %q already exists") so callers can
// layer their own context on top with fmt.Errorf("...: %w", err) without
// ending up with doubled or clashing phrasing.
package file

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-way2go/cli/validate"
)

// writableFile is the narrow internal seam WriteNew needs. It deliberately
// is not an exported filesystem abstraction; it only makes full-write and
// cleanup failure behaviour deterministic to test.
type writableFile interface {
	Write([]byte) (int, error)
	Close() error
}

var (
	openFile = func(path string, flag int, perm os.FileMode) (writableFile, error) {
		return os.OpenFile(path, flag, perm)
	}
	removeFile = os.Remove
)

// Exists returns a validator that requires path to exist. It follows symbolic
// links, matching the behaviour of operations that open the path normally.
func Exists() validate.Validator[string] {
	return func(path string) error {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("file %q must exist: %w", path, err)
		}
		return nil
	}
}

// MustNotExist returns a validator that requires path not to exist. It uses
// Lstat so a dangling symbolic link still counts as an occupied path, as it
// does for WriteNew's exclusive creation.
//
// This is feedback only, not overwrite protection. Call WriteNew to create a
// new file safely after this validator has passed.
func MustNotExist() validate.Validator[string] {
	return func(path string) error {
		_, err := os.Lstat(path)
		switch {
		case err == nil:
			return fmt.Errorf("file %q already exists", path)
		case errors.Is(err, os.ErrNotExist):
			return nil
		default:
			return fmt.Errorf("failed to check whether file %q exists: %w", path, err)
		}
	}
}

// RegularFile returns a validator that requires path to name an existing
// regular file. Symbolic links are followed.
func RegularFile() validate.Validator[string] {
	return func(path string) error {
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("failed to inspect file %q: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("file %q must be a regular file", path)
		}
		return nil
	}
}

// Directory returns a validator that requires path to name an existing
// directory. Symbolic links are followed.
func Directory() validate.Validator[string] {
	return func(path string) error {
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("failed to inspect directory %q: %w", path, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("file %q must be a directory", path)
		}
		return nil
	}
}

// ParentExists returns a validator that requires path's parent to exist and
// be a directory. It does not create a parent and does not attempt an early
// writability check: that check is inherently incomplete and the eventual
// operation remains authoritative.
func ParentExists() validate.Validator[string] {
	return func(path string) error {
		parent := filepath.Dir(path)
		info, err := os.Stat(parent)
		if err != nil {
			return fmt.Errorf("parent directory %q for file %q must exist: %w", parent, path, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("parent %q for file %q must be a directory", parent, path)
		}
		return nil
	}
}

// Extension returns a validator that requires path to have extension. The
// extension is compared exactly as filepath.Ext returns it, including its
// leading dot (for example, ".json").
func Extension(extension string) validate.Validator[string] {
	return func(path string) error {
		if got := filepath.Ext(path); got != extension {
			return fmt.Errorf("file %q must have extension %q (got %q)", path, extension, got)
		}
		return nil
	}
}

// Within returns a lexical path-policy validator that requires path to be in
// base or equal to base. It cleans both inputs but does not resolve symbolic
// links or access the filesystem.
func Within(base string) validate.Validator[string] {
	cleanBase := filepath.Clean(base)
	return func(path string) error {
		rel, err := filepath.Rel(cleanBase, filepath.Clean(path))
		if err != nil {
			return fmt.Errorf("cannot compare file %q with base %q: %w", path, base, err)
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("file %q must be within %q", path, base)
		}
		return nil
	}
}

// WriteNew creates a new file at path containing exactly data, and fails if
// a file already exists at path rather than silently overwriting it. The
// file is created and written with O_CREATE|O_EXCL|O_WRONLY, so creation
// and the no-overwrite check happen atomically from the filesystem's point
// of view — no separate Stat-then-Open race window. perm is passed through
// to os.OpenFile as-is; like any call to os.OpenFile, the permissions the
// file actually ends up with are subject to the process umask, which is
// normal os.OpenFile behavior and not something this function works
// around.
//
// If the file cannot be created because it already exists, WriteNew
// returns an error identifying path, wrapping os.ErrExist so callers can
// still detect that case with errors.Is. If the file is created but the
// write or the subsequent close fails, WriteNew removes the partial file
// with os.Remove rather than leaving truncated content behind, and returns
// the original write/close error — the primary failure a caller should
// see and handle — even if the cleanup os.Remove itself also fails; a
// cleanup failure is folded into the returned error via error wrapping so
// it isn't silently lost, but it never displaces the original cause.
//
// ctx is checked once, before any I/O: if ctx.Err() is non-nil, WriteNew
// returns it immediately without touching the filesystem at all.
func WriteNew(ctx context.Context, path string, data []byte, perm os.FileMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	f, err := openFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("file %q already exists: %w", path, err)
		}
		return fmt.Errorf("failed to create file %q: %w", path, err)
	}

	if n, err := f.Write(data); err != nil {
		f.Close()
		return removeAfterFailure(path, fmt.Errorf("failed to write file %q: %w", path, err))
	} else if n != len(data) {
		f.Close()
		return removeAfterFailure(path, fmt.Errorf("failed to write file %q: %w", path, io.ErrShortWrite))
	}
	if err := f.Close(); err != nil {
		return removeAfterFailure(path, fmt.Errorf("failed to close file %q: %w", path, err))
	}
	return nil
}

// removeAfterFailure removes the partially-written file at path following
// writeErr (a write or close failure that has already occurred) and
// returns writeErr, augmented with the removal failure if os.Remove itself
// also fails. writeErr is always the error callers see as the primary
// cause; a cleanup failure is additional information, never a
// replacement.
func removeAfterFailure(path string, writeErr error) error {
	if rmErr := removeFile(path); rmErr != nil {
		return fmt.Errorf("%w (additionally failed to remove partial file %q: %v)", writeErr, path, rmErr)
	}
	return writeErr
}

// ReadAll reads and returns the entire contents of the file at path,
// symmetric with WriteNew. ctx is checked once, before any I/O: if
// ctx.Err() is non-nil, ReadAll returns it immediately without touching
// the filesystem at all. Any failure to read the file is wrapped with an
// error identifying path.
func ReadAll(ctx context.Context, path string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %q: %w", path, err)
	}
	return data, nil
}
