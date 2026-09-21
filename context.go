package cli

import (
	"context"
	"io"

	"github.com/go-way2go/cli/internal/invocation"
	"github.com/go-way2go/cli/param"
)

// Context carries one command invocation's cancellation, parameters and streams.
// Its zero value has no parameters, exhausted input and discarded output.
type Context struct {
	inv invocation.Context
}

// NewContext binds invocation resources. Nil parents use Background, nil input
// is exhausted, and nil writers discard output. Input buffering is shared with
// prompt helpers through Context().
func NewContext(parent context.Context, values *param.Values, in io.Reader, out, err io.Writer) Context {
	return Context{inv: invocation.New(parent, values, in, out, err)}
}

// Context returns the standard context for services and leaf-package helpers.
func (c Context) Context() context.Context { return c.inv.Context() }

// WithContext replaces cancellation and caller values while preserving prepared
// parameters and the same streams, including any unread buffered input.
func (c Context) WithContext(parent context.Context) Context {
	return Context{inv: c.inv.WithContext(parent)}
}

// Stdin returns the shared buffered invocation input.
func (c Context) Stdin() io.Reader { return c.inv.Stdin() }

// Stdout returns the invocation output writer.
func (c Context) Stdout() io.Writer { return c.inv.Stdout() }

// Stderr returns the invocation error writer.
func (c Context) Stderr() io.Writer { return c.inv.Stderr() }

// Read returns a prepared parameter, or panics with param.UndeclaredReadError
// when the invocation did not declare it.
func Read[T any](c Context, p param.Descriptor[T]) T { return param.Read(c.Context(), p) }
