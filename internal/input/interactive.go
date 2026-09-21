package input

import "context"

// interactiveKey is the unexported context key under which MarkInteractive
// records that the calling process's real os.Stdin -- not whatever source
// NewContext bound into ctx -- is a genuine, directly addressable file
// descriptor a caller may read from outside the ReadLine/NewContext
// machinery entirely.
type interactiveKey struct{}

// MarkInteractive returns a copy of parent recording that the real process
// standard input is available for direct, non-relayed access by a caller
// that specifically needs it -- for example way2go/prompt's ReadSecret,
// which must reach a real terminal file descriptor to disable echo, and
// cannot do that through NewContext's bound, possibly-relayed source.
//
// Only cli.App.Run's real-process entry point calls this, from the one
// place where os.Stdin genuinely is the process's own standard input.
// Execute never does, so Interactive(ctx) is false for every context a
// test or other programmatic caller builds by calling Execute directly --
// regardless of whether that process's own real os.Stdin happens to be a
// terminal (for instance, a developer running `go test` from an
// interactive shell). This is deliberate: it is what keeps a direct
// terminal-reading caller from ever mistaking a test's injected input
// source for the real terminal.
func MarkInteractive(parent context.Context) context.Context {
	return context.WithValue(parent, interactiveKey{}, true)
}

// Interactive reports whether ctx was marked by MarkInteractive.
func Interactive(ctx context.Context) bool {
	v, _ := ctx.Value(interactiveKey{}).(bool)
	return v
}
