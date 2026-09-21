package prompt

import (
	"context"
	"os"

	"golang.org/x/term"

	"github.com/go-way2go/cli/internal/input"
	"github.com/go-way2go/cli/internal/output"
	"github.com/go-way2go/cli/validate"
)

// isTerminal and readPassword are package-level seams over golang.org/x/term
// so tests can simulate ReadSecret's interactive-terminal branch (success,
// retry, restore-by-construction, and the observable timing of
// cancellation) deterministically, without a real or pseudo terminal.
// Production code never overrides these. See secret_test.go for why the
// actual OS-level echo-suppression syscall itself is exercised only
// manually/by design, not by this package's automated tests.
var (
	isTerminal = func(ctx context.Context) bool {
		return input.Interactive(ctx) && term.IsTerminal(int(os.Stdin.Fd()))
	}
	readPassword = func() (string, error) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		return string(b), err
	}
)

// ReadSecret is Read's non-echoing counterpart for secret input: mnemonics,
// Recovery Codes, transport keys, and anything else that must never appear
// on the terminal as it is typed. Its parse/validate/retry contract is
// otherwise identical to Read — same Option[T] machinery (Validate,
// RetryInvalid), same InvalidInputError classification — so an existing
// parser function written for Read works unchanged with ReadSecret.
//
// # Terminal vs non-terminal input
//
// ReadSecret's behavior depends on whether the real process standard input —
// not whatever source ctx's NewContext bound, which may be a relayed pipe or
// a test's injected reader — is a genuine, directly addressable terminal:
//
//   - If so, ReadSecret reads with terminal echo disabled, via
//     golang.org/x/term.ReadPassword against os.Stdin's own file
//     descriptor. This deliberately bypasses ctx's bound input source
//     entirely: term.ReadPassword needs a real terminal file descriptor (it
//     issues an ioctl against it to disable echo and then reads raw), and
//     ctx's bound source is never that, even in a real interactive process
//     (see way2go/cli's stdinRelay for why its own relay of the same
//     os.Stdin is demand-driven specifically so it never races this direct
//     read for the same bytes). ReadSecret only ever considers this branch
//     when ctx was itself marked by cli.App.Run —
//     never for a context a test or other programmatic caller built via
//     cli.App.Execute — so a real terminal attached to the process running
//     the tests (for example a developer's shell) can never be mistaken for
//     the input a test intended to inject.
//   - Otherwise — piped input, a test harness, a non-interactive script or
//     CI invocation, or simply a context Execute built — ReadSecret falls
//     back to an ordinary line read from ctx's bound input, exactly like
//     Read. This is NOT equally safe as a real no-echo terminal read:
//     whatever is on the other end of that source (a terminal multiplexer,
//     a script, a test) sees the secret exactly as typed or supplied,
//     unobscured. Non-interactive callers (tests, scripts, piped input) are
//     responsible for not exposing secrets that
//     way — ReadSecret cannot detect or prevent it, only refuse to
//     misrepresent the fallback as equivalent to a real no-echo read.
//
// # Terminal state
//
// Each terminal-path attempt is self-contained: term.ReadPassword disables
// echo and unconditionally restores it via its own internal defer before
// returning, on every attempt, whether that attempt succeeds, fails to
// parse or validate, or ends in a read error. ReadSecret never calls
// term.ReadPassword more than once concurrently, so this per-call guarantee
// covers every attempt a RetryInvalid loop drives.
//
// # Cancellation
//
// Like Read, ReadSecret performs a cooperative ctx.Done() check before
// writing label on every attempt (including the first); an already-done ctx
// returns immediately, before writing a prompt line or touching the
// terminal. Once a terminal-path attempt is actually in flight, though,
// ReadSecret cannot preempt it: there is no portable way to interrupt a
// blocked terminal read, exactly as package input's ReadLine documents for
// arbitrary readers, and unlike App.Run's ordinary ctx-bound prompt path,
// ReadSecret has no closeable relay to fall back on here, because it
// deliberately reads the real file descriptor directly. A ctx cancellation
// that arrives while a terminal read is in flight is therefore observed only
// once that read itself returns — on Enter, EOF/Ctrl-D, or a read error —
// and the next cooperative check runs; term.ReadPassword's own defer has
// already restored terminal echo by then, so the terminal is never left
// echo-less on account of ReadSecret returning ctx.Err(). The non-terminal
// fallback path has no equivalent gap: it shares Read's cancellation
// behavior exactly, through ctx's bound input.
func ReadSecret[T any](
	ctx context.Context,
	label string,
	parse func(string) (T, error),
	options ...Option[T],
) (T, error) {
	var s settings[T]
	for _, opt := range options {
		if opt == nil {
			continue
		}
		opt(&s)
	}

	interactive := isTerminal(ctx)

	var zero T
	for {
		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		default:
		}

		output.Errorln(ctx, label)

		var line string
		var err error
		if interactive {
			line, err = readPassword()
		} else {
			line, err = input.ReadLine(ctx)
		}
		if err != nil {
			return zero, err
		}

		value, err := parse(line)
		if err == nil {
			err = validate.Apply(value, s.validators...)
		}
		if err != nil {
			if s.retryInvalid {
				output.Errorln(ctx, err)
				continue
			}
			return zero, &InvalidInputError{Err: err}
		}

		return value, nil
	}
}
