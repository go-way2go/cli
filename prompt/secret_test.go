package prompt

// Tests for ReadSecret (secret.go). This file lives in package prompt
// (not prompt_test, unlike prompt_test.go) specifically so it can override
// the unexported isTerminal/readPassword seams, the same technique
// way2go/file uses internally (openFile/removeFile) to make otherwise
// untestable syscall-adjacent behavior deterministic.
//
// # Why the actual terminal echo-suppression syscall itself is not exercised
//
// term.ReadPassword's real behavior — disabling echo via an ioctl against a
// genuine terminal file descriptor, then restoring it — requires a real or
// pseudo terminal to observe. Faking one (for example with
// github.com/creack/pty) is more machinery than this package's other tests
// use anywhere else, and would only prove that golang.org/x/term itself
// works, which is not this package's code to test. Instead, the
// isTerminal/readPassword seams below let every piece of ReadSecret's OWN
// logic that sits around that syscall — which branch runs, the retry loop,
// cooperative cancellation, label writing, and the fact that a successful
// or failed attempt never leaves ReadSecret itself holding any residual
// state — be exercised deterministically and headlessly. The one thing NOT
// covered by an automated test here is "does term.ReadPassword actually
// suppress echo on a real terminal", which is exercised manually and is,
// by design, entirely
// golang.org/x/term's own tested responsibility, not this package's.
//
// The non-terminal fallback path (the isTerminal-false branch) additionally
// gets its own direct coverage against a real injected io.Reader via
// input.NewContext, mirroring prompt_test.go's established style exactly —
// that branch needs no seam at all, since it already goes through the
// ordinary, already-tested input.ReadLine(ctx) path.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/go-way2go/cli/internal/input"
	"github.com/go-way2go/cli/internal/output"
)

// withSeams temporarily overrides isTerminal and readPassword for the
// duration of one test, restoring the real golang.org/x/term-backed
// implementations afterward via t.Cleanup.
func withSeams(t *testing.T, terminal bool, password func() (string, error)) {
	t.Helper()
	prevIsTerminal := isTerminal
	prevReadPassword := readPassword
	isTerminal = func(context.Context) bool { return terminal }
	readPassword = password
	t.Cleanup(func() {
		isTerminal = prevIsTerminal
		readPassword = prevReadPassword
	})
}

// newSecretCtx builds a context carrying independent stdout/stderr buffers
// and, optionally, ctx-bound input data (used only by the non-terminal
// fallback path).
func newSecretCtx(data string) (ctx context.Context, out, errOut *bytes.Buffer) {
	out = &bytes.Buffer{}
	errOut = &bytes.Buffer{}
	ctx = output.NewContext(context.Background(), out, errOut)
	ctx = input.NewContext(ctx, strings.NewReader(data))
	return ctx, out, errOut
}

const secretLabel = "secret: "

func parseSecretInt(s string) (int, error) {
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}
	return v, nil
}

// --- Terminal-path tests (isTerminal seam forced true) ---

func TestReadSecretTerminalSuccess(t *testing.T) {
	calls := 0
	withSeams(t, true, func() (string, error) {
		calls++
		return "42", nil
	})
	ctx, out, errOut := newSecretCtx("")

	got, err := ReadSecret(ctx, secretLabel, parseSecretInt)
	if err != nil {
		t.Fatalf("ReadSecret returned err = %v, want nil", err)
	}
	if got != 42 {
		t.Fatalf("ReadSecret returned %d, want 42", got)
	}
	if calls != 1 {
		t.Fatalf("readPassword called %d times, want 1", calls)
	}
	if !strings.Contains(errOut.String(), secretLabel) {
		t.Fatalf("stderr = %q, want it to contain label %q", errOut.String(), secretLabel)
	}
	if out.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", out.String())
	}
}

func TestReadSecretTerminalRetriesOnInvalidThenSucceeds(t *testing.T) {
	attempts := []string{"not-a-number", "7"}
	i := 0
	withSeams(t, true, func() (string, error) {
		v := attempts[i]
		i++
		return v, nil
	})
	ctx, _, errOut := newSecretCtx("")

	got, err := ReadSecret(ctx, secretLabel, parseSecretInt, RetryInvalid[int]())
	if err != nil {
		t.Fatalf("ReadSecret returned err = %v, want nil", err)
	}
	if got != 7 {
		t.Fatalf("ReadSecret returned %d, want 7", got)
	}
	if i != 2 {
		t.Fatalf("readPassword called %d times, want 2 (initial invalid attempt + one retry)", i)
	}
	if n := strings.Count(errOut.String(), secretLabel); n != 2 {
		t.Fatalf("label written %d times, want 2", n)
	}
	// Note: this test deliberately does not assert that the invalid
	// attempt's raw text never appears on stderr -- parseSecretInt uses
	// strconv.Atoi, whose own error message quotes its input ("strconv.Atoi:
	// parsing \"not-a-number\": invalid syntax"), which is expected,
	// standard behavior for a plain int and not a secret-leak concern.
	// Callers must audit their own secret parsers to prove their specific
	// error paths never echo submitted secrets.
}

func TestReadSecretTerminalNoRetryReturnsInvalidImmediately(t *testing.T) {
	withSeams(t, true, func() (string, error) {
		return "not-a-number", nil
	})
	ctx, _, errOut := newSecretCtx("")

	_, err := ReadSecret(ctx, secretLabel, parseSecretInt)
	if err == nil {
		t.Fatalf("expected a parse error, got nil")
	}
	var invalid *InvalidInputError
	if !errors.As(err, &invalid) {
		t.Fatalf("error = %T, want *InvalidInputError", err)
	}
	// The error text itself (strconv.Atoi's message) is allowed to quote
	// the offending value -- that is standard, expected parser-error
	// behavior for a plain int, not a secret. What must never happen is the
	// *label* or any unrelated leakage; this assertion documents the
	// boundary precisely so a future secret parser is not modeled on an
	// int's own error-quoting convention. Callers must test their parsers to
	// prove they never echo submitted secrets in an error.
	if n := strings.Count(errOut.String(), secretLabel); n != 1 {
		t.Fatalf("label written %d times, want exactly 1 (no retry loop should have run)", n)
	}
}

func TestReadSecretTerminalReadFailureReturnsImmediately(t *testing.T) {
	sentinel := errors.New("boom")
	withSeams(t, true, func() (string, error) {
		return "", sentinel
	})
	ctx, _, _ := newSecretCtx("")

	_, err := ReadSecret(ctx, secretLabel, parseSecretInt, RetryInvalid[int]())
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want %v", err, sentinel)
	}
}

// TestReadSecretTerminalCancelledBeforeAttemptReturnsPromptly proves the
// cooperative pre-check: an already-cancelled ctx never writes a prompt line
// and never calls readPassword at all, matching Read's own documented
// belt-and-suspenders check and this package's existing
// TestReadReturnsPromptlyOnAlreadyCancelledContext.
func TestReadSecretTerminalCancelledBeforeAttemptReturnsPromptly(t *testing.T) {
	calls := 0
	withSeams(t, true, func() (string, error) {
		calls++
		return "42", nil
	})
	base, cancel := context.WithCancel(context.Background())
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	ctx := output.NewContext(base, out, errOut)
	cancel()

	_, err := ReadSecret(ctx, secretLabel, parseSecretInt)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if calls != 0 {
		t.Fatalf("readPassword called %d times, want 0: an already-cancelled context must never touch the terminal", calls)
	}
	if errOut.Len() != 0 {
		t.Fatalf("stderr = %q, want empty (no prompt should be written for an already-cancelled context)", errOut.String())
	}
}

// TestReadSecretTerminalCancelledBetweenRetriesStopsPromptly proves
// cancellation is observed at the next cooperative check between retry
// attempts, without requiring readPassword itself to be preemptible mid-call
// -- exactly the documented behavior in ReadSecret's own doc comment.
func TestReadSecretTerminalCancelledBetweenRetriesStopsPromptly(t *testing.T) {
	base, cancel := context.WithCancel(context.Background())
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	ctx := output.NewContext(base, out, errOut)

	calls := 0
	withSeams(t, true, func() (string, error) {
		calls++
		if calls == 1 {
			// First attempt is invalid, triggering a retry; cancel ctx
			// before the retry's cooperative check runs.
			cancel()
			return "not-a-number", nil
		}
		t.Fatalf("readPassword called a second time; cancellation should have stopped the retry loop first")
		return "", nil
	})

	_, err := ReadSecret(ctx, secretLabel, parseSecretInt, RetryInvalid[int]())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if calls != 1 {
		t.Fatalf("readPassword called %d times, want exactly 1", calls)
	}
}

// --- Non-terminal fallback tests (isTerminal seam forced false; real
// input.ReadLine(ctx) path, mirroring prompt_test.go's own established
// style) ---

func TestReadSecretNonTerminalFallbackSuccess(t *testing.T) {
	withSeams(t, false, func() (string, error) {
		t.Fatalf("readPassword must never be called on the non-terminal fallback path")
		return "", nil
	})
	ctx, out, errOut := newSecretCtx("42\n")

	got, err := ReadSecret(ctx, secretLabel, parseSecretInt)
	if err != nil {
		t.Fatalf("ReadSecret returned err = %v, want nil", err)
	}
	if got != 42 {
		t.Fatalf("ReadSecret returned %d, want 42", got)
	}
	if !strings.Contains(errOut.String(), secretLabel) {
		t.Fatalf("stderr = %q, want it to contain label %q", errOut.String(), secretLabel)
	}
	if out.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", out.String())
	}
}

func TestReadSecretNonTerminalFallbackRetriesOnInvalid(t *testing.T) {
	withSeams(t, false, func() (string, error) {
		t.Fatalf("readPassword must never be called on the non-terminal fallback path")
		return "", nil
	})
	ctx, _, errOut := newSecretCtx("not-a-number\n7\n")

	got, err := ReadSecret(ctx, secretLabel, parseSecretInt, RetryInvalid[int]())
	if err != nil {
		t.Fatalf("ReadSecret returned err = %v, want nil", err)
	}
	if got != 7 {
		t.Fatalf("ReadSecret returned %d, want 7", got)
	}
	if n := strings.Count(errOut.String(), secretLabel); n != 2 {
		t.Fatalf("label written %d times, want 2", n)
	}
}

func TestReadSecretNonTerminalFallbackEOFReturnsImmediately(t *testing.T) {
	withSeams(t, false, func() (string, error) {
		t.Fatalf("readPassword must never be called on the non-terminal fallback path")
		return "", nil
	})
	ctx, _, _ := newSecretCtx("")

	_, err := ReadSecret(ctx, secretLabel, parseSecretInt, RetryInvalid[int]())
	if !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want io.EOF", err)
	}
}

func TestReadSecretNonTerminalFallbackCancelledBeforeAttemptReturnsPromptly(t *testing.T) {
	withSeams(t, false, func() (string, error) {
		t.Fatalf("readPassword must never be called on the non-terminal fallback path")
		return "", nil
	})
	base, cancel := context.WithCancel(context.Background())
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	ctx := output.NewContext(base, out, errOut)
	ctx = input.NewContext(ctx, strings.NewReader("unread\n"))
	cancel()

	_, err := ReadSecret(ctx, secretLabel, parseSecretInt)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if errOut.Len() != 0 {
		t.Fatalf("stderr = %q, want empty (no prompt should be written for an already-cancelled context)", errOut.String())
	}
}

// TestReadSecretDefaultSeamsFallBackWithoutMarkInteractive proves the real,
// unoverridden isTerminal seam: without input.MarkInteractive having been
// called on ctx (exactly the situation for every context cli.App.Execute
// builds -- i.e. every test in this whole module, and any other
// programmatic caller), ReadSecret always takes the non-terminal fallback
// path, regardless of whatever the test process's own real os.Stdin happens
// to be. This is the property that keeps a developer's real terminal from
// ever being mistaken for a test's injected input.
func TestReadSecretDefaultSeamsFallBackWithoutMarkInteractive(t *testing.T) {
	ctx, _, errOut := newSecretCtx("42\n")

	got, err := ReadSecret(ctx, secretLabel, parseSecretInt)
	if err != nil {
		t.Fatalf("ReadSecret returned err = %v, want nil", err)
	}
	if got != 42 {
		t.Fatalf("ReadSecret returned %d, want 42", got)
	}
	if !strings.Contains(errOut.String(), secretLabel) {
		t.Fatalf("stderr = %q, want it to contain label %q", errOut.String(), secretLabel)
	}
}
