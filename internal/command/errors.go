package command

import (
	"errors"
	"fmt"

	"github.com/go-way2go/cli/internal/invocation"
)

// programmerError wraps a recovered Way2Go programmer error (see
// programmerFailure) so App.Execute can tell it apart from an
// ordinary input error returned by Cobra's own parsing/argument-count
// validation or by param.Prepare. It is the sole error type App.Execute maps
// to exit code 1 when returned by parser dispatch; other dispatch errors map
// to exit code 2. Handler outcomes are classified separately.
type programmerError struct {
	err error
}

func (e *programmerError) Error() string { return e.err.Error() }
func (e *programmerError) Unwrap() error { return e.err }

// invoke runs handler with ctx inside a recovery boundary that recovers only
// a panic whose value implements programmerFailure (the same
// selective-recovery discipline used by every Way2Go target execution
// boundary): such a panic is turned into a *programmerError and returned as
// err rather than propagated. Every other panic — including an ordinary
// input error type such as *param.MissingValueError, which is never raised
// as a panic by this package's own code in the first place, and any
// unrelated application panic — is re-panicked unchanged, so it is never
// silently swallowed or mislabeled as a Param error.
func invoke(ctx invocation.Context, handler invocation.Handler) (outcome invocation.Outcome, err error) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		if asErr, ok := r.(error); ok {
			var pe programmerFailure
			if errors.As(asErr, &pe) {
				err = &programmerError{err: asErr}
				return
			}
		}
		panic(r)
	}()
	outcome = handler(ctx)
	return outcome, nil
}

// inputError prefixes a parameter preparation failure with "cli:" while
// preserving its underlying cause.
// App.Execute does not need to type-switch on it specially: any error that
// is not a *programmerError already maps to exit code 2.
type inputError struct {
	err error
}

func (e *inputError) Error() string { return fmt.Sprintf("cli: %v", e.err) }
func (e *inputError) Unwrap() error { return e.err }

// inputFailure is the small marker interface interactive packages use to
// identify an ordinary invalid user entry. It deliberately lives here rather
// than importing prompt: prompt remains independent from CLI, and another
// interactive source can opt into the same fixed status-2 convention.
type inputFailure interface {
	InputError() bool
}

func isInputFailure(err error) bool {
	var failure inputFailure
	return errors.As(err, &failure) && failure.InputError()
}

// programmerFailure is structurally satisfied by programmer-mistake errors.
type programmerFailure interface {
	error
	Way2GoProgrammerError()
}
