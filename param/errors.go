package param

import "fmt"

// MissingValueError reports that a required Param (one without a static default)
// was not supplied. It is an ordinary input error: callers map it to their
// own input-error representation, never
// to the Way2Go programmer-error recovery path.
type MissingValueError struct {
	// Param identifies the descriptor; the external label is the adapter's.
	Param AnyDescriptor
}

func (e *MissingValueError) Error() string {
	return fmt.Sprintf("param %s: value is required", Label(e.Param))
}

// ParseError records that a supplied raw value could not be parsed as the
// Param's declared type. It is retained as the wrapped cause of a
// ValidationError so callers can inspect the raw value and parse cause while
// targets consistently classify all rejected input as ValidationError.
type ParseError struct {
	Param AnyDescriptor
	Raw   string
	Err   error
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("param %s: %v", Label(e.Param), e.Err)
}

func (e *ParseError) Unwrap() error { return e.Err }

// ValidationError reports that a supplied value was rejected by a declared
// validator. It is an ordinary input error.
type ValidationError struct {
	Param AnyDescriptor
	Err   error
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("param %s: %v", Label(e.Param), e.Err)
}

func (e *ValidationError) Unwrap() error { return e.Err }

// UndeclaredReadError reports that param.Read was called with a descriptor
// that is not part of the prepared value set reachable from ctx — i.e. a
// Param the invocation never declared, or a call made outside any
// prepared invocation context at all. This is a Way2Go programmer error, not
// user input: it can only happen because handler or middleware code reads a
// Param it never declared. It implements the Way2Go programmer-error
// contract via Way2GoProgrammerError, so
// target recovery boundaries can recognise it through errors.As without
// param importing an execution package.
type UndeclaredReadError struct {
	Param AnyDescriptor
}

func (e *UndeclaredReadError) Error() string {
	return fmt.Sprintf("param: read of undeclared param %s", Label(e.Param))
}

// Way2GoProgrammerError marks UndeclaredReadError as a Way2Go programmer
// error, recognizable structurally by execution boundaries.
func (e *UndeclaredReadError) Way2GoProgrammerError() {}

// Label renders d for diagnostics that have no external name, from its type
// and description, for example `string "Search text"`. Adapters should prefer
// their own external label and use Label only as a fallback.
func Label(d AnyDescriptor) string {
	if d == nil {
		return "<unknown>"
	}
	return d.core().label()
}
