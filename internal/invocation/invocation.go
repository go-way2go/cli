// Package invocation holds the invocation resources shared by the CLI command
// engine and the public cli facade: the invocation context, its handler
// signature and a handler's outcome. It exists so the engine and the root
// package can both use them without importing each other.
package invocation

import (
	"context"
	"io"

	"github.com/go-way2go/cli/internal/input"
	"github.com/go-way2go/cli/internal/output"
	"github.com/go-way2go/cli/param"
)

// Outcome is the engine-level result of a handler. OK reports success; a
// failed outcome may carry an error to be reported to the user.
type Outcome struct {
	OK  bool
	Err error
}

// Handler receives an invocation context and returns its outcome.
type Handler func(ctx Context) Outcome

// Context carries one command invocation's cancellation, parameters and streams.
// Its zero value has no parameters, exhausted input and discarded output.
type Context struct {
	parent   context.Context
	values   *param.Values
	in       io.Reader
	out, err io.Writer
}

// New binds invocation resources. Nil parents use Background, nil input is
// exhausted, and nil writers discard output. Input buffering is shared with
// input and prompt helpers through Context().
func New(parent context.Context, values *param.Values, in io.Reader, out, err io.Writer) Context {
	if parent == nil {
		parent = context.Background()
	}
	bound := input.NewContext(parent, in)
	if out == nil {
		out = io.Discard
	}
	if err == nil {
		err = io.Discard
	}
	return Context{parent: parent, values: values, in: input.Reader(bound), out: out, err: err}
}

// Context returns the standard context for services and leaf-package helpers.
func (c Context) Context() context.Context {
	parent := c.parent
	if parent == nil {
		parent = context.Background()
	}
	parent = param.NewContext(parent, c.values)
	parent = input.NewContext(parent, c.Stdin())
	return output.NewContext(parent, c.Stdout(), c.Stderr())
}

// WithContext replaces cancellation and caller values while preserving prepared
// parameters and the same streams, including any unread buffered input.
func (c Context) WithContext(parent context.Context) Context {
	if parent == nil {
		parent = context.Background()
	}
	c.parent = parent
	return c
}

// Stdin returns the shared buffered invocation input.
func (c Context) Stdin() io.Reader {
	if c.in == nil {
		return emptyInput{}
	}
	return c.in
}

// Stdout returns the invocation output writer.
func (c Context) Stdout() io.Writer {
	if c.out == nil {
		return io.Discard
	}
	return c.out
}

// Stderr returns the invocation error writer.
func (c Context) Stderr() io.Writer {
	if c.err == nil {
		return io.Discard
	}
	return c.err
}

type emptyInput struct{}

func (emptyInput) Read([]byte) (int, error) { return 0, io.EOF }
