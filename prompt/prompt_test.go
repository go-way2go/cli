package prompt_test

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
	"github.com/go-way2go/cli/param"
	"github.com/go-way2go/cli/prompt"
	"github.com/go-way2go/cli/validate"
)

const label = "age: "

func parseInt(s string) (int, error) {
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}
	return v, nil
}

// newCtx builds a context carrying data as the bound input source and
// independent stdout/stderr buffers, mirroring how cli/exec_test.go and
// input/input_test.go construct contexts for deterministic tests.
func newCtx(data string) (ctx context.Context, out, errOut *bytes.Buffer) {
	out = &bytes.Buffer{}
	errOut = &bytes.Buffer{}
	ctx = output.NewContext(context.Background(), out, errOut)
	ctx = input.NewContext(ctx, strings.NewReader(data))
	return ctx, out, errOut
}

func TestReadSuccessNoValidators(t *testing.T) {
	ctx, out, errOut := newCtx("42\n")

	got, err := prompt.Read(ctx, label, parseInt)
	if err != nil {
		t.Fatalf("Read returned err = %v, want nil", err)
	}
	if got != 42 {
		t.Fatalf("Read returned %d, want 42", got)
	}
	if !strings.Contains(errOut.String(), label) {
		t.Fatalf("stderr = %q, want it to contain label %q", errOut.String(), label)
	}
	if out.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", out.String())
	}
}

func TestReadParseErrorNoRetryReturnsImmediately(t *testing.T) {
	ctx, out, errOut := newCtx("abc\n")

	_, err := prompt.Read(ctx, label, parseInt)
	if err == nil {
		t.Fatalf("expected a parse error, got nil")
	}
	var invalid *prompt.InvalidInputError
	if !errors.As(err, &invalid) {
		t.Fatalf("error = %T, want *prompt.InvalidInputError", err)
	}
	if !invalid.InputError() {
		t.Fatal("InvalidInputError.InputError() = false, want true")
	}
	if n := strings.Count(errOut.String(), label); n != 1 {
		t.Fatalf("label was written %d times, want exactly 1 (no retry loop should have run)", n)
	}
	if out.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", out.String())
	}
}

func TestReadParseErrorWithRetryInvalidRetries(t *testing.T) {
	ctx, _, errOut := newCtx("abc\n42\n")

	got, err := prompt.Read(ctx, label, parseInt, prompt.RetryInvalid[int]())
	if err != nil {
		t.Fatalf("Read returned err = %v, want nil", err)
	}
	if got != 42 {
		t.Fatalf("Read returned %d, want 42", got)
	}
	if n := strings.Count(errOut.String(), label); n != 2 {
		t.Fatalf("label was written %d times, want exactly 2 (initial prompt + one retry)", n)
	}
}

func TestReadValidatorRejectsNoRetryReturnsImmediately(t *testing.T) {
	ctx, _, errOut := newCtx("-5\n")
	positive := func(v int) error {
		if v <= 0 {
			return errors.New("must be positive")
		}
		return nil
	}

	_, err := prompt.Read(ctx, label, parseInt, prompt.Validate(positive))
	if err == nil {
		t.Fatalf("expected a validation error, got nil")
	}
	if err.Error() != "must be positive" {
		t.Fatalf("err = %q, want %q", err.Error(), "must be positive")
	}
	if n := strings.Count(errOut.String(), label); n != 1 {
		t.Fatalf("label was written %d times, want exactly 1 (no retry loop should have run)", n)
	}
}

func TestReadValidatorRejectsWithRetryRetriesUntilValid(t *testing.T) {
	ctx, _, _ := newCtx("-5\n7\n")
	positive := func(v int) error {
		if v <= 0 {
			return errors.New("must be positive")
		}
		return nil
	}

	got, err := prompt.Read(ctx, label, parseInt, prompt.Validate(positive), prompt.RetryInvalid[int]())
	if err != nil {
		t.Fatalf("Read returned err = %v, want nil", err)
	}
	if got != 7 {
		t.Fatalf("Read returned %d, want 7", got)
	}
}

func TestReadReusesTheSameValidatorValueAsParam(t *testing.T) {
	sentinel := errors.New("must be positive")
	positive := validate.Check(func(v int) error {
		if v <= 0 {
			return sentinel
		}
		return nil
	})

	// The same typed value is accepted by both independent public APIs.
	p := param.Int("", positive)
	_, paramErr := param.Prepare([]param.AnyDescriptor{p}, map[param.AnyDescriptor]param.RawValue{
		p: {Value: "-1", Present: true},
	})
	if !errors.Is(paramErr, sentinel) {
		t.Fatalf("Param validation error = %v, want it to unwrap %v", paramErr, sentinel)
	}

	ctx, _, _ := newCtx("-1\n")
	_, promptErr := prompt.Read(ctx, label, parseInt, prompt.Validate(positive))
	if !errors.Is(promptErr, sentinel) {
		t.Fatalf("Prompt validation error = %v, want it to unwrap %v", promptErr, sentinel)
	}
}

// TestReadValidatorsAccumulateInOrderFirstErrorWins proves prompt.Validate's
// accumulation wiring is correct: multiple Validate(...) calls among one
// Read call's options run in the order supplied, and the first failure wins
// — the same ordered, first-error-wins contract validate.Apply itself
// implements (this is not re-testing Apply, just prompt's wiring to it).
func TestReadValidatorsAccumulateInOrderFirstErrorWins(t *testing.T) {
	var order []string
	first := func(v int) error {
		order = append(order, "first")
		return errors.New("first failed")
	}
	second := func(v int) error {
		order = append(order, "second")
		return errors.New("second failed")
	}

	ctx, _, _ := newCtx("5\n")
	_, err := prompt.Read(ctx, label, parseInt, prompt.Validate(first), prompt.Validate(second))
	if err == nil {
		t.Fatalf("expected a validation error, got nil")
	}
	if err.Error() != "first failed" {
		t.Fatalf("err = %q, want %q", err.Error(), "first failed")
	}
	if len(order) != 1 || order[0] != "first" {
		t.Fatalf("order = %v, want only [first] to have run", order)
	}
}

func TestReadEOFReturnsImmediatelyEvenWithRetryInvalid(t *testing.T) {
	ctx, _, errOut := newCtx("")

	_, err := prompt.Read(ctx, label, parseInt, prompt.RetryInvalid[int]())
	if !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want io.EOF", err)
	}
	// The prompt is still written once, for the one read attempt that
	// discovered the source was exhausted; RetryInvalid never applies to an
	// input.ReadLine error, so exactly one attempt (and one label write)
	// happens.
	if n := strings.Count(errOut.String(), label); n != 1 {
		t.Fatalf("label was written %d times, want exactly 1", n)
	}
}

// TestReadReturnsPromptlyOnAlreadyCancelledContext depends on
// input.ReadLine's documented cooperative cancellation check: a context
// cancelled before Read is called returns ctx.Err() immediately. Read
// performs its own equivalent check before writing the prompt label, so no
// prompt line is written for a read that was never going to happen.
func TestReadReturnsPromptlyOnAlreadyCancelledContext(t *testing.T) {
	base, cancel := context.WithCancel(context.Background())
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	ctx := output.NewContext(base, out, errOut)
	ctx = input.NewContext(ctx, strings.NewReader("unread\n"))
	cancel()

	_, err := prompt.Read(ctx, label, parseInt)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if errOut.Len() != 0 {
		t.Fatalf("stderr = %q, want empty (no prompt should be written for an already-cancelled context)", errOut.String())
	}
	if out.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", out.String())
	}
}
