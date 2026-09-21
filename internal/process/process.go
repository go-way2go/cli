// Package process implements the real-process entry point of a CLI: argument
// and standard-stream wiring, the closeable stdin relay and SIGINT/SIGTERM
// handling. It runs a caller-supplied execution callback and does not depend
// on the command engine or the cli root package.
package process

import (
	"context"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/go-way2go/cli/internal/input"
)

// ExecuteFunc runs one command invocation and returns its exit code.
type ExecuteFunc func(ctx context.Context, args []string, in io.Reader, out, err io.Writer) int

// Run is the real-process convenience entry point: it executes execute against
// os.Args[1:] with os.Stdin/os.Stdout/os.Stderr and terminates the process
// with the resulting exit code via os.Exit.
//
// Run watches for os.Interrupt (SIGINT, e.g. Ctrl-C) and SIGTERM with
// signal.Notify on an explicit channel — rather than signal.NotifyContext,
// which does not expose which signal fired — so it can tell the two apart.
// It runs execute in its own goroutine and races that goroutine's
// completion against the signal channel:
//
//   - if execute finishes first, Run exits with its returned code (0, 1 or 2),
//     unchanged;
//   - if a signal arrives first, Run cancels execute's context and closes its
//     active input relay. That interrupts the handler's ctx-bound terminal or
//     pipe read, then Run waits for execute to return before exiting with 130
//     for SIGINT or 143 for SIGTERM.
//
// Execute itself deliberately accepts arbitrary injected readers. A reader
// that cannot be closed cannot in general be interrupted while blocked; it
// still observes cancellation before and after each read. Run avoids that
// limitation for process stdin with an internal, demand-driven input relay
// (see stdinRelay): its read end is always closeable even where closing an
// inherited os.Stdin does not reliably interrupt an in-flight operating-
// system read. A Read call already in flight against the real os.Stdin when
// Close happens leaks until it returns on its own (new input, EOF, or a read
// error) or the process exits, which happens immediately after execute
// returns on a signal, so it cannot outlive Run.
//
// Run also marks ctx as interactive (input.MarkInteractive) before starting
// execute: this is the one real-process entry point where os.Stdin genuinely
// is the process's own standard input, which is what lets prompt.ReadSecret
// decide it is safe to attempt a direct, non-echoing read against the real
// terminal file descriptor instead of ctx's relayed source. See stdinRelay's
// doc comment for why that direct read never races the relay.
func Run(execute ExecuteFunc) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = input.MarkInteractive(ctx)
	in := newStdinRelay()
	defer in.Close()

	done := make(chan int, 1)
	go func() {
		done <- execute(ctx, os.Args[1:], in, os.Stdout, os.Stderr)
	}()

	select {
	case code := <-done:
		os.Exit(code)
	case sig := <-sigCh:
		cancel()
		_ = in.Close()
		<-done
		switch sig {
		case syscall.SIGTERM:
			os.Exit(143)
		default: // os.Interrupt (SIGINT)
			os.Exit(130)
		}
	}
}

// stdinRelay is Run's demand-driven adapter over process stdin. Unlike a
// free-running io.Copy, it issues a Read against the real os.Stdin only the
// instant its own Read method is called, and never reads ahead
// speculatively. This is what lets way2go/prompt's non-echoing secret-input
// path (ReadSecret) safely read os.Stdin directly — via golang.org/x/term,
// which needs the real terminal file descriptor, not ctx's relayed/buffered
// source — in between ordinary ReadLine calls, without racing this relay for
// the same bytes: stdinRelay is guaranteed to be idle, parked on a channel
// receive rather than blocked inside a live os.Stdin.Read call, whenever no
// ReadLine call is in flight, because a CLI handler only ever has one read
// of any kind outstanding at a time. An eagerly free-running relay (the
// previous io.Copy-based implementation) cannot offer that guarantee: it
// always tries to stay one Read ahead of its consumer, so it is typically
// already blocked inside os.Stdin.Read — with no portable way to hand that
// read off — exactly when a secret prompt would need the fd.
//
// Close makes any Read call currently or later blocked on the relay return
// io.ErrClosedPipe immediately, mirroring io.PipeReader's close behavior; it
// does not and cannot interrupt an os.Stdin.Read call already in flight
// inside loop — the same limitation package input's ReadLine documents for
// any arbitrary reader. A Read call already in flight when Close happens
// leaks until it returns on its own (new input, EOF, or a read error) or the
// process exits, exactly as the previous io.Copy-based relay's goroutine
// could leak after a signal.
type stdinRelay struct {
	pull      chan []byte
	result    chan stdinReadResult
	closed    chan struct{}
	closeOnce sync.Once
}

// stdinReadResult carries one os.Stdin.Read call's outcome back to the
// stdinRelay.Read call that requested it.
type stdinReadResult struct {
	n   int
	err error
}

// newStdinRelay starts a stdinRelay's background loop and returns it.
func newStdinRelay() *stdinRelay {
	r := &stdinRelay{
		pull:   make(chan []byte),
		result: make(chan stdinReadResult),
		closed: make(chan struct{}),
	}
	go r.loop()
	return r
}

// loop services pull requests one at a time, each with its own single
// os.Stdin.Read call, and exits once closed is closed — except that a Read
// call already in flight when that happens cannot itself be interrupted;
// see the type doc comment.
func (r *stdinRelay) loop() {
	for {
		select {
		case buf := <-r.pull:
			n, err := os.Stdin.Read(buf)
			select {
			case r.result <- stdinReadResult{n, err}:
			case <-r.closed:
				return
			}
		case <-r.closed:
			return
		}
	}
}

// Read implements io.Reader by forwarding p to the relay loop and returning
// exactly what its one corresponding os.Stdin.Read(p) call produced.
func (r *stdinRelay) Read(p []byte) (int, error) {
	select {
	case r.pull <- p:
	case <-r.closed:
		return 0, io.ErrClosedPipe
	}
	select {
	case res := <-r.result:
		return res.n, res.err
	case <-r.closed:
		return 0, io.ErrClosedPipe
	}
}

// Close makes every blocked or future Read return io.ErrClosedPipe. It is
// safe to call more than once.
func (r *stdinRelay) Close() error {
	r.closeOnce.Do(func() { close(r.closed) })
	return nil
}
