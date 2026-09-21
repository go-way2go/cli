// Package param implements the target-neutral, declarative Param core: typed,
// nameless parameter descriptors (identity, type, description, parser and
// validators and an optional static default), preparation of raw external
// input into a validated typed value set, and param.Read access to that
// prepared set from within a handler.
//
// A descriptor carries no external name. It owns its requiredness: without a
// Default option it is required, with exactly one it is optional, including
// zero, false, empty and typed nil defaults. Different defaults require
// different descriptors. Presence and value are distinct: an explicitly
// supplied empty string is present and is validated like any other value,
// never silently replaced by a default.
//
// This package has no knowledge of Web or CLI. Target packages resolve raw
// values from query strings, form fields, CLI options or positional arguments,
// then call Prepare with the descriptors and the resulting param.RawValue set.
package param

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/go-way2go/cli/file"
	"github.com/go-way2go/cli/validate"
)

// Kind identifies a Param descriptor's declared type.
type Kind int

const (
	// KindString identifies a param.String descriptor.
	KindString Kind = iota + 1
	// KindInt identifies a param.Int descriptor.
	KindInt
	// KindBool identifies a param.Bool descriptor.
	KindBool
	// KindFile identifies a param.File descriptor. Its external and Go value
	// are both strings; the distinct kind lets documentation render it as a
	// file path rather than a generic string.
	KindFile
	// KindDirectory identifies an existing directory path.
	KindDirectory
	// KindInputFile identifies an existing regular file path.
	KindInputFile
	// KindOutputFile identifies a new file path whose parent exists.
	KindOutputFile
	// KindCustom identifies a descriptor constructed with Of.
	KindCustom
)

// String returns a human-readable name for k, e.g. "string".
func (k Kind) String() string {
	switch k {
	case KindString:
		return "string"
	case KindInt:
		return "int"
	case KindBool:
		return "bool"
	case KindFile:
		return "file path"
	case KindDirectory:
		return "directory"
	case KindInputFile:
		return "input file"
	case KindOutputFile:
		return "output file"
	case KindCustom:
		return "custom"
	default:
		return "unknown"
	}
}

// core holds a descriptor's shared state. It is unexported so AnyDescriptor
// can only be implemented by types defined in this package: identity is the
// pointer to a core, which is what makes two Descriptor values (of any T)
// comparable as the "same Param" or "different Params".
type core struct {
	kind        Kind
	typeName    string
	description string
	parseRaw    func(string) (any, error)
	validateAny func(any) error
	// hasDefault is stored separately from def so that zero, false, empty
	// and nil defaults are still present defaults.
	hasDefault bool
	def        any
}

// label renders a descriptor for diagnostics without an external name.
func (c *core) label() string {
	if c.description != "" {
		return fmt.Sprintf("%s %q", c.typeName, c.description)
	}
	return c.typeName
}

// AnyDescriptor is the type-erased view of a Param descriptor (as returned by
// String, Int, Bool, ...), used wherever a Param must be stored, compared or
// introspected without its type parameter.
//
// Only param's own descriptor types implement AnyDescriptor: the unexported
// core method seals the interface to this package, so descriptor identity
// can be trusted by caller deduplication and conflict checks.
type AnyDescriptor interface {
	// Kind reports the Param's declared type.
	Kind() Kind
	// TypeName returns the generic, documentable name of the value type.
	TypeName() string
	// Description returns the Param's optional human-readable description.
	Description() string
	// HasDefault reports whether the Param declares a static default. It
	// distinguishes an absent default from a nil one.
	HasDefault() bool
	// Default returns the static default, or nil if HasDefault is false. A
	// typed nil default is returned as an any holding the typed nil.
	Default() any

	core() *core
}

// Descriptor is an identity-bearing, typed Param descriptor. Two Descriptor
// values are the same Param if and only if they were produced by the same
// constructor call (or a copy thereof); calling String twice produces two
// distinct identities even though their options match.
//
// Construct one with String, Int, Bool, File, Directory, InputFile,
// OutputFile or Of.
type Descriptor[T any] struct {
	c *core
}

// Kind reports the Param's declared type.
func (d Descriptor[T]) Kind() Kind { return d.c.kind }

// TypeName returns the generic, documentable name of d's value type.
func (d Descriptor[T]) TypeName() string { return d.c.typeName }

// Description returns the Param's optional human-readable description.
func (d Descriptor[T]) Description() string { return d.c.description }

// HasDefault reports whether d declares a static default.
func (d Descriptor[T]) HasDefault() bool { return d.c.hasDefault }

// Default returns d's static default, or nil if HasDefault is false. The
// default is retained by reference; callers must not mutate it.
func (d Descriptor[T]) Default() any {
	if !d.c.hasDefault {
		return nil
	}
	return d.c.def
}

func (d Descriptor[T]) core() *core { return d.c }

var _ AnyDescriptor = Descriptor[string]{}

// Option configures a Param descriptor of value type T at construction
// time. It is a structural marker interface whose method signature includes
// T: validate.Validator[T] and Default[T] implement it, while validators or
// defaults of another type, raw functions and arbitrary values do not
// compile. The marker is never called. Use validate.Check to adapt a plain
// func(T) error.
type Option[T any] interface {
	// ParameterOption marks a value as usable beside a descriptor
	// constructor's other options.
	ParameterOption(T)
}

type defaultOption[T any] struct{ value T }

func (defaultOption[T]) ParameterOption(T) {}

// Default declares a static default for a Param, which makes it optional. A
// descriptor without Default is required. Zero, false, empty and typed nil
// values are all present defaults.
//
// A descriptor accepts at most one Default, wherever it appears among its
// options. The default is validated against every declared validator at
// construction and again whenever it is used by Prepare. It is retained by
// reference, so callers must not mutate it after declaration.
func Default[T any](value T) Option[T] { return defaultOption[T]{value: value} }

// String declares a string Param. An empty description means none; the same
// holds for every constructor below.
func String(description string, opts ...Option[string]) Descriptor[string] {
	return newDescriptor(KindString, "string", parseString, description, opts)
}

// Int declares an int Param, parsed with strconv.Atoi.
func Int(description string, opts ...Option[int]) Descriptor[int] {
	return newDescriptor(KindInt, "int", parseInt, description, opts)
}

// Bool declares a bool Param, parsed with strconv.ParseBool.
func Bool(description string, opts ...Option[bool]) Descriptor[bool] {
	return newDescriptor(KindBool, "bool", parseBool, description, opts)
}

// File declares a file-path Param. Its value is a path string; File neither
// reads nor writes that path while constructing or preparing the Param.
// Attach validators such as file.Exists or file.MustNotExist when the command
// needs preflight feedback.
//
// File has String's semantics, but its Kind renders as "file path" for
// descriptor consumers and documentation.
func File(description string, opts ...Option[string]) Descriptor[string] {
	return newDescriptor(KindFile, "file path", parseString, description, opts)
}

// Directory declares a path to an existing directory.
func Directory(description string, opts ...Option[string]) Descriptor[string] {
	return newDescriptor(KindDirectory, "directory", parseString, description, opts, file.Directory())
}

// InputFile declares a path to an existing regular file.
func InputFile(description string, opts ...Option[string]) Descriptor[string] {
	return newDescriptor(KindInputFile, "input file", parseString, description, opts, file.RegularFile())
}

// OutputFile declares a path for a file that does not yet exist and whose
// parent directory exists. WriteNew remains the race-safe authority for the
// subsequent creation.
func OutputFile(description string, opts ...Option[string]) Descriptor[string] {
	return newDescriptor(KindOutputFile, "output file", parseString, description, opts, file.MustNotExist(), file.ParentExists())
}

// Parser converts one raw external value into T.
//
// A parser error is reported by Prepare as a ValidationError for the
// concrete descriptor, because both parser rejection and validator rejection
// are invalid user input at the target boundary. The original error remains
// available through errors.Is/errors.As on ValidationError.
type Parser[T any] func(string) (T, error)

// Type is a reusable, documentable parameter type. Construct it with
// DefineType and declare a concrete Param from it with Of.
type Type[T any] struct {
	typeName string
	parse    Parser[T]
}

// DefineType defines a reusable parameter type with a generic name and raw
// input parser. typeName names the type itself (for example "BIP-39 batch
// mnemonic"), not an individual parameter.
func DefineType[T any](typeName string, parse Parser[T]) Type[T] {
	typeName = strings.TrimSpace(typeName)
	if typeName == "" {
		panic("param: type name must not be empty")
	}
	if parse == nil {
		panic("param: type parser must not be nil")
	}
	return Type[T]{typeName: typeName, parse: parse}
}

// Of declares a Param of custom type typ with the given description (which
// may be empty). Validators supplied in opts run only after typ has parsed the
// raw value successfully.
func Of[T any](typ Type[T], description string, opts ...Option[T]) Descriptor[T] {
	if typ.typeName == "" || typ.parse == nil {
		panic("param: Type must be created with DefineType")
	}
	return newDescriptor(KindCustom, typ.typeName, typ.parse, description, opts)
}

func parseString(s string) (string, error) { return s, nil }

func parseInt(s string) (int, error) {
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid int value %q: %w", s, err)
	}
	return v, nil
}

func parseBool(s string) (bool, error) {
	v, err := strconv.ParseBool(s)
	if err != nil {
		return false, fmt.Errorf("invalid bool value %q: %w", s, err)
	}
	return v, nil
}

// newDescriptor builds a descriptor. invariants are type-owned validators
// that run before caller validators. All options are collected before the
// default is validated, so Default's position does not change which
// validators apply. An unsupported Option implementation, nil option,
// duplicate default or invalid default panics at declaration even if no value
// is ever prepared.
func newDescriptor[T any](kind Kind, typeName string, parse Parser[T], description string, opts []Option[T], invariants ...validate.Validator[T]) Descriptor[T] {
	validators := append([]validate.Validator[T](nil), invariants...)
	var (
		hasDefault bool
		def        T
	)
	for i, opt := range opts {
		switch o := opt.(type) {
		case nil:
			panic(fmt.Sprintf("param: %s option %d must not be nil", typeName, i+1))
		case validate.Validator[T]:
			validators = append(validators, o)
		case defaultOption[T]:
			if hasDefault {
				panic(fmt.Sprintf("param: %s option %d declares a second default", typeName, i+1))
			}
			hasDefault, def = true, o.value
		default:
			panic(fmt.Sprintf("param: %s option %d has unsupported type %T", typeName, i+1, opt))
		}
	}

	c := &core{
		kind:        kind,
		typeName:    typeName,
		description: description,
		parseRaw: func(raw string) (any, error) {
			v, err := parse(raw)
			if err != nil {
				return nil, err
			}
			return v, nil
		},
		validateAny: func(v any) error {
			// A nil any is a nil interface-valued T, not a foreign type.
			typed, ok := v.(T)
			if !ok && v != nil {
				panic(fmt.Sprintf("param: %s descriptor applied to a value of type %T", typeName, v))
			}
			return validate.Apply(typed, validators...)
		},
	}
	if hasDefault {
		if err := c.validateAny(def); err != nil {
			panic(fmt.Sprintf("param: invalid default for %s: %v", c.label(), err))
		}
		c.hasDefault, c.def = true, def
	}
	return Descriptor[T]{c: c}
}
