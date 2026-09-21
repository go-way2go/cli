package cli_test

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/go-way2go/cli"
	"github.com/go-way2go/cli/param"
	"github.com/go-way2go/cli/validate"
)

// Options resolve defaults, parse explicit values and validate before dispatch.
func TestFlagsResolveDefaultAndValidate(t *testing.T) {
	limit := param.Int("", validate.Check(func(v int) error {
		if v < 0 {
			return fmt.Errorf("limit must be >= 0")
		}
		return nil
	}), param.Default(10))

	var got int
	act := cli.Command("list", cli.Define("", func(ctx cli.Context) cli.Outcome {
		got = cli.Read(ctx, limit)
		return cli.OK()
	}, cli.WithOption(limit, "limit")))
	app := cli.New(act)

	var out, errBuf bytes.Buffer
	if code := app.Execute(context.Background(), []string{"list", "--limit=5"}, nil, &out, &errBuf); code != 0 {
		t.Fatalf("Execute(--limit=5) code = %d, want 0; stderr=%s", code, errBuf.String())
	}
	if got != 5 {
		t.Fatalf("got = %d, want 5", got)
	}

	out.Reset()
	errBuf.Reset()
	if code := app.Execute(context.Background(), []string{"list"}, nil, &out, &errBuf); code != 0 {
		t.Fatalf("Execute() (default) code = %d, want 0; stderr=%s", code, errBuf.String())
	}
	if got != 10 {
		t.Fatalf("got = %d, want default 10", got)
	}

	out.Reset()
	errBuf.Reset()
	if code := app.Execute(context.Background(), []string{"list", "--limit=-1"}, nil, &out, &errBuf); code != 2 {
		t.Fatalf("Execute(--limit=-1) code = %d, want 2 (validation failure); stderr=%s", code, errBuf.String())
	}
}

// Boolean options accept bare true and explicit equals-form false.
func TestBoolFlagAcceptsBareForm(t *testing.T) {
	verbose := param.Bool("", param.Default(false))

	var got bool
	act := cli.Command("run", cli.Define("", func(ctx cli.Context) cli.Outcome {
		got = cli.Read(ctx, verbose)
		return cli.OK()
	}, cli.WithOption(verbose, "verbose")))
	app := cli.New(act)

	var out, errBuf bytes.Buffer
	if code := app.Execute(context.Background(), []string{"run", "--verbose"}, nil, &out, &errBuf); code != 0 {
		t.Fatalf("Execute(--verbose) code = %d, want 0; stderr=%s", code, errBuf.String())
	}
	if !got {
		t.Fatal("got = false, want true for bare --verbose")
	}

	out.Reset()
	errBuf.Reset()
	got = true
	if code := app.Execute(context.Background(), []string{"run"}, nil, &out, &errBuf); code != 0 {
		t.Fatalf("Execute() (default) code = %d, want 0; stderr=%s", code, errBuf.String())
	}
	if got {
		t.Fatal("got = true, want default false when --verbose is omitted")
	}

	out.Reset()
	errBuf.Reset()
	if code := app.Execute(context.Background(), []string{"run", "--verbose=false"}, nil, &out, &errBuf); code != 0 {
		t.Fatalf("Execute(--verbose=false) code = %d, want 0; stderr=%s", code, errBuf.String())
	}
	if got {
		t.Fatal("got = true, want false for --verbose=false")
	}
}

// Positional arguments resolve in declaration order.
func TestPositionalArgsResolveInDeclarationOrder(t *testing.T) {
	first := param.String("")
	second := param.String("", param.Default("fallback"))

	var gotFirst, gotSecond string
	act := cli.Command("greet", cli.Define("", func(ctx cli.Context) cli.Outcome {
		gotFirst = cli.Read(ctx, first)
		gotSecond = cli.Read(ctx, second)
		return cli.OK()
	}, cli.WithArgument(first, "first"), cli.WithArgument(second, "second")))
	app := cli.New(act)

	var out, errBuf bytes.Buffer
	if code := app.Execute(context.Background(), []string{"greet", "alice", "bob"}, nil, &out, &errBuf); code != 0 {
		t.Fatalf("code = %d, want 0; stderr=%s", code, errBuf.String())
	}
	if gotFirst != "alice" || gotSecond != "bob" {
		t.Fatalf("got (%q, %q), want (%q, %q)", gotFirst, gotSecond, "alice", "bob")
	}

	out.Reset()
	errBuf.Reset()
	gotFirst, gotSecond = "", ""
	if code := app.Execute(context.Background(), []string{"greet", "alice"}, nil, &out, &errBuf); code != 0 {
		t.Fatalf("code = %d, want 0 (second optional); stderr=%s", code, errBuf.String())
	}
	if gotFirst != "alice" || gotSecond != "fallback" {
		t.Fatalf("got (%q, %q), want (%q, %q)", gotFirst, gotSecond, "alice", "fallback")
	}
}

// Excess positional arguments fail before the handler runs.
func TestExtraPositionalArgumentFails(t *testing.T) {
	one := param.String("")
	act := cli.Command("echo", cli.Define("", func(ctx cli.Context) cli.Outcome { return cli.OK() }, cli.WithArgument(one, "one")))
	app := cli.New(act)

	var out, errBuf bytes.Buffer
	code := app.Execute(context.Background(), []string{"echo", "a", "b"}, nil, &out, &errBuf)
	if code != 2 {
		t.Fatalf("code = %d, want 2 (extra argument); stderr=%s", code, errBuf.String())
	}
}

// Missing required input exits with code 2 before the handler runs.
func TestMissingRequiredFlagExitsTwo(t *testing.T) {
	name := param.String("")
	executed := false
	act := cli.Command("greet", cli.Define("", func(ctx cli.Context) cli.Outcome {
		executed = true
		return cli.OK()
	}, cli.WithOption(name, "name")))
	app := cli.New(act)

	var out, errBuf bytes.Buffer
	code := app.Execute(context.Background(), []string{"greet"}, nil, &out, &errBuf)
	if code != 2 {
		t.Fatalf("code = %d, want 2; stderr=%s", code, errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "--name: value is required") {
		t.Fatalf("stderr = %q, want external label", errBuf.String())
	}
	if executed {
		t.Fatal("handler must not run when a required Param is missing")
	}
}

// An undeclared parameter read is reported as a programmer error with exit code 1.
func TestRecoveredProgrammerErrorExitsOne(t *testing.T) {
	stray := param.String("stray value") // deliberately never bound to this Command
	act := cli.Command("boom", cli.Define("", func(ctx cli.Context) cli.Outcome {
		cli.Read(ctx, stray)
		return cli.OK()
	}))
	app := cli.New(act)

	var out, errBuf bytes.Buffer
	code := app.Execute(context.Background(), []string{"boom"}, nil, &out, &errBuf)
	if code != 1 {
		t.Fatalf("code = %d, want 1 (recovered programmer error); stderr=%s", code, errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "stray") {
		t.Fatalf("stderr = %q, want it to mention the undeclared param", errBuf.String())
	}
}

// Panics unrelated to parameter misuse propagate to the caller.
func TestUnrelatedPanicRepanics(t *testing.T) {
	act := cli.Command("boom", cli.Define("", func(ctx cli.Context) cli.Outcome {
		panic("unrelated failure, not a Way2Go programmer error")
	}))
	app := cli.New(act)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected the unrelated panic to propagate out of Execute")
		}
		if !strings.Contains(fmt.Sprint(r), "unrelated failure") {
			t.Fatalf("recovered value = %v, want it to be the original panic", r)
		}
	}()
	var out, errBuf bytes.Buffer
	app.Execute(context.Background(), []string{"boom"}, nil, &out, &errBuf)
}

// Nested groups dispatch to their leaf command.
func TestNestedGroupsDispatch(t *testing.T) {
	var ran bool
	leaf := cli.Command("run", cli.Define("", func(ctx cli.Context) cli.Outcome {
		ran = true
		return cli.OK()
	}))
	app := cli.New(cli.Group("job", cli.Group("sub", leaf)))

	var out, errBuf bytes.Buffer
	code := app.Execute(context.Background(), []string{"job", "sub", "run"}, nil, &out, &errBuf)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr=%s", code, errBuf.String())
	}
	if !ran {
		t.Fatal("expected the nested leaf Command to run")
	}
}

func TestEmptyGroupNamePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for empty group name")
		}
	}()
	cli.Group("   ")
}

// TestErrorOutcomePrintsOnceWithCommandPrefixAndExitsOne proves that
// cli.Error(err) maps to the same exit code as cli.NOK() (1), but
// — unlike NOK, which is silent — the framework itself prints err to
// stderr exactly once, prefixed with the dispatched Command's name, and
// preserves err's wrapped chain rather than flattening it.
func TestErrorOutcomePrintsOnceWithCommandPrefixAndExitsOne(t *testing.T) {
	inner := fmt.Errorf("underlying cause")
	wrapped := fmt.Errorf("failed to seal Batch: %w", inner)
	act := cli.Command("generate", cli.Define("", func(ctx cli.Context) cli.Outcome {
		return cli.Error(wrapped)
	}))
	app := cli.New(act)

	var out, errBuf bytes.Buffer
	code := app.Execute(context.Background(), []string{"generate"}, nil, &out, &errBuf)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	got := errBuf.String()
	want := "generate: failed to seal Batch: underlying cause\n"
	if got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
	if !errors.Is(wrapped, inner) {
		t.Fatal("sanity: wrapped should still Is-match inner")
	}
}

func TestErrorRejectsNil(t *testing.T) {
	defer func() {
		if got := recover(); got == nil || !strings.Contains(fmt.Sprint(got), "non-nil") {
			t.Fatalf("panic = %v, want non-nil error diagnostic", got)
		}
	}()
	_ = cli.Error(nil)
}

func TestMarkedInteractiveInputFailureExitsTwo(t *testing.T) {
	act := cli.Command("ask", cli.Define("", func(cli.Context) cli.Outcome {
		return cli.Error(markedInputError{err: errors.New("must contain a word")})
	}))
	app := cli.New(act)

	var out, errBuf bytes.Buffer
	if code := app.Execute(context.Background(), []string{"ask"}, nil, &out, &errBuf); code != 2 {
		t.Fatalf("code = %d, want 2; stderr=%q", code, errBuf.String())
	}
	if got, want := errBuf.String(), "ask: must contain a word\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}

type markedInputError struct{ err error }

func (e markedInputError) Error() string  { return e.err.Error() }
func (e markedInputError) Unwrap() error  { return e.err }
func (markedInputError) InputError() bool { return true }

// TestExecuteWithNilWritersDoesNotPanic proves that Execute must
// normalize nil out/err writers for both handler-side writes and its own direct
// writes (the input-error path and the cli.Error single-print path) must
// not panic on a literal nil writer either.
func TestExecuteWithNilWritersDoesNotPanic(t *testing.T) {
	t.Run("input error with nil err writer", func(t *testing.T) {
		name := param.String("")
		act := cli.Command("greet", cli.Define("", func(ctx cli.Context) cli.Outcome { return cli.OK() }, cli.WithOption(name, "name")))
		app := cli.New(act)

		var out bytes.Buffer
		code := app.Execute(context.Background(), []string{"greet"}, nil, &out, nil)
		if code != 2 {
			t.Fatalf("code = %d, want 2", code)
		}
	})

	t.Run("cli.Error with nil out and err writers", func(t *testing.T) {
		act := cli.Command("boom", cli.Define("", func(ctx cli.Context) cli.Outcome {
			return cli.Error(fmt.Errorf("kaboom"))
		}))
		app := cli.New(act)

		code := app.Execute(context.Background(), []string{"boom"}, nil, nil, nil)
		if code != 1 {
			t.Fatalf("code = %d, want 1", code)
		}
	})
}

// TestConcurrentExecutionsDoNotCrossContaminateOutput proves that each
// App.Execute call's handler writes through the
// independently injected out buffer for that call, with no cross-talk
// between concurrent executions of the same App/Definition.
func TestConcurrentExecutionsDoNotCrossContaminateOutput(t *testing.T) {
	message := param.String("")
	act := cli.Command("say", cli.Define("", func(ctx cli.Context) cli.Outcome {
		fmt.Fprintln(ctx.Stdout(), cli.Read(ctx, message))
		return cli.OK()
	}, cli.WithOption(message, "message")))
	app := cli.New(act)

	const n = 25
	var wg sync.WaitGroup
	results := make([]string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var out, errBuf bytes.Buffer
			msg := fmt.Sprintf("hello-%d", i)
			code := app.Execute(context.Background(), []string{"say", "--message=" + msg}, nil, &out, &errBuf)
			if code != 0 {
				t.Errorf("goroutine %d: code = %d, want 0; stderr=%s", i, code, errBuf.String())
			}
			results[i] = strings.TrimSpace(out.String())
		}(i)
	}
	wg.Wait()

	for i, got := range results {
		want := fmt.Sprintf("hello-%d", i)
		if got != want {
			t.Errorf("goroutine %d: stdout = %q, want %q", i, got, want)
		}
	}
}

// Concurrent executions read only their own injected input through Context.Stdin.
func TestConcurrentExecutionsDoNotCrossContaminateInput(t *testing.T) {
	act := cli.Command("echo", cli.Define("", func(ctx cli.Context) cli.Outcome {
		line, err := bufio.NewReader(ctx.Stdin()).ReadString('\n')
		if err != nil {
			fmt.Fprintln(ctx.Stderr(), err)
			return cli.NOK()
		}
		fmt.Fprint(ctx.Stdout(), line)
		return cli.OK()
	}))
	app := cli.New(act)

	const n = 25
	var wg sync.WaitGroup
	results := make([]string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			in := strings.NewReader(fmt.Sprintf("input-%d\n", i))
			var out, errBuf bytes.Buffer
			code := app.Execute(context.Background(), []string{"echo"}, in, &out, &errBuf)
			if code != 0 {
				t.Errorf("goroutine %d: code = %d, want 0; stderr=%s", i, code, errBuf.String())
			}
			results[i] = strings.TrimSpace(out.String())
		}(i)
	}
	wg.Wait()

	for i, got := range results {
		want := fmt.Sprintf("input-%d", i)
		if got != want {
			t.Errorf("goroutine %d: stdout = %q, want %q", i, got, want)
		}
	}
}

// TestNewIsPureAndExecuteRepeatable proves assembling an App runs no
// handler and that the same App executes repeatedly with injected resources.
func TestNewIsPureAndExecuteRepeatable(t *testing.T) {
	name := param.String("")
	calls := 0
	greet := cli.Command("greet", cli.Define("", func(ctx cli.Context) cli.Outcome {
		calls++
		fmt.Fprintf(ctx.Stdout(), "hi %s\n", cli.Read(ctx, name))
		return cli.OK()
	}, cli.WithArgument(name, "name")))
	app := cli.New(greet, cli.Group("g", greet))
	if calls != 0 {
		t.Fatal("New executed a handler")
	}
	for _, who := range []string{"ada", "grace"} {
		var out bytes.Buffer
		if code := app.Execute(context.Background(), []string{"greet", who}, nil, &out, nil); code != 0 || out.String() != "hi "+who+"\n" {
			t.Fatalf("%s: code %d out %q", who, code, out.String())
		}
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestUndeclaredReadIsProgrammerErrorExit1(t *testing.T) {
	stray := param.String("stray-value")
	var captured any
	app := cli.New(cli.Command("boom", cli.Define("", func(ctx cli.Context) (outcome cli.Outcome) {
		defer func() {
			if r := recover(); r != nil {
				captured = r
				panic(r)
			}
		}()
		_ = cli.Read(ctx, stray)
		return cli.OK()
	})))
	var out, errBuf bytes.Buffer
	if code := app.Execute(context.Background(), []string{"boom"}, nil, &out, &errBuf); code != 1 {
		t.Fatalf("exit = %d, want 1; stderr=%s", code, errBuf.String())
	}
	e, ok := captured.(error)
	if !ok {
		t.Fatalf("panic value = %v (%T), want error", captured, captured)
	}
	var u *param.UndeclaredReadError
	if !errors.As(e, &u) || u.Param != stray {
		t.Fatalf("panic = %v, want *param.UndeclaredReadError for stray-value", e)
	}
}

func TestOutcomeExitCodesAndOutput(t *testing.T) {
	for _, tc := range []struct {
		name    string
		outcome cli.Outcome
		code    int
	}{
		{"ok", cli.OK(), 0}, {"nok", cli.NOK(), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := cli.New(cli.Command(tc.name, cli.Define("", func(cli.Context) cli.Outcome { return tc.outcome })))
			code, out, stderr := run(app, tc.name)
			if code != tc.code || out != "" || stderr != "" {
				t.Fatalf("code=%d want=%d stdout=%q stderr=%q", code, tc.code, out, stderr)
			}
		})
	}
}
