// Package input implements Way2Go's context-bound stdin source for CLI
// handlers. ReadLine never reads through a package-global os.Stdin
// variable: it reads through whatever io.Reader NewContext bound into the
// context.Context it is called with. This is what lets tests inject
// independent readers per execution and lets concurrent command
// executions run without cross-contaminating each other's input, mirroring
// the output package's context-bound-sink pattern exactly.
//
// NewContext wraps the bound source in a single *bufio.Reader once, at
// NewContext time, and stores that one instance in the returned context.
// Every ReadLine(ctx) call (and every call against any context derived from
// it) retrieves that same *bufio.Reader instance rather than constructing a
// fresh one, so a call is free to use bufio.Reader.ReadString, which reads
// and buffers ahead of the delimiter it is looking for, without losing any
// of that read-ahead: whatever call N leaves buffered but unconsumed is
// exactly what call N+1 continues from, because it is the same *bufio.Reader
// object. This lets a handler call ReadLine more than once in sequence
// against the same bound source and see each call pick up where the previous one left
// off, without resorting to a one-byte-at-a-time read loop to avoid
// over-reading.
//
// This package depends on nothing but the standard library — in particular
// it does not depend on cli, so cli (which does depend on input, to inject
// a source before invoking a handler) cannot form an import cycle with it.
package input

import (
	"bufio"
	"context"
	"io"
	"os"
)

// sourceKey is the unexported context key under which NewContext stores a
// context's bound *bufio.Reader.
type sourceKey struct{}

// eofReader is the io.Reader NewContext substitutes for a nil r, so a
// context deliberately bound with no input source reads as immediately
// exhausted rather than falling back to a shared, mutable global source —
// the same reasoning that leads output.NewContext to substitute io.Discard
// for a nil out or err.
type eofReader struct{}

func (eofReader) Read([]byte) (int, error) { return 0, io.EOF }

// NewContext returns a copy of parent that carries a *bufio.Reader wrapping
// r, once, as the input source ReadLine reads through when called with ctx,
// or any context derived from it. A nil r is replaced with a reader that
// reads as already exhausted (io.EOF), rather than silently falling back to
// a shared reader.
//
// Callers (in v1, the cli package's execution boundary) call NewContext
// once per command execution with a reader scoped to that execution, so
// concurrent executions never share an input source, and each execution's
// *bufio.Reader is created exactly once, before any ReadLine call, so that
// every ReadLine call against ctx shares the same buffering state.
func NewContext(parent context.Context, r io.Reader) context.Context {
	if r == nil {
		r = eofReader{}
	}
	if parent == nil {
		parent = context.Background()
	}
	buffered, ok := r.(*bufio.Reader)
	if !ok {
		buffered = bufio.NewReader(r)
	}
	return context.WithValue(parent, sourceKey{}, buffered)
}

// Reader returns the shared buffered input bound by NewContext. Direct reads
// continue from the same buffer used by ReadLine.
func Reader(ctx context.Context) io.Reader { return source(ctx) }

// source returns the *bufio.Reader bound to ctx by NewContext. A context
// that carries no source — e.g. a handler invoked directly in an ad hoc
// test or script, outside any cli execution boundary — falls back to a
// fresh *bufio.Reader wrapping os.Stdin, constructed fresh at call time
// rather than cached in package state, so this fallback never becomes a
// shared, mutable global source. Because it is fresh per call, this
// fallback path does not give successive calls the same buffered-read
// continuity NewContext-bound contexts get; that is fine, since it exists
// for ad hoc/test use outside any cli execution boundary, not for the
// multi-call-buffering-sensitive path real production entry points use
// (those always go through NewContext, via App.Execute/App.Run).
func source(ctx context.Context) *bufio.Reader {
	if r, ok := ctx.Value(sourceKey{}).(*bufio.Reader); ok {
		return r
	}
	return bufio.NewReader(os.Stdin)
}

// ReadLine reads one newline-terminated line from ctx's bound input
// source. The trailing newline, and any immediately preceding carriage
// return, are stripped from the returned string.
//
// Return contract: if the source is exhausted after producing some data
// with no trailing newline, that data is a successful final line — ReadLine
// returns (thatData, nil), not an io.EOF-tagged result. Only a read that
// produces zero bytes before hitting EOF returns ("", io.EOF). A non-EOF
// read error is returned as-is, alongside whatever partial line was read
// before it occurred.
//
// Cancellation: ReadLine performs a cheap, cooperative check — if ctx is
// already cancelled before the read is attempted, it returns ("",
// ctx.Err()) immediately without touching the bound source. That is the
// full extent of ReadLine's cancellation support: once a Read call on the
// bound source is actually in flight, ReadLine cannot preempt it, because
// there is no portable way to interrupt a blocking Read on an arbitrary
// io.Reader in the standard library — not even for a *os.File: closing it
// from another goroutine while a Read is in flight has unspecified effect
// for an inherited standard descriptor such as os.Stdin (Go deliberately
// does not register those with its runtime poller, to avoid mutating
// shared open-file-description flags a parent process or another goroutine
// may depend on), so it cannot be relied on to unblock a pending read
// either. App.Run does not attempt this: on a received signal it force-
// exits the whole process via os.Exit instead of trying to unblock a
// handler's in-flight ReadLine call. cli.Run supplies a private closeable
// relay for standard input and closes it on SIGINT/SIGTERM, so its normal
// terminal path is interruptible; callers of Execute with arbitrary readers
// retain the limitation described here.
func ReadLine(ctx context.Context) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	r := source(ctx)
	line, err := r.ReadString('\n')
	// A cancellation that happened while an arbitrary reader was blocked must
	// win over data or an error it eventually returned: prompt callers must
	// never mistake cancellation for retryable user input.
	if ctxErr := ctx.Err(); ctxErr != nil {
		return "", ctxErr
	}
	if err != nil {
		if err == io.EOF {
			if len(line) == 0 {
				return "", io.EOF
			}
			return trimCR(line), nil
		}
		return trimCR(line), err
	}
	return trimCR(line[:len(line)-1]), nil
}

// trimCR strips a single trailing carriage return, so callers see the same
// line content whether the source used "\n" or "\r\n" line endings.
func trimCR(line string) string {
	if n := len(line); n > 0 && line[n-1] == '\r' {
		line = line[:n-1]
	}
	return line
}
