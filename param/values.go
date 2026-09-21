package param

import "context"

// RawValue is a single external input already resolved by a target binding
// (Web query/form, CLI option/arg, ...), before parsing. Present
// distinguishes an absent value from an explicitly supplied empty string:
// the two are never conflated.
type RawValue struct {
	// Value is the raw, unparsed external input. It is meaningful only
	// when Present is true.
	Value string
	// Present reports whether the target binding found this value at
	// all. An absent value falls back to the descriptor's default (if
	// any) or is a MissingValueError; a present, explicitly empty value
	// is validated like any other value.
	Present bool
}

// Values is an immutable, prepared set of typed Param values for one
// invocation. Obtain one with Prepare and make it
// reachable to handlers with NewContext; read from it with Read.
type Values struct {
	m map[AnyDescriptor]any
}

// Prepare resolves raw against parameters, in order: for each descriptor, a
// present raw value is parsed and validated; an absent value falls back to the
// descriptor's static default if it has one, or yields a MissingValueError.
// Defaults are revalidated because external state may have changed since
// construction. Prepare returns the first ordinary input error encountered,
// in parameter order; all such errors are input errors, never Way2Go
// programmer errors. Errors identify the descriptor rather than an external
// name.
//
// The returned Values contains exactly one entry per descriptor — the
// complete, validated Param view available before the first user middleware
// runs. A nil or zero descriptor panics as a programmer error.
func Prepare(parameters []AnyDescriptor, raw map[AnyDescriptor]RawValue) (*Values, error) {
	values := make(map[AnyDescriptor]any, len(parameters))
	for _, d := range parameters {
		if d == nil || d.core() == nil {
			panic("param: nil or zero descriptor passed to Prepare")
		}
		c := d.core()
		r := raw[d]

		if r.Present {
			v, err := c.parseRaw(r.Value)
			if err != nil {
				// Parsing is part of accepting external input. Expose it through
				// the same public input-error type as a validator failure while
				// retaining the parser's cause for errors.Is/errors.As.
				return nil, &ValidationError{Param: d, Err: &ParseError{Param: d, Raw: r.Value, Err: err}}
			}
			if err := c.validateAny(v); err != nil {
				return nil, &ValidationError{Param: d, Err: err}
			}
			values[d] = v
			continue
		}

		if !c.hasDefault {
			return nil, &MissingValueError{Param: d}
		}
		if err := c.validateAny(c.def); err != nil {
			return nil, &ValidationError{Param: d, Err: err}
		}
		values[d] = c.def
	}
	return &Values{m: values}, nil
}

type valuesKey struct{}

// NewContext returns a copy of parent that carries values, retrievable by
// Read from ctx or any context derived from it.
func NewContext(parent context.Context, values *Values) context.Context {
	return context.WithValue(parent, valuesKey{}, values)
}

// Read returns the prepared value for d from the Values set carried by ctx.
//
// Read never infers a source or substitutes a zero value for a missing entry.
// Prepared values may themselves be zero or nil. If ctx carries no Values, or the
// prepared Values does not contain d — because the invocation never
// declared it — Read panics with an *UndeclaredReadError, a Way2Go
// programmer error.
func Read[T any](ctx context.Context, d Descriptor[T]) T {
	values, _ := ctx.Value(valuesKey{}).(*Values)
	if values == nil {
		panic(&UndeclaredReadError{Param: d})
	}
	v, ok := values.m[d]
	if !ok {
		panic(&UndeclaredReadError{Param: d})
	}
	// A nil interface-valued parameter is stored as a nil any.
	typed, _ := v.(T)
	return typed
}
