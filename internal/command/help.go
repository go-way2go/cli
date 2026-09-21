package command

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/go-way2go/cli/param"
)

// bindingError reports an input error under the parameter's actual external
// label. It unwraps to the original param error, so errors.Is/As still reach
// param.MissingValueError, param.ValidationError and their causes.
type bindingError struct {
	label string
	msg   string
	err   error
}

func (e *bindingError) Error() string { return e.label + ": " + e.msg }
func (e *bindingError) Unwrap() error { return e.err }

// externalize rewrites a param preparation error to name the binding rather
// than the descriptor. Unrecognized errors are returned unchanged.
func (d Definition) externalize(err error) error {
	var (
		missing *param.MissingValueError
		invalid *param.ValidationError
		desc    param.AnyDescriptor
		msg     string
	)
	switch {
	case errors.As(err, &missing):
		desc, msg = missing.Param, "value is required"
	case errors.As(err, &invalid):
		desc, msg = invalid.Param, invalid.Err.Error()
		var pe *param.ParseError
		if errors.As(invalid.Err, &pe) {
			msg = pe.Err.Error()
		}
	default:
		return err
	}
	for _, b := range d.named {
		if b.descriptor() == desc {
			return &bindingError{b.label(), msg, err}
		}
	}
	for _, b := range d.positional {
		if b.descriptor() == desc {
			return &bindingError{b.label(), msg, err}
		}
	}
	return err
}

// help renders the command's help from its effective bindings alone. It never
// prepares input or runs a handler.
func (d Definition) help(path string) string {
	var sb strings.Builder
	if d.description != "" {
		sb.WriteString(d.description + "\n\n")
	}
	usage := path
	if len(d.named) > 0 {
		usage += " [options]"
	}
	for _, b := range d.positional {
		if b.hasDefault() {
			usage += " [" + b.argument + "]"
		} else {
			usage += " <" + b.argument + ">"
		}
	}
	sb.WriteString("Usage:\n  " + usage + "\n")

	if len(d.positional) > 0 {
		sb.WriteString("\nArguments:\n")
		var rows [][2]string
		for _, b := range d.positional {
			rows = append(rows, [2]string{b.argument + " <" + b.descriptor().TypeName() + ">", bindingNote(b)})
		}
		writeRows(&sb, rows)
	}
	if len(d.named) > 0 {
		sb.WriteString("\nOptions:\n")
		var rows [][2]string
		for _, b := range d.named {
			rows = append(rows, [2]string{optionSpelling(b), bindingNote(b)})
		}
		writeRows(&sb, rows)
	}
	return sb.String()
}

// optionSpelling lists all of b's public names in declaration order.
func optionSpelling(b Binding) string {
	names := make([]string, len(b.options))
	for i, n := range b.options {
		names[i] = optionLabel(n)
	}
	s := strings.Join(names, ", ")
	if !b.isBool() {
		s += " <" + b.descriptor().TypeName() + ">"
	}
	return s
}

// bindingNote combines a parameter's description with its required/default
// state. A required Boolean is documented as required, never as false.
func bindingNote(b Binding) string {
	var parts []string
	if desc := b.descriptor().Description(); desc != "" {
		parts = append(parts, desc)
	}
	if b.hasDefault() {
		def := b.desc.Default()
		if isNil(def) {
			parts = append(parts, "(optional, default nil)")
		} else if s, ok := def.(string); ok {
			parts = append(parts, fmt.Sprintf("(optional, default %q)", s))
		} else {
			parts = append(parts, fmt.Sprintf("(optional, default %v)", def))
		}
	} else {
		parts = append(parts, "(required)")
	}
	return strings.Join(parts, " ")
}

func isNil(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface, reflect.Func, reflect.Chan:
		return rv.IsNil()
	}
	return false
}

func writeRows(sb *strings.Builder, rows [][2]string) {
	width := 0
	for _, r := range rows {
		if len(r[0]) > width {
			width = len(r[0])
		}
	}
	for _, r := range rows {
		fmt.Fprintf(sb, "  %-*s  %s\n", width, r[0], r[1])
	}
}
