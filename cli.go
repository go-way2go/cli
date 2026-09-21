// Package cli provides declarative commands, groups, concrete invocation
// contexts, typed parameter binding and CLI middleware. Define creates a
// reusable, nameless command definition, Command registers it under a name,
// New assembles an App for injectable execution, and Run is the process entry
// point. Parser types remain internal.
package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/go-way2go/cli/internal/command"
	"github.com/go-way2go/cli/internal/invocation"
	"github.com/go-way2go/cli/internal/process"
)

// CommandOption configures a command using CLI-owned declarations.
type CommandOption interface{ applyCommand(*command.Builder) }

// BindingOption contributes parameters to a command or middleware.
type BindingOption interface {
	CommandOption
	bindingOption()
}

// CommandDefinition is a validated, nameless command: its description,
// handler, bindings and middleware. It is not a Node; register it under a name
// with Command. One definition may be registered any number of times.
type CommandDefinition struct {
	def command.Definition
}

// Define declares a reusable command definition. description is shown in help;
// an empty string means none. Invalid declarations (a nil handler, invalid
// middleware, conflicting bindings, a required positional argument after an
// optional one) panic immediately, and no handler runs. Middleware is composed
// once, here, not per registration.
func Define(description string, handler HandlerFunc, options ...CommandOption) CommandDefinition {
	return CommandDefinition{command.NewDefinition(description, toEngine(handler), func(b *command.Builder) {
		for _, opt := range options {
			opt.applyCommand(b)
		}
	})}
}

type commandNode struct{ n command.Node }

func (c commandNode) node() command.Node { return c.n }

// Command registers definition as a command called name. It panics on an empty
// name or a zero CommandDefinition (one not created by Define). The
// registration does not modify definition, so it may be registered again under
// other names or groups.
func Command(name string, definition CommandDefinition) Node {
	return commandNode{command.Command(name, definition.def)}
}

// MiddlewareFunc wraps a CLI handler. The first declared wrapper is outermost.
type MiddlewareFunc func(HandlerFunc) HandlerFunc

// MiddlewareDefinition holds a wrapper and its parameter bindings.
type MiddlewareDefinition struct {
	wrap     MiddlewareFunc
	bindings []BindingOption
}

// DefineMiddleware declares CLI middleware with optional parameter contributions.
// name identifies middleware in declaration diagnostics. A nil wrapper is
// rejected immediately.
func DefineMiddleware(name string, wrap MiddlewareFunc, bindings ...BindingOption) MiddlewareDefinition {
	if wrap == nil {
		panic("cli: middleware wrapper must not be nil")
	}
	for _, o := range bindings {
		if b, ok := o.(binding); ok && b.b.Positional() {
			panic(fmt.Sprintf("cli: middleware %q cannot declare positional argument %q", name, b.b.Argument()))
		}
	}
	return MiddlewareDefinition{wrap: wrap, bindings: append([]BindingOption(nil), bindings...)}
}

// WithMiddleware adds middleware in declaration order.
func WithMiddleware(definitions ...MiddlewareDefinition) CommandOption {
	return middlewareOption(append([]MiddlewareDefinition(nil), definitions...))
}

type middlewareOption []MiddlewareDefinition

func (m middlewareOption) applyCommand(b *command.Builder) {
	for _, d := range m {
		if d.wrap == nil {
			panic("cli: middleware wrapper must not be nil")
		}
		bindings := make([]command.Binding, len(d.bindings))
		for i, o := range d.bindings {
			bindings[i] = o.(binding).b
		}
		wrap := d.wrap
		b.AddMiddleware(func(next invocation.Handler) invocation.Handler {
			return toEngine(wrap(fromEngine(next)))
		}, bindings)
	}
}

// Node is a member of a CLI command tree: either a registered command returned
// by Command or a group returned by Group. Its methods are unexported, so the
// only values that satisfy it are the ones this package itself produces.
type Node interface {
	node() command.Node
}

type groupNode struct{ n command.Node }

func (g groupNode) node() command.Node { return g.n }

// Group declares a named, nested command group. Its children may be further
// groups or commands in any combination; only commands are leaves. Group
// panics if name is empty, or if two children share a name: both are
// declaration-time conflicts, detected when the tree is declared rather than
// when it is later executed.
func Group(name string, children ...Node) Node {
	return groupNode{command.Group(name, nodes(children)...)}
}

func nodes(children []Node) []command.Node {
	out := make([]command.Node, len(children))
	for i, c := range children {
		out[i] = c.node()
	}
	return out
}

// App is an assembled CLI command tree. It exposes no parser configuration:
// the only operations are Execute, for programmatic execution with explicit
// arguments and streams, and Run, the real-process entry point.
type App struct {
	app command.App
}

// New assembles registered commands and groups into an App without executing
// any handler or starting a process. As with Group, two top-level nodes sharing
// a name panic during declaration.
func New(children ...Node) App {
	return App{command.New(nodes(children)...)}
}

// Run is shorthand for New(nodes...).Run().
func Run(children ...Node) { New(children...).Run() }

// Execute parses args against a's command tree and dispatches to the resolved
// command, exactly as a real CLI invocation would, but under full caller
// control: ctx seeds the execution context (nil is treated as
// context.Background()), in is the stdin source, out and err are the
// stdout/stderr sinks handlers observe through their Context, and the returned
// int is the exit code this execution maps to. Execute never calls os.Exit;
// that is Run's job, and it is safe to call repeatedly and concurrently.
//
// A nil out or err is normalized to io.Discard, so a caller passing a literal
// nil never causes a nil-writer panic. Every declared parameter is resolved and
// validated before any middleware or the handler runs. Exit code mapping is
// fixed:
//
//   - OK maps to 0;
//   - NOK maps to 1, silently;
//   - Error(err) maps to 1 for ordinary errors, printing err to stderr once,
//     prefixed with the dispatched command's name (for example
//     "generate: failed to seal batch: ...");
//   - a flag, argument, parameter or marked interactive input error (missing,
//     unparsable or validator-rejected value; an unmatched command; too many
//     positional arguments) maps to 2;
//   - a recovered Way2Go programmer error maps to 1. An unrelated panic is
//     re-panicked out of Execute, not recovered.
func (a App) Execute(ctx context.Context, args []string, in io.Reader, out, err io.Writer) int {
	return a.app.Execute(ctx, args, in, out, err)
}

// Run executes a against os.Args[1:] with the process's standard streams and
// terminates the process with the resulting exit code. On SIGINT or SIGTERM it
// cancels the invocation context, interrupts pending standard-input reads,
// waits for execution to return and exits with 130 or 143 respectively.
// Production programs call Run; tests call Execute to keep control of
// arguments, input, output and the process.
func (a App) Run() { process.Run(a.app.Execute) }
