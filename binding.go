package cli

import (
	"github.com/go-way2go/cli/internal/command"
	"github.com/go-way2go/cli/param"
)

// WithOption binds p as a named option supplied through name and any aliases.
// A single ASCII letter is a short option (-n); any longer name is a long
// option (--name). Names are listed in declaration order, and the first is
// used in diagnostics. Requiredness and defaults belong to p: it is optional
// exactly when p declares a default. Declaration panics on a zero descriptor
// or an empty, invalid, repeated or reserved (help, h) name.
func WithOption(p param.AnyDescriptor, name string, aliases ...string) BindingOption {
	return binding{command.NewOption(p, name, aliases)}
}

// WithArgument binds p as the next positional argument, named for help and
// diagnostics only. Positionals are matched in declaration order and required
// ones must precede those whose parameter declares a default. Declaration
// panics on a zero descriptor or invalid name.
func WithArgument(p param.AnyDescriptor, name string) BindingOption {
	return binding{command.NewArgument(p, name)}
}

// binding is the root's BindingOption: a validated engine binding.
type binding struct{ b command.Binding }

func (binding) bindingOption() {}
func (b binding) applyCommand(c *command.Builder) {
	c.Bind(b.b)
}
