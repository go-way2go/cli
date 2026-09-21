// Package validate implements Way2Go's target-neutral, generic validator
// contract: a Validator[T] is any func(T) error, and Apply runs a sequence of
// them against one value in order, stopping at the first non-nil error.
//
// This shape exists to be shared, not merely reused. Package param declares
// typed, target-neutral Param descriptors and package prompt (a sibling,
// target-neutral CLI-prompt package) declares typed interactive prompts;
// both need "run these checks against a value of type T, in order, first
// failure wins" and neither should depend on the other to get it. Rather
// than each package defining its own equivalent function type and loop —
// which would make a validator written for one unusable with the other, and
// would let their semantics quietly drift apart — this package is the
// single leaf both depend on: validate <- param and validate <- prompt.
// It depends on nothing but the standard library, and it must stay that way,
// so it can never itself pull param, prompt, or any target package into a
// cycle.
//
// Apply's ordering and first-error-wins semantics are deliberately simple:
// validators commonly build on one another (e.g. "is not empty" before "is a
// valid email"), so running every validator regardless of earlier failures
// would surface confusing, redundant errors for what is really one
// underlying problem. Callers that want every failure reported independently
// should collect their own results outside of Apply.
package validate

import (
	"cmp"
	"fmt"
	"strings"
)

// Validator is a single check against a value of type T: it returns nil if
// the value is acceptable, or a non-nil error describing why it is not.
// Validator carries no notion of "which target" or "which field" — that
// context belongs to the caller (e.g. param.ValidationError names the
// Param), not to the validator itself.
type Validator[T any] func(T) error

// ParameterOption marks a Validator[T] as acceptable beside the construction
// options of a parameter of value type T. The method is a structural marker
// only and is never called: its signature includes T so that param.Option[T]
// accepts validators of the matching type at compile time, while this package
// does not import param.
func (Validator[T]) ParameterOption(T) {}

// Check adapts fn to a Validator[T]. The type argument is inferred from fn.
// A nil fn yields a nil Validator, which Apply and parameter preparation skip
// like any other nil validator.
func Check[T any](fn func(T) error) Validator[T] { return Validator[T](fn) }

// Apply runs validators against value in order and returns the first
// non-nil error; later validators do not run once one has failed. Apply
// returns nil if every validator passes, including when validators is empty.
// A nil entry in validators is skipped rather than called, so callers that
// assemble a validator slice programmatically need not filter out nil
// entries themselves.
func Apply[T any](value T, validators ...Validator[T]) error {
	for _, v := range validators {
		if v == nil {
			continue
		}
		if err := v(value); err != nil {
			return err
		}
	}
	return nil
}

// Min returns a validator that requires value to be greater than or equal to
// minimum. It is inclusive, so Min(1) accepts 1.
func Min[T cmp.Ordered](minimum T) Validator[T] {
	return func(value T) error {
		if value < minimum {
			return fmt.Errorf("must be at least %v (got %v)", minimum, value)
		}
		return nil
	}
}

// Max returns a validator that requires value to be less than or equal to
// maximum. It is inclusive, so Max(1) accepts 1.
func Max[T cmp.Ordered](maximum T) Validator[T] {
	return func(value T) error {
		if value > maximum {
			return fmt.Errorf("must be at most %v (got %v)", maximum, value)
		}
		return nil
	}
}

// Between returns a validator that requires value to fall in the inclusive
// range from minimum through maximum. Between panics when minimum is greater
// than maximum because that is a programming error in a validator definition,
// not invalid external input.
func Between[T cmp.Ordered](minimum, maximum T) Validator[T] {
	if minimum > maximum {
		panic(fmt.Sprintf("validate: minimum %v exceeds maximum %v", minimum, maximum))
	}
	return func(value T) error {
		if value < minimum || value > maximum {
			return fmt.Errorf("must be between %v and %v (got %v)", minimum, maximum, value)
		}
		return nil
	}
}

// OneOf returns a validator that requires value to equal one of allowed.
// Calling OneOf without any allowed values returns a validator that rejects
// every value; this is useful when the allowed set is assembled dynamically.
func OneOf[T comparable](allowed ...T) Validator[T] {
	return func(value T) error {
		for _, candidate := range allowed {
			if value == candidate {
				return nil
			}
		}
		return fmt.Errorf("must be one of %s (got %v)", formatAllowed(allowed), value)
	}
}

func formatAllowed[T any](allowed []T) string {
	if len(allowed) == 0 {
		return "no values"
	}
	values := make([]string, len(allowed))
	for i, value := range allowed {
		values[i] = fmt.Sprintf("%v", value)
	}
	return strings.Join(values, ", ")
}

// NonEmpty returns a validator that rejects the empty string. Whitespace is
// intentionally not trimmed: whether whitespace-only input is meaningful is
// domain-specific and belongs to a separate validator or parser.
func NonEmpty() Validator[string] {
	return func(value string) error {
		if value == "" {
			return fmt.Errorf("must not be empty")
		}
		return nil
	}
}

// Each returns a validator for a slice that applies validators to every
// element in slice order. For each element, validators run with Apply's
// ordering and first-error-wins semantics. It stops at the first failing
// element and wraps that error with its one-based element number.
func Each[T any](validators ...Validator[T]) Validator[[]T] {
	return func(values []T) error {
		for i, value := range values {
			if err := Apply(value, validators...); err != nil {
				return fmt.Errorf("element %d: %w", i+1, err)
			}
		}
		return nil
	}
}
