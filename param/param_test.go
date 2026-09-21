package param_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/go-way2go/cli/file"
	"github.com/go-way2go/cli/param"
	"github.com/go-way2go/cli/validate"
)

func mustPanic(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatalf("%s: expected panic", name)
		}
	}()
	fn()
}

func prepare(t *testing.T, parameters []param.AnyDescriptor, raw map[param.AnyDescriptor]param.RawValue) (context.Context, error) {
	t.Helper()
	values, err := param.Prepare(parameters, raw)
	if err != nil {
		return nil, err
	}
	return param.NewContext(context.Background(), values), nil
}

func present(v string) param.RawValue { return param.RawValue{Value: v, Present: true} }

// -- construction / options -------------------------------------------------

func TestDescriptorIntrospection(t *testing.T) {
	d := param.String("query text")
	if d.Kind() != param.KindString || d.TypeName() != "string" || d.Description() != "query text" {
		t.Fatalf("metadata = %v %q %q", d.Kind(), d.TypeName(), d.Description())
	}
	if param.Int("").TypeName() != "int" || param.Bool("").TypeName() != "bool" {
		t.Fatal("unexpected type names")
	}
}

func TestOptionCompilation(t *testing.T) {
	for name, want := range map[string]string{
		"valid":                   "",
		"raw_func":                "cannot use",
		"int_option":              "cannot use",
		"missing_description":     "not enough arguments",
		"valid_typed":             "",
		"default_mismatch":        "cannot use",
		"validator_mismatch":      "cannot use",
		"of_validator_mismatch":   "does not match inferred type",
		"file_validator_mismatch": "cannot use",
		"arbitrary_option":        "cannot use",
	} {
		t.Run(name, func(t *testing.T) {
			out, err := exec.Command("go", "build", "-o", filepath.Join(t.TempDir(), "fixture"), "./testdata/optionapi/"+name).CombinedOutput()
			if want == "" {
				if err != nil {
					t.Fatalf("valid API failed: %v\n%s", err, out)
				}
				return
			}
			if err == nil || !strings.Contains(string(out), want) {
				t.Fatalf("expected compilation failure containing %q: %v\n%s", want, err, out)
			}
		})
	}
}

func TestDescriptionPrecedesValidators(t *testing.T) {
	a := param.String("a", validate.NonEmpty())
	b := param.String("b", validate.NonEmpty())
	if a.Description() != "a" || b.Description() != "b" || param.String("").Description() != "" {
		t.Fatal("description not carried verbatim")
	}
	for _, d := range []param.Descriptor[string]{a, b} {
		_, err := param.Prepare([]param.AnyDescriptor{d}, map[param.AnyDescriptor]param.RawValue{d: present("")})
		var ve *param.ValidationError
		if !errors.As(err, &ve) {
			t.Fatalf("err = %v, want ValidationError", err)
		}
	}
}

func TestInvalidOptionsPanicAtDeclaration(t *testing.T) {
	mustPanic(t, "nil option", func() { param.String("", nil) })
	mustPanic(t, "nil option after default", func() { param.Int("", param.Default(1), nil) })
	mustPanic(t, "foreign implementation", func() { param.String("", foreignOption{}) })
}

// foreignOption structurally satisfies Option[string] but is not one of the
// options param understands, so declaration rejects it.
type foreignOption struct{}

func (foreignOption) ParameterOption(string) {}

func TestNilValidatorsAreSkipped(t *testing.T) {
	var fn func(string) error
	d := param.String("", validate.Validator[string](nil), validate.Check(fn))
	ctx, err := prepare(t, []param.AnyDescriptor{d}, map[param.AnyDescriptor]param.RawValue{d: present("x")})
	if err != nil || param.Read(ctx, d) != "x" {
		t.Fatalf("err = %v", err)
	}
}

func TestCustomFunctionThroughCheck(t *testing.T) {
	sentinel := errors.New("custom rejected")
	d := param.String("", validate.NonEmpty(), validate.Check(func(s string) error {
		if s == "bad" {
			return sentinel
		}
		return nil
	}))
	parameters := []param.AnyDescriptor{d}
	if _, err := prepare(t, parameters, map[param.AnyDescriptor]param.RawValue{d: present("ok")}); err != nil {
		t.Fatal(err)
	}
	_, err := prepare(t, parameters, map[param.AnyDescriptor]param.RawValue{d: present("bad")})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want sentinel", err)
	}
}

func TestFileDescriptorKindsAndValidation(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/new.data"
	d := param.File("new output file", file.MustNotExist(), file.ParentExists(), file.Extension(".data"), param.Default(path))
	if d.Kind() != param.KindFile || d.Kind().String() != "file path" || d.Description() != "new output file" {
		t.Fatalf("metadata = %v %q", d.Kind(), d.Description())
	}
	parameters := []param.AnyDescriptor{d}
	ctx, err := prepare(t, parameters, nil)
	if err != nil || param.Read(ctx, d) != path {
		t.Fatalf("default prepare = %v", err)
	}
	_, err = prepare(t, parameters, map[param.AnyDescriptor]param.RawValue{d: present(dir + "/wrong.txt")})
	var ve *param.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v, want ValidationError", err)
	}
}

// -- parsing ------------------------------------------------------------

func TestPrepareParsesStringIntBool(t *testing.T) {
	name, age, active := param.String(""), param.Int(""), param.Bool("")
	ctx, err := prepare(t,
		[]param.AnyDescriptor{name, age, active},
		map[param.AnyDescriptor]param.RawValue{name: present("ada"), age: present("36"), active: present("true")})
	if err != nil {
		t.Fatal(err)
	}
	if param.Read(ctx, name) != "ada" || param.Read(ctx, age) != 36 || !param.Read(ctx, active) {
		t.Fatal("wrong values")
	}
}

func TestPrepareRejectsUnparsableAsValidationErrorWithIdentity(t *testing.T) {
	age := param.Int("Age in years")
	_, err := param.Prepare([]param.AnyDescriptor{age}, map[param.AnyDescriptor]param.RawValue{age: present("nope")})
	var ve *param.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v (%T)", err, err)
	}
	if ve.Param != param.AnyDescriptor(age) {
		t.Fatal("ValidationError must carry the descriptor identity")
	}
	var pe *param.ParseError
	if !errors.As(err, &pe) || pe.Raw != "nope" || pe.Param != param.AnyDescriptor(age) {
		t.Fatalf("ParseError cause missing: %v", err)
	}
	var numErr *strconv.NumError
	if !errors.As(err, &numErr) {
		t.Fatal("parser cause must be preserved")
	}
	if !strings.Contains(err.Error(), "Age in years") {
		t.Fatalf("message lacks description label: %v", err)
	}
	b := param.Bool("")
	_, err = param.Prepare([]param.AnyDescriptor{b}, map[param.AnyDescriptor]param.RawValue{b: present("maybe")})
	if !errors.As(err, &ve) {
		t.Fatalf("bool err = %v", err)
	}
}

func TestDefineTypeAndOfParseThenValidate(t *testing.T) {
	parseErr := errors.New("not an even integer")
	evenInt := param.DefineType("even integer", func(raw string) (int, error) {
		v, err := strconv.Atoi(raw)
		if err != nil || v%2 != 0 {
			return 0, parseErr
		}
		return v, nil
	})
	d := param.Of(evenInt, "", validate.Min(2))
	if d.Kind() != param.KindCustom || d.TypeName() != "even integer" {
		t.Fatalf("metadata = %v %q", d.Kind(), d.TypeName())
	}
	parameters := []param.AnyDescriptor{d}
	ctx, err := prepare(t, parameters, map[param.AnyDescriptor]param.RawValue{d: present("4")})
	if err != nil || param.Read(ctx, d) != 4 {
		t.Fatalf("valid = %v", err)
	}
	_, err = prepare(t, parameters, map[param.AnyDescriptor]param.RawValue{d: present("3")})
	var ve *param.ValidationError
	if !errors.As(err, &ve) || !errors.Is(err, parseErr) {
		t.Fatalf("parse failure = %v", err)
	}
	_, err = prepare(t, parameters, map[param.AnyDescriptor]param.RawValue{d: present("0")})
	if !errors.As(err, &ve) || errors.Is(err, parseErr) {
		t.Fatalf("validator failure = %v", err)
	}
}

func TestDefineTypeRejectsInvalidDefinitions(t *testing.T) {
	mustPanic(t, "empty name", func() { param.DefineType[int]("", func(string) (int, error) { return 0, nil }) })
	mustPanic(t, "nil parser", func() { param.DefineType[int]("integer", nil) })
	mustPanic(t, "zero type", func() { param.Of(param.Type[int]{}, "") })
}

func TestPathTypeConstructorsEnforceTheirSemantics(t *testing.T) {
	dir := t.TempDir()
	regular := dir + "/input.txt"
	if err := os.WriteFile(regular, []byte("input"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		d    param.Descriptor[string]
		ok   string
		bad  string
		kind param.Kind
	}{
		{"directory", param.Directory(""), dir, regular, param.KindDirectory},
		{"input file", param.InputFile(""), regular, dir, param.KindInputFile},
		{"output file", param.OutputFile(""), dir + "/new.txt", regular, param.KindOutputFile},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if tt.d.Kind() != tt.kind {
				t.Fatalf("Kind() = %v", tt.d.Kind())
			}
			parameters := []param.AnyDescriptor{tt.d}
			if _, err := prepare(t, parameters, map[param.AnyDescriptor]param.RawValue{tt.d: present(tt.ok)}); err != nil {
				t.Fatalf("valid = %v", err)
			}
			_, err := prepare(t, parameters, map[param.AnyDescriptor]param.RawValue{tt.d: present(tt.bad)})
			var ve *param.ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("invalid = %v", err)
			}
		})
	}
}

// -- descriptor-owned defaults ------------------------------------------------

func TestDescriptorsExposeDefaultPolicy(t *testing.T) {
	req, opt := param.Int(""), param.Int("", param.Default(7))
	if req.HasDefault() || req.Default() != nil || param.AnyDescriptor(req).HasDefault() {
		t.Fatalf("required = %v %v", req.HasDefault(), req.Default())
	}
	if !opt.HasDefault() || opt.Default() != 7 || param.AnyDescriptor(opt).Default() != 7 {
		t.Fatalf("optional = %v %v", opt.HasDefault(), opt.Default())
	}
}

func TestSameDefaultsNeedDistinctDescriptors(t *testing.T) {
	a, b := param.Int("", param.Default(10)), param.Int("", param.Default(20))
	for i, want := range []struct {
		d param.Descriptor[int]
		v int
	}{{a, 10}, {b, 20}, {a, 10}} {
		ctx, err := prepare(t, []param.AnyDescriptor{want.d}, nil)
		if err != nil || param.Read(ctx, want.d) != want.v {
			t.Fatalf("case %d: err=%v", i, err)
		}
	}
}

func TestPrepareRequiredMissingFails(t *testing.T) {
	q := param.String("")
	_, err := param.Prepare([]param.AnyDescriptor{q}, map[param.AnyDescriptor]param.RawValue{})
	var me *param.MissingValueError
	if !errors.As(err, &me) || me.Param != param.AnyDescriptor(q) {
		t.Fatalf("err = %v (%T)", err, err)
	}
}

func TestPrepareDefaultAndOverride(t *testing.T) {
	limit := param.Int("", param.Default(10))
	parameters := []param.AnyDescriptor{limit}
	ctx, err := prepare(t, parameters, nil)
	if err != nil || param.Read(ctx, limit) != 10 {
		t.Fatalf("default = %v", err)
	}
	ctx, err = prepare(t, parameters, map[param.AnyDescriptor]param.RawValue{limit: present("25")})
	if err != nil || param.Read(ctx, limit) != 25 {
		t.Fatalf("override = %v", err)
	}
}

func TestAbsentVersusExplicitEmpty(t *testing.T) {
	q := param.String("", param.Default("fallback"))
	parameters := []param.AnyDescriptor{q}
	ctx, err := prepare(t, parameters, map[param.AnyDescriptor]param.RawValue{q: present("")})
	if err != nil || param.Read(ctx, q) != "" {
		t.Fatalf("explicit empty = %v", err)
	}
	ctx, err = prepare(t, parameters, map[param.AnyDescriptor]param.RawValue{})
	if err != nil || param.Read(ctx, q) != "fallback" {
		t.Fatalf("absent = %v", err)
	}
	// Explicit empty is validated, not replaced by the default.
	v := param.String("", validate.NonEmpty(), param.Default("fallback"))
	_, err = prepare(t, []param.AnyDescriptor{v}, map[param.AnyDescriptor]param.RawValue{v: present("")})
	var ve *param.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("empty must be validated: %v", err)
	}
	// Present=false with a non-empty Value is still absent.
	ctx, err = prepare(t, parameters, map[param.AnyDescriptor]param.RawValue{q: {Value: "ignored"}})
	if err != nil || param.Read(ctx, q) != "fallback" {
		t.Fatalf("absent with stray value = %v", err)
	}
}

func TestZeroFalseAndEmptyDefaultsAreDefaults(t *testing.T) {
	n, b, s := param.Int("", param.Default(0)), param.Bool("", param.Default(false)), param.String("", param.Default(""))
	for _, d := range []param.AnyDescriptor{n, b, s} {
		if !d.HasDefault() {
			t.Fatalf("%v: zero default must be present", d.TypeName())
		}
	}
	ctx, err := prepare(t, []param.AnyDescriptor{n, b, s}, nil)
	if err != nil || param.Read(ctx, n) != 0 || param.Read(ctx, b) || param.Read(ctx, s) != "" {
		t.Fatalf("zero defaults = %v", err)
	}
	// Otherwise equivalent descriptors without a default are required.
	for _, d := range []param.AnyDescriptor{param.Int(""), param.Bool(""), param.String("")} {
		var me *param.MissingValueError
		if _, err := param.Prepare([]param.AnyDescriptor{d}, nil); !errors.As(err, &me) {
			t.Fatalf("%v: err = %v, want MissingValueError", d.TypeName(), err)
		}
	}
}

func TestDefaultsAreCheckedAgainstAllValidatorsRegardlessOfPosition(t *testing.T) {
	mustPanic(t, "default before validator", func() { param.Int("", param.Default(0), validate.Min(1)) })
	mustPanic(t, "default after validator", func() { param.Int("", validate.Min(1), param.Default(0)) })
	mustPanic(t, "default between validators", func() {
		param.Int("", validate.Min(0), param.Default(0), validate.Min(1))
	})
	param.Int("", param.Default(1), validate.Min(1))
}

func TestDefaultValidationFollowsInvariantsThenUserValidatorsOrder(t *testing.T) {
	var calls []string
	record := func(name string) validate.Validator[string] {
		return validate.Check(func(string) error { calls = append(calls, name); return nil })
	}
	dir := t.TempDir()
	// The regular-file invariant rejects a directory before the user validator runs.
	msg := panicMessage(t, func() {
		param.InputFile("", param.Default(dir), record("user"))
	})
	if len(calls) != 0 || !strings.Contains(msg, "regular file") {
		t.Fatalf("calls = %v, panic = %q", calls, msg)
	}
	// Later validators still cover a default declared first.
	param.String("", param.Default("x"), record("a"), record("b"))
	if strings.Join(calls, ",") != "a,b" {
		t.Fatalf("calls = %v, want a,b", calls)
	}
}

func panicMessage(t *testing.T, fn func()) (msg string) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic")
		}
		msg = fmt.Sprint(r)
	}()
	fn()
	return ""
}

func TestDuplicateDefaultsAreRejected(t *testing.T) {
	mustPanic(t, "different defaults", func() { param.Int("", param.Default(1), param.Default(2)) })
	mustPanic(t, "equal defaults", func() { param.Int("", param.Default(1), validate.Min(0), param.Default(1)) })
	mustPanic(t, "equal zero defaults", func() { param.String("", param.Default(""), param.Default("")) })
}

func TestInvalidDefaultPanicsAtConstruction(t *testing.T) {
	mustPanic(t, "invalid default", func() { param.Int("", validate.Min(0), param.Default(-1)) })
	dir := t.TempDir()
	regular := dir + "/input.txt"
	if err := os.WriteFile(regular, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustPanic(t, "directory default", func() { param.Directory("", param.Default(regular)) })
	mustPanic(t, "input file default", func() { param.InputFile("", param.Default(dir)) })
	mustPanic(t, "output file default", func() { param.OutputFile("", param.Default(regular)) })
}

func TestPrepareRejectsNilAndZeroDescriptors(t *testing.T) {
	mustPanic(t, "nil descriptor", func() { _, _ = param.Prepare([]param.AnyDescriptor{nil}, nil) })
	mustPanic(t, "zero descriptor", func() { _, _ = param.Prepare([]param.AnyDescriptor{param.Descriptor[int]{}}, nil) })
	mustPanic(t, "zero among valid", func() {
		_, _ = param.Prepare([]param.AnyDescriptor{param.Int("", param.Default(1)), param.Descriptor[string]{}}, nil)
	})
}

func TestPrepareRevalidatesFilesystemDefaultAtInvocation(t *testing.T) {
	path := t.TempDir() + "/result"
	d := param.OutputFile("", param.Default(path)) // valid at construction
	parameters := []param.AnyDescriptor{d}
	if err := os.WriteFile(path, []byte("occupied"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := param.Prepare(parameters, nil)
	var ve *param.ValidationError
	if !errors.As(err, &ve) || ve.Param != param.AnyDescriptor(d) {
		t.Fatalf("expected invocation validation failure, got %v", err)
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("cause not preserved: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := param.Prepare(parameters, nil); err != nil {
		t.Fatalf("default valid again: %v", err)
	}
}

type endpoint struct{ host string }

func TestCustomPointerDefaults(t *testing.T) {
	typ := param.DefineType("endpoint", func(s string) (*endpoint, error) { return &endpoint{s}, nil })
	d := param.Of(typ, "", param.Default[*endpoint](nil))
	parameters := []param.AnyDescriptor{d}
	if !d.HasDefault() {
		t.Fatal("typed nil default must count as present")
	}
	if got, ok := d.Default().(*endpoint); !ok || got != nil {
		t.Fatalf("Default() = %#v, want typed nil", d.Default())
	}
	ctx, err := prepare(t, parameters, nil)
	if err != nil || param.Read(ctx, d) != nil {
		t.Fatalf("nil default = %v", err)
	}
	ctx, err = prepare(t, parameters, map[param.AnyDescriptor]param.RawValue{d: present("example.test")})
	if err != nil || param.Read(ctx, d).host != "example.test" {
		t.Fatalf("custom parser = %v", err)
	}
	shared := &endpoint{"shared"}
	withShared := param.Of(typ, "", param.Default(shared))
	ctx, err = prepare(t, []param.AnyDescriptor{withShared}, nil)
	if err != nil || param.Read(ctx, withShared) != shared {
		t.Fatalf("pointer default must be retained by reference: %v", err)
	}
	// A nil pointer default runs through the validators like any other value.
	seen := 0
	checked := param.Of(typ, "", param.Default[*endpoint](nil), validate.Check(func(e *endpoint) error { seen++; return nil }))
	if _, err := prepare(t, []param.AnyDescriptor{checked}, nil); err != nil || seen != 2 {
		t.Fatalf("err = %v, validator calls = %d (construction and use)", err, seen)
	}
}

type shape interface{ area() int }

func TestNilInterfaceDefault(t *testing.T) {
	typ := param.DefineType("shape", func(string) (shape, error) { return nil, nil })
	d := param.Of(typ, "", param.Default[shape](nil))
	if !d.HasDefault() || d.Default() != nil {
		t.Fatalf("HasDefault = %v, Default = %#v", d.HasDefault(), d.Default())
	}
	ctx, err := prepare(t, []param.AnyDescriptor{d}, nil)
	if err != nil || param.Read(ctx, d) != nil {
		t.Fatalf("nil interface default = %v", err)
	}
	// A nil interface produced by the parser is valid input as well.
	ctx, err = prepare(t, []param.AnyDescriptor{d}, map[param.AnyDescriptor]param.RawValue{d: present("x")})
	if err != nil || param.Read(ctx, d) != nil {
		t.Fatalf("nil parsed value = %v", err)
	}
	required := param.Of(typ, "")
	var me *param.MissingValueError
	if _, err := param.Prepare([]param.AnyDescriptor{required}, nil); !errors.As(err, &me) {
		t.Fatalf("err = %v, want MissingValueError", err)
	}
}

// -- validators -----------------------------------------------------------

func TestValidatorsRunInDeclarationOrderAndFirstFailureWins(t *testing.T) {
	var order []string
	first := validate.Check(func(string) error { order = append(order, "first"); return errors.New("first failed") })
	second := validate.Check(func(string) error { order = append(order, "second"); return errors.New("second failed") })
	q := param.String("", first, second)
	_, err := param.Prepare([]param.AnyDescriptor{q}, map[param.AnyDescriptor]param.RawValue{q: present("x")})
	var ve *param.ValidationError
	if !errors.As(err, &ve) || ve.Err.Error() != "first failed" {
		t.Fatalf("err = %v", err)
	}
	if len(order) != 1 || order[0] != "first" {
		t.Fatalf("order = %v", order)
	}
}

func TestValidatorsRunSecondAfterFirstPasses(t *testing.T) {
	var order []string
	first := validate.Check(func(string) error { order = append(order, "first"); return nil })
	second := validate.Check(func(string) error { order = append(order, "second"); return errors.New("second failed") })
	q := param.String("", first, second)
	_, err := param.Prepare([]param.AnyDescriptor{q}, map[param.AnyDescriptor]param.RawValue{q: present("x")})
	if err == nil || !strings.Contains(err.Error(), "second failed") {
		t.Fatalf("err = %v", err)
	}
	if strings.Join(order, ",") != "first,second" {
		t.Fatalf("order = %v", order)
	}
}

func TestValidationApplySemanticsBackParamValidationFailure(t *testing.T) {
	first := validate.Check(func(int) error { return errors.New("first failed") })
	second := validate.Check(func(int) error { return errors.New("second failed") })
	direct := validate.Apply(7, first, second)
	d := param.Int("", first, second)
	_, err := param.Prepare([]param.AnyDescriptor{d}, map[param.AnyDescriptor]param.RawValue{d: present("7")})
	var ve *param.ValidationError
	if !errors.As(err, &ve) || ve.Err.Error() != direct.Error() {
		t.Fatalf("err = %v, want %v", err, direct)
	}
}

func TestSharedValidatorAcrossIndependentParams(t *testing.T) {
	sentinel := errors.New("bad")
	shared := validate.Check(func(s string) error {
		if s == "bad" {
			return sentinel
		}
		return nil
	})
	a, b := param.String("", shared), param.String("", shared)
	for _, d := range []param.Descriptor[string]{a, b} {
		_, err := param.Prepare([]param.AnyDescriptor{d}, map[param.AnyDescriptor]param.RawValue{d: present("bad")})
		if !errors.Is(err, sentinel) {
			t.Fatalf("err = %v", err)
		}
	}
}

// -- undeclared reads / programmer error contract --------------------------

// progErr mirrors the (error + Way2GoProgrammerError) shape without
// importing an execution package, since param must remain independently reusable.
type progErr interface {
	error
	Way2GoProgrammerError()
}

func TestReadOfUndeclaredParamPanicsWithProgrammerError(t *testing.T) {
	declared := param.String("")
	other := param.String("") // distinct identity, same name
	values, err := param.Prepare([]param.AnyDescriptor{declared}, map[param.AnyDescriptor]param.RawValue{
		declared: {Value: "hi", Present: true},
	})
	if err != nil {
		t.Fatalf("Prepare returned error: %v", err)
	}
	ctx := param.NewContext(context.Background(), values)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic reading an undeclared param")
		}
		err, ok := r.(error)
		if !ok {
			t.Fatalf("recovered value %v is not an error", r)
		}
		var pe progErr
		if !errors.As(err, &pe) {
			t.Fatalf("recovered error %v (%T) does not satisfy the programmer-error contract", err, err)
		}
		var ure *param.UndeclaredReadError
		if !errors.As(err, &ure) {
			t.Fatalf("recovered error %v (%T) is not a *param.UndeclaredReadError", err, err)
		}
	}()
	param.Read(ctx, other)
}

func TestReadWithoutAnyPreparedContextPanics(t *testing.T) {
	q := param.String("")
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic reading from a context with no prepared Values")
		}
	}()
	param.Read(context.Background(), q)
}

func TestOrdinaryInputErrorsDoNotSatisfyProgrammerErrorContract(t *testing.T) {
	cases := []error{
		&param.MissingValueError{Param: param.String("")},
		&param.ParseError{Param: param.Int(""), Raw: "x", Err: errors.New("bad")},
		&param.ValidationError{Param: param.Int(""), Err: errors.New("bad")},
	}
	for _, err := range cases {
		var pe progErr
		if errors.As(err, &pe) {
			t.Fatalf("%T unexpectedly satisfies the programmer-error contract", err)
		}
	}
}

// -- descriptor identity ---------------------------------------------------

func TestDistinctConstructorCallsHaveDistinctIdentity(t *testing.T) {
	a := param.String("")
	b := param.String("")
	var anyA, anyB param.AnyDescriptor = a, b
	if anyA == anyB {
		t.Fatalf("two distinct param.String calls must not be the same identity")
	}
}

func TestCopyOfDescriptorSharesIdentity(t *testing.T) {
	a := param.String("")
	b := a
	var anyA, anyB param.AnyDescriptor = a, b
	if anyA != anyB {
		t.Fatalf("copying a Descriptor value must preserve identity")
	}
}

func TestUndeclaredReadCarriesIdentityAndDoesNotLeakValues(t *testing.T) {
	declared := param.String("declared")
	other := param.String("other")
	values, err := param.Prepare([]param.AnyDescriptor{declared}, map[param.AnyDescriptor]param.RawValue{declared: present("secret")})
	if err != nil {
		t.Fatal(err)
	}
	ctx := param.NewContext(context.Background(), values)
	defer func() {
		ure, ok := recover().(*param.UndeclaredReadError)
		if !ok || ure.Param != param.AnyDescriptor(other) {
			t.Fatalf("recovered %v", ure)
		}
		if strings.Contains(ure.Error(), "secret") || !strings.Contains(ure.Error(), "other") {
			t.Fatalf("message = %q", ure.Error())
		}
	}()
	param.Read(ctx, other)
}
