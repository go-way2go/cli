// Package command implements CLI command declarations, binding validation,
// effective binding metadata, parser construction, help, execution and
// outcome classification. The cli root package wraps its values behind a small
// public facade; nothing here is public API.
package command

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-way2go/cli/internal/invocation"
	"github.com/go-way2go/cli/param"
)

// Wrapper wraps a handler; the first declared wrapper is outermost.
type Wrapper func(invocation.Handler) invocation.Handler

// Binding is one effective use of a parameter by a command or middleware. It
// owns one descriptor and either an ordered list of option names (single
// ASCII letters are short options, longer names are long options) or one
// positional name. Requiredness and defaults belong to the descriptor.
type Binding struct {
	desc     param.AnyDescriptor
	options  []string
	argument string
}

func checkDescriptor(p param.AnyDescriptor) {
	if p == nil || isZeroDescriptor(p) {
		panic("cli: binding requires a non-zero param descriptor")
	}
}

// isZeroDescriptor reports whether p is a zero Descriptor value. Its state is
// unexported, so a zero descriptor is recognized by its accessors panicking.
func isZeroDescriptor(p param.AnyDescriptor) (zero bool) {
	defer func() {
		if recover() != nil {
			zero = true
		}
	}()
	_ = p.Kind()
	return false
}

// NewOption binds p to name and aliases as option names. It panics on a
// zero descriptor, or an invalid, reserved or repeated name.
func NewOption(p param.AnyDescriptor, name string, aliases []string) Binding {
	checkDescriptor(p)
	b := Binding{desc: p}
	label := param.Label(p)
	for _, n := range append([]string{name}, aliases...) {
		if err := validateOptionName(n); err != nil {
			panic(fmt.Sprintf("cli: binding for %s: %v", label, err))
		}
		if isShort(n) && n == "h" || n == "help" {
			panic(fmt.Sprintf("cli: binding for %s: %s is reserved for help", label, optionLabel(n)))
		}
		for _, e := range b.options {
			if e == n {
				panic(fmt.Sprintf("cli: binding for %s: repeated option name %q", label, n))
			}
		}
		b.options = append(b.options, n)
	}
	return b
}

// NewArgument binds p to one positional name. It panics on a zero descriptor
// or an invalid name.
func NewArgument(p param.AnyDescriptor, name string) Binding {
	checkDescriptor(p)
	if err := validateToken("argument", name); err != nil {
		panic(fmt.Sprintf("cli: binding for %s: %v", param.Label(p), err))
	}
	return Binding{desc: p, argument: name}
}

func isShort(n string) bool { return utf8.RuneCountInString(n) == 1 }

func optionLabel(n string) string {
	if isShort(n) {
		return "-" + n
	}
	return "--" + n
}

func validateToken(kind, name string) error {
	if name == "" || strings.HasPrefix(name, "-") || strings.ContainsAny(name, "=\x00") || strings.IndexFunc(name, unicode.IsSpace) >= 0 {
		return fmt.Errorf("%s name %q must be a nonempty token without leading dash, whitespace, NUL or '='", kind, name)
	}
	return nil
}

func validateOptionName(name string) error {
	if err := validateToken("option", name); err != nil {
		return err
	}
	if isShort(name) && !(name[0] >= 'a' && name[0] <= 'z' || name[0] >= 'A' && name[0] <= 'Z') {
		return fmt.Errorf("short option name %q must be a single ASCII letter", name)
	}
	return nil
}

func (b Binding) descriptor() param.AnyDescriptor { return b.desc }
func (b Binding) isBool() bool                    { return b.desc.Kind() == param.KindBool }
func (b Binding) hasDefault() bool                { return b.desc.HasDefault() }

// Positional reports whether b is supplied as a positional argument.
func (b Binding) Positional() bool { return b.argument != "" }

// Argument returns b's positional name, or "" for a named binding.
func (b Binding) Argument() string { return b.argument }

// label is the stable external name used in diagnostics: the first declared
// public name.
func (b Binding) label() string {
	if b.argument != "" {
		return "argument " + b.argument
	}
	return optionLabel(b.options[0])
}

// sameNames reports whether a and b declare the same names, ignoring order.
func sameNames(a, b Binding) bool {
	if a.argument != b.argument || len(a.options) != len(b.options) {
		return false
	}
	set := map[string]bool{}
	for _, n := range a.options {
		set[n] = true
	}
	for _, n := range b.options {
		if !set[n] {
			return false
		}
	}
	return true
}

// Builder accumulates one command's declaration options.
type Builder struct {
	bindings []Binding
	wrappers []Wrapper
	// inMiddleware is set while applying a middleware's bindings, which may
	// not contribute positional arguments.
	inMiddleware bool
}

// Bind adds bd as the effective binding for its parameter identity. A repeat
// with the same name set deduplicates regardless of order; anything else that
// reuses the identity or an external token conflicts.
func (b *Builder) Bind(bd Binding) {
	if bd.Positional() && b.inMiddleware {
		panic(fmt.Sprintf("cli: middleware cannot declare positional argument %q", bd.argument))
	}
	for _, e := range b.bindings {
		if e.descriptor() == bd.descriptor() {
			if !sameNames(e, bd) {
				panic(fmt.Sprintf("cli: conflicting bindings for %s (%s versus %s)", param.Label(bd.descriptor()), e.label(), bd.label()))
			}
			return
		}
		for _, n := range bd.options {
			for _, en := range e.options {
				if n == en {
					panic(fmt.Sprintf("cli: option %s is already declared by a different param", optionLabel(n)))
				}
			}
		}
		if bd.argument != "" && e.argument == bd.argument {
			panic(fmt.Sprintf("cli: argument %q is already declared by a different param", bd.argument))
		}
	}
	b.bindings = append(b.bindings, bd)
}

// AddMiddleware appends a wrapper after applying its named bindings.
func (b *Builder) AddMiddleware(wrap Wrapper, bindings []Binding) {
	b.inMiddleware = true
	for _, bd := range bindings {
		b.Bind(bd)
	}
	b.inMiddleware = false
	b.wrappers = append(b.wrappers, wrap)
}

// Definition is a validated, nameless command: its effective bindings,
// metadata and middleware-wrapped handler. It is not a Node; a registration
// (see Command) pairs it with a name. Its zero value is invalid.
type Definition struct {
	description string
	named       []Binding
	positional  []Binding
	uses        []param.AnyDescriptor
	handler     invocation.Handler
}

// IsZero reports whether d was not created by NewDefinition.
func (d Definition) IsZero() bool { return d.handler == nil }

// NewDefinition validates and composes a nameless command definition. Invalid
// declarations panic immediately; no handler runs.
func NewDefinition(description string, handler invocation.Handler, apply func(*Builder)) Definition {
	if handler == nil {
		panic("cli: handler must not be nil")
	}
	b := &Builder{}
	if apply != nil {
		apply(b)
	}
	d := Definition{description: description, handler: handler}
	for _, bd := range b.bindings {
		d.uses = append(d.uses, bd.desc)
		if bd.Positional() {
			d.positional = append(d.positional, bd)
		} else {
			d.named = append(d.named, bd)
		}
	}
	validateArgOrder(d.positional)
	for i := len(b.wrappers) - 1; i >= 0; i-- {
		d.handler = b.wrappers[i](d.handler)
		if d.handler == nil {
			panic("cli: middleware returned a nil handler")
		}
	}
	return d
}

// validateArgOrder enforces that every required positional binding precedes
// every optional one, in effective declaration order.
func validateArgOrder(positional []Binding) {
	seenOptional := ""
	for _, p := range positional {
		if p.hasDefault() {
			seenOptional = p.argument
			continue
		}
		if seenOptional != "" {
			panic(fmt.Sprintf(
				"cli: required argument %q must be declared before optional argument %q",
				p.argument, seenOptional,
			))
		}
	}
}

// TrimmedNonEmpty returns s without surrounding whitespace, or panics with
// panicMsg if nothing remains.
func TrimmedNonEmpty(panicMsg string, s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		panic(panicMsg)
	}
	return trimmed
}
