// Package prompt implements Way2Go's imperative, generic helper for
// interactively prompting a user over an invocation Context's input and stderr:
// Read[T] writes a prompt label, reads one line, parses it into a T, and
// optionally validates and retries on invalid entry.
//
// Prompts are called directly by handlers and return their values to the caller.
// The package uses validate for typed validation and invocation-bound input and
// stderr for interaction. It is independent of param and CLI declarations.
package prompt

import (
	"context"

	"github.com/go-way2go/cli/internal/input"
	"github.com/go-way2go/cli/internal/output"
	"github.com/go-way2go/cli/validate"
)

// settings accumulates the Options applied to a single Read[T] call.
type settings[T any] struct {
	validators   []validate.Validator[T]
	retryInvalid bool
}

// Option configures a single Read[T] call, via Validate and RetryInvalid.
type Option[T any] func(*settings[T])

// InvalidInputError reports that a line was read successfully but could not
// be parsed or validated. It is distinct from input-source failures such as
// EOF, I/O errors and context cancellation, which Read returns unchanged.
// Callers such as cli can use errors.As to classify this as ordinary user
// input failure while errors.Is/errors.As still reach the parser or validator
// error through Unwrap.
type InvalidInputError struct {
	Err error
}

func (e *InvalidInputError) Error() string {
	if e == nil || e.Err == nil {
		return "invalid prompt input"
	}
	return e.Err.Error()
}

func (e *InvalidInputError) Unwrap() error { return e.Err }

// InputError marks InvalidInputError as ordinary invalid user input. It lets
// target packages classify the error through a small local interface instead
// of importing prompt, preserving prompt's independence from CLI targets.
func (e *InvalidInputError) InputError() bool { return true }

// Validate adds one or more validators, run in declaration order against a
// successfully parsed value; the first validator to return a non-nil error
// wins, via validate.Apply. Multiple Validate calls among a single Read
// call's options accumulate in the order supplied.
func Validate[T any](validators ...validate.Validator[T]) Option[T] {
	return func(s *settings[T]) {
		s.validators = append(s.validators, validators...)
	}
}

// RetryInvalid makes Read re-prompt on an invalid entry (a parse error or a
// validation failure) instead of returning it immediately. Without
// RetryInvalid, Read returns the first invalid-entry error it encounters,
// leaving the caller to decide how to fail. RetryInvalid never applies to
// input read errors (EOF, other I/O failures, context cancellation) —
// those are unrecoverable read conditions, not invalid entries, and always
// return immediately regardless of this option.
func RetryInvalid[T any]() Option[T] {
	return func(s *settings[T]) {
		s.retryInvalid = true
	}
}

// Read writes label to ctx's bound stderr sink (never
// stdout — so a command's actual result output stays uncontaminated by
// interactive prompt chatter), reads one line from ctx's bound input,
// parses it with parse, and — if parsing succeeds — validates it against
// every validator accumulated via Validate, in order, using
// validate.Apply.
//
// label is written verbatim as one line: Read appends nothing beyond the
// trailing newline the prompt itself adds, so label is expected to
// already carry whatever punctuation the caller wants (e.g. "Enter your
// name:" rather than "Enter your name" with punctuation bolted on
// elsewhere).
//
// Read distinguishes two failure classes:
//
//   - An input read error — io.EOF (source exhausted), any other I/O
//     error, or a context-cancellation error — is an unrecoverable read
//     condition. Read returns it immediately as (zero T, err), never
//     retrying, regardless of RetryInvalid.
//   - A parse error, or a validation error from a successfully parsed
//     value, is an invalid user entry. Without RetryInvalid, Read returns it
//     immediately as (zero T, err). With RetryInvalid, Read writes the error
//     to ctx's stderr sink and re-prompts (re-writing label and reading
//     another line), until either a valid entry arrives or an
//     input read error ends the loop.
//
// Before writing label on each attempt (including the first), Read performs
// the same cheap, cooperative pre-read cancellation check the input read
// itself performs: if ctx is already done, Read returns (zero T, ctx.Err())
// without writing a prompt line at all. This is a deliberate
// belt-and-suspenders check, not a replacement for the input read's own — it
// exists so an already-cancelled context never produces a dangling prompt
// line on ctx's stderr sink for a read that was never going to happen; a
// context that becomes cancelled after this check but before or during the
// ReadLine call is still caught by ReadLine's own check.
func Read[T any](
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

	var zero T
	for {
		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		default:
		}

		output.Errorln(ctx, label)

		line, err := input.ReadLine(ctx)
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
