package cli

import "github.com/go-way2go/cli/internal/invocation"

// HandlerFunc receives typed invocation capabilities and returns a process outcome.
type HandlerFunc func(ctx Context) Outcome

// Outcome is the fixed result a CLI handler returns. The only way to obtain a
// meaningful Outcome is OK, NOK or Error, so a command cannot select an
// arbitrary exit code: exit code mapping is entirely owned by the cli
// execution boundary (see App.Execute). The zero Outcome is not successful and
// is treated as NOK, silently failing with exit code 1.
type Outcome struct {
	ok  bool
	err error
}

// OK reports a successful outcome. App.Execute maps it to exit code 0.
func OK() Outcome { return Outcome{ok: true} }

// NOK reports an ordinary unsuccessful outcome — a handler-detected failure
// that is not a Way2Go programmer error. App.Execute maps it to exit code 1.
// Unlike Error, NOK carries no message: the framework prints nothing on its
// account.
func NOK() Outcome { return Outcome{ok: false} }

// Error reports an unsuccessful outcome carrying a non-nil error. App.Execute
// prints it once to stderr, prefixed with the command's name, using its normal
// error formatting. Ordinary errors map to exit code 1; marked interactive
// input errors map to exit code 2 (see App.Execute).
func Error(err error) Outcome {
	if err == nil {
		panic("cli: Error requires a non-nil error")
	}
	return Outcome{ok: false, err: err}
}

// toEngine adapts a public handler to the engine's handler; nil stays nil.
func toEngine(h HandlerFunc) invocation.Handler {
	if h == nil {
		return nil
	}
	return func(c invocation.Context) invocation.Outcome {
		o := h(Context{inv: c})
		return invocation.Outcome{OK: o.ok, Err: o.err}
	}
}

// fromEngine adapts an engine handler to a public handler; nil stays nil.
func fromEngine(h invocation.Handler) HandlerFunc {
	if h == nil {
		return nil
	}
	return func(c Context) Outcome {
		o := h(c.inv)
		return Outcome{ok: o.OK, err: o.Err}
	}
}
