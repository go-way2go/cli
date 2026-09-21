// Package output implements Way2Go's context-bound stdout/stderr sinks for
// CLI handlers. Println, Errorln and friends never write through a
// package-global os.Stdout/os.Stderr variable: they write through whatever
// writers NewContext bound into the context.Context they are called with.
// This is what lets tests inject independent buffers per execution and lets
// concurrent command executions run without cross-contaminating each
// other's output.
//
// This package depends on nothing but the standard library — in particular
// it does not depend on cli, so cli (which does depend on output, to inject
// sinks before invoking a handler) cannot form an import cycle with it.
package output

import (
	"context"
	"fmt"
	"io"
	"os"
)

// sinksKey is the unexported context key under which NewContext stores a
// context's stdout/stderr sinks.
type sinksKey struct{}

type sinks struct {
	out io.Writer
	err io.Writer
}

// NewContext returns a copy of parent that carries out and err as the
// stdout and stderr sinks Println, Printf, Errorln and Errorf write through
// when called with ctx, or any context derived from it. A nil out or err is
// replaced with io.Discard rather than silently falling back to a shared
// writer.
//
// Callers (in v1, the cli package's execution boundary) call NewContext
// once per command execution with writers scoped to that execution, so
// concurrent executions never share a writer.
func NewContext(parent context.Context, out, err io.Writer) context.Context {
	if out == nil {
		out = io.Discard
	}
	if err == nil {
		err = io.Discard
	}
	return context.WithValue(parent, sinksKey{}, sinks{out: out, err: err})
}

// writers returns the stdout/stderr sinks bound to ctx by NewContext. A
// context that carries no sinks — e.g. a handler invoked directly in an ad
// hoc test or script, outside any cli execution boundary — falls back to
// os.Stdout/os.Stderr, read fresh at call time rather than cached in
// package state, so this fallback never becomes a shared, mutable global
// sink either.
func writers(ctx context.Context) (out, err io.Writer) {
	if s, ok := ctx.Value(sinksKey{}).(sinks); ok {
		return s.out, s.err
	}
	return os.Stdout, os.Stderr
}

// Println writes args to ctx's stdout sink, formatted as fmt.Println would.
func Println(ctx context.Context, args ...any) {
	out, _ := writers(ctx)
	fmt.Fprintln(out, args...)
}

// Printf writes a formatted message to ctx's stdout sink, as fmt.Printf
// would.
func Printf(ctx context.Context, format string, args ...any) {
	out, _ := writers(ctx)
	fmt.Fprintf(out, format, args...)
}

// Errorln writes args to ctx's stderr sink, formatted as fmt.Println would.
func Errorln(ctx context.Context, args ...any) {
	_, errW := writers(ctx)
	fmt.Fprintln(errW, args...)
}

// Errorf writes a formatted message to ctx's stderr sink, as fmt.Printf
// would.
func Errorf(ctx context.Context, format string, args ...any) {
	_, errW := writers(ctx)
	fmt.Fprintf(errW, format, args...)
}
