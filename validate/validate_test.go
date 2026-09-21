package validate_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/go-way2go/cli/validate"
)

func TestApplyNoValidatorsReturnsNil(t *testing.T) {
	if err := validate.Apply("anything"); err != nil {
		t.Fatalf("Apply with no validators = %v, want nil", err)
	}
}

func TestMin(t *testing.T) {
	validator := validate.Min(3)
	for _, value := range []int{3, 4} {
		if err := validator(value); err != nil {
			t.Errorf("Min(3)(%d) = %v, want nil", value, err)
		}
	}
	if err := validator(2); err == nil || err.Error() != "must be at least 3 (got 2)" {
		t.Errorf("Min(3)(2) = %v, want useful lower-bound error", err)
	}
}

func TestMax(t *testing.T) {
	validator := validate.Max(3)
	for _, value := range []int{2, 3} {
		if err := validator(value); err != nil {
			t.Errorf("Max(3)(%d) = %v, want nil", value, err)
		}
	}
	if err := validator(4); err == nil || err.Error() != "must be at most 3 (got 4)" {
		t.Errorf("Max(3)(4) = %v, want useful upper-bound error", err)
	}
}

func TestOrderedValidatorsAlsoSupportStrings(t *testing.T) {
	if err := validate.Min("cat")("cat"); err != nil {
		t.Fatalf("Min[string](cat)(cat) = %v, want nil", err)
	}
	if err := validate.Max("cat")("dog"); err == nil {
		t.Fatal("Max[string](cat)(dog) = nil, want error")
	}
}

func TestBetween(t *testing.T) {
	validator := validate.Between(1, 3)
	for _, value := range []int{1, 2, 3} {
		if err := validator(value); err != nil {
			t.Errorf("Between(1, 3)(%d) = %v, want nil", value, err)
		}
	}
	if err := validator(0); err == nil || err.Error() != "must be between 1 and 3 (got 0)" {
		t.Errorf("Between(1, 3)(0) = %v, want useful range error", err)
	}
	if err := validator(4); err == nil || err.Error() != "must be between 1 and 3 (got 4)" {
		t.Errorf("Between(1, 3)(4) = %v, want useful range error", err)
	}
}

func TestBetweenPanicsForReversedBounds(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Between(3, 1) did not panic")
		}
	}()
	validate.Between(3, 1)
}

func TestOneOf(t *testing.T) {
	validator := validate.OneOf("json", "yaml")
	if err := validator("json"); err != nil {
		t.Fatalf("OneOf(json, yaml)(json) = %v, want nil", err)
	}
	if err := validator("toml"); err == nil || err.Error() != "must be one of json, yaml (got toml)" {
		t.Errorf("OneOf(json, yaml)(toml) = %v, want useful choice error", err)
	}
}

func TestOneOfWithNoCandidatesRejectsEveryValue(t *testing.T) {
	if err := validate.OneOf[int]()(1); err == nil || err.Error() != "must be one of no values (got 1)" {
		t.Errorf("OneOf[int]()(1) = %v, want useful error", err)
	}
}

func TestNonEmpty(t *testing.T) {
	validator := validate.NonEmpty()
	if err := validator(" "); err != nil {
		t.Fatalf("NonEmpty()(space) = %v, want nil", err)
	}
	if err := validator(""); err == nil || err.Error() != "must not be empty" {
		t.Errorf("NonEmpty()(empty) = %v, want useful error", err)
	}
}

func TestEachRunsValidatorsByElementAndValidatorOrder(t *testing.T) {
	var ran []string
	first := func(value int) error {
		ran = append(ran, "first")
		return nil
	}
	second := func(value int) error {
		ran = append(ran, "second")
		return nil
	}

	if err := validate.Each(first, second)([]int{1, 2}); err != nil {
		t.Fatalf("Each = %v, want nil", err)
	}
	if want := []string{"first", "second", "first", "second"}; !reflect.DeepEqual(ran, want) {
		t.Errorf("validator order = %v, want %v", ran, want)
	}
}

func TestEachStopsAtFirstInvalidElementAndWrapsCause(t *testing.T) {
	sentinel := errors.New("must be positive")
	var seen []int
	positive := func(value int) error {
		seen = append(seen, value)
		if value <= 0 {
			return sentinel
		}
		return nil
	}

	err := validate.Each(positive)([]int{1, 0, 2})
	if !errors.Is(err, sentinel) {
		t.Errorf("Each error = %v, want it to wrap %v", err, sentinel)
	}
	if err == nil || err.Error() != "element 2: must be positive" {
		t.Errorf("Each error = %v, want element context", err)
	}
	if want := []int{1, 0}; !reflect.DeepEqual(seen, want) {
		t.Errorf("seen = %v, want %v; elements after failure must be skipped", seen, want)
	}
}

func TestEachUsesApplyFirstErrorWinsForEachElement(t *testing.T) {
	secondRan := false
	first := func(value string) error { return errors.New("first failed") }
	second := func(value string) error {
		secondRan = true
		return nil
	}

	err := validate.Each(first, second)([]string{"bad"})
	if err == nil || err.Error() != "element 1: first failed" {
		t.Errorf("Each error = %v, want first validator error", err)
	}
	if secondRan {
		t.Error("second validator ran after first validator failed")
	}
}

func TestApplySingleValidatorNoErrorReturnsNil(t *testing.T) {
	ok := func(v string) error { return nil }
	if err := validate.Apply("x", ok); err != nil {
		t.Fatalf("Apply = %v, want nil", err)
	}
}

func TestApplyOrderedFirstErrorWins(t *testing.T) {
	secondRan := false
	first := func(v string) error { return errors.New("first failed") }
	second := func(v string) error {
		secondRan = true
		return errors.New("second failed")
	}

	err := validate.Apply("x", first, second)
	if err == nil || err.Error() != "first failed" {
		t.Fatalf("Apply = %v, want \"first failed\"", err)
	}
	if secondRan {
		t.Fatalf("second validator ran after first failed; want it skipped")
	}
}

func TestApplyMultipleValidatorsAllPassReturnsNil(t *testing.T) {
	var ran []string
	first := func(v int) error { ran = append(ran, "first"); return nil }
	second := func(v int) error { ran = append(ran, "second"); return nil }
	third := func(v int) error { ran = append(ran, "third"); return nil }

	if err := validate.Apply(1, first, second, third); err != nil {
		t.Fatalf("Apply = %v, want nil", err)
	}
	if len(ran) != 3 || ran[0] != "first" || ran[1] != "second" || ran[2] != "third" {
		t.Fatalf("ran = %v, want [first second third] all to have run", ran)
	}
}

// TestApplyReusesAcrossDistinctTypeInstantiations proves Apply is genuinely
// generic, not merely written once and used at a single T: string and int
// instantiations both compile and behave identically against this package's
// only exported function.
func TestApplyReusesAcrossDistinctTypeInstantiations(t *testing.T) {
	strErr := errors.New("bad string")
	intErr := errors.New("bad int")

	if err := validate.Apply("ok", func(v string) error { return nil }); err != nil {
		t.Fatalf("Apply[string] = %v, want nil", err)
	}
	if err := validate.Apply("bad", func(v string) error { return strErr }); err != strErr {
		t.Fatalf("Apply[string] = %v, want %v", err, strErr)
	}
	if err := validate.Apply(5, func(v int) error { return nil }); err != nil {
		t.Fatalf("Apply[int] = %v, want nil", err)
	}
	if err := validate.Apply(-1, func(v int) error { return intErr }); err != intErr {
		t.Fatalf("Apply[int] = %v, want %v", err, intErr)
	}
}

func TestApplySkipsNilValidatorWithoutPanicking(t *testing.T) {
	var ran []string
	first := func(v string) error { ran = append(ran, "first"); return nil }
	third := func(v string) error { ran = append(ran, "third"); return nil }

	err := validate.Apply("x", first, nil, third)
	if err != nil {
		t.Fatalf("Apply = %v, want nil", err)
	}
	if len(ran) != 2 || ran[0] != "first" || ran[1] != "third" {
		t.Fatalf("ran = %v, want [first third], nil validator must be skipped not called", ran)
	}
}

func TestCheckAdaptsFunctionAndPreservesOrder(t *testing.T) {
	sentinel := errors.New("rejected")
	var calls []string
	first := validate.Check(func(s string) error { calls = append(calls, "first"); return nil })
	second := validate.Check(func(s string) error { calls = append(calls, "second"); return sentinel })
	third := validate.Check(func(s string) error { calls = append(calls, "third"); return nil })
	if err := validate.Apply("x", first, second, third); !errors.Is(err, sentinel) {
		t.Fatalf("Apply = %v, want sentinel", err)
	}
	if got := strings.Join(calls, ","); got != "first,second" {
		t.Fatalf("calls = %s, want first,second", got)
	}
}

func TestCheckNilFunctionIsSkipped(t *testing.T) {
	var fn func(int) error
	nilValidator := validate.Check(fn)
	if nilValidator != nil {
		t.Fatal("Check(nil) must yield a nil validator")
	}
	if err := validate.Apply(1, nilValidator); err != nil {
		t.Fatalf("Apply with nil validator = %v", err)
	}
}
