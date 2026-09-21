package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/go-way2go/cli/internal/invocation"
	"github.com/go-way2go/cli/param"
)

// Node is a member of a command tree: a registered command or a group.
type Node interface {
	Name() string
	Build(result *Result) *cobra.Command
}

// Result carries the single outcome produced by whichever leaf command an
// Execute call ends up dispatching to, along with the name of that command
// (needed to prefix a reported error). A fresh Result, and a fresh
// *cobra.Command tree built with it, is created for every Execute call, so
// concurrent Execute calls on the same App never share mutable state.
type Result struct {
	in          io.Reader
	out, err    io.Writer
	outcome     invocation.Outcome
	hasOutcome  bool
	commandName string
}

// command is a registered node: a validated name paired with a Definition.
// Registrations share the definition and handler, but each Execute call builds
// independent parser state. Handler closures and reference defaults may be shared.
type command struct {
	name string
	def  Definition
}

// Command registers def under name. It panics on an empty name or a zero
// definition.
func Command(name string, def Definition) Node {
	name = TrimmedNonEmpty("cli: command name must not be empty", name)
	if def.IsZero() {
		panic(fmt.Sprintf("cli: command %q: definition must be created with Define", name))
	}
	return command{name: name, def: def}
}

// Name returns the command's registered name.
func (c command) Name() string { return c.name }

type groupNode struct {
	group    string
	children []Node
}

func (g groupNode) Name() string { return g.group }

func (g groupNode) Build(result *Result) *cobra.Command {
	cmd := &cobra.Command{
		Use:           g.group,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	for _, child := range g.children {
		cmd.AddCommand(child.Build(result))
	}
	return cmd
}

// Group declares a named, nested command group. It panics if name is empty or
// two children share a name, both detected when the tree is declared.
func Group(name string, children ...Node) Node {
	name = TrimmedNonEmpty("cli: group name must not be empty", name)
	rejectDuplicateNames(name, children)
	return groupNode{group: name, children: children}
}

// App is the root of a command tree.
type App struct {
	nodes []Node
}

// New assembles the root command group of a command tree. Two top-level
// children sharing a name is a declaration-time panic.
func New(children ...Node) App {
	rejectDuplicateNames("root", children)
	return App{nodes: children}
}

func rejectDuplicateNames(parent string, children []Node) {
	seen := make(map[string]bool, len(children))
	for _, c := range children {
		n := c.Name()
		if seen[n] {
			panic(fmt.Sprintf("cli: duplicate command or group name %q under %q", n, parent))
		}
		seen[n] = true
	}
}

// Execute parses args against a's command tree and dispatches to the
// resolved command, exactly as a real CLI invocation would, but under full
// caller control: ctx seeds the execution context (nil is treated as
// context.Background()), in is the stdin source, out and err are the
// stdout/stderr sinks, and the returned int is the exit code this execution
// maps to. Execute never calls os.Exit.
//
// A nil out or err is normalized to io.Discard before Execute uses it for
// anything, so a caller passing a literal nil never causes a nil-writer panic.
//
// Every declared parameter is resolved and validated, via param.Prepare, before
// any user middleware or the handler runs. Exit code mapping is fixed:
//
//   - an OK outcome maps to 0;
//   - a failed outcome without error maps to 1, silently;
//   - a failed outcome carrying an error also maps to 1, but first prints the
//     error to the normalized err writer exactly once, prefixed with the
//     dispatched command's name;
//   - a flag, argument, parameter or marked interactive input error (missing,
//     unparsable or validator-rejected value; an unmatched command; too many
//     positional arguments) maps to 2;
//   - a recovered Way2Go programmer error (see programmerFailure) maps to 1.
//     An unrelated panic is re-panicked out of Execute, not recovered.
//
// Execute builds a brand new *cobra.Command tree from a's declarative node
// tree on every call: no Cobra command or pflag.FlagSet, and therefore no
// flag "Changed" state, is ever reused or shared across calls, which is what
// keeps concurrent calls from cross-contaminating each other.
func (a App) Execute(ctx context.Context, args []string, in io.Reader, out, err io.Writer) int {
	if ctx == nil {
		ctx = context.Background()
	}
	if out == nil {
		out = io.Discard
	}
	if err == nil {
		err = io.Discard
	}
	// Internal short-only registration names start with NUL, which a real
	// argv cannot contain; reject a programmatic attempt to spell one.
	for _, a := range args {
		if a == "--" {
			break
		}
		if strings.HasPrefix(a, "--\x00") {
			fmt.Fprintf(err, "unknown flag: --%s\n", strings.TrimPrefix(a, "--\x00"))
			return 2
		}
	}
	result := &Result{in: in, out: out, err: err}
	root := &cobra.Command{
		Use:           programName(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	for _, n := range a.nodes {
		root.AddCommand(n.Build(result))
	}
	root.SetOut(out)
	root.SetErr(err)
	root.SetArgs(args)

	execErr := root.ExecuteContext(ctx)
	if execErr != nil {
		var pe *programmerError
		if errors.As(execErr, &pe) {
			fmt.Fprintln(err, execErr)
			return 1
		}
		fmt.Fprintln(err, publicParserError(execErr.Error()))
		return 2
	}

	if result.hasOutcome && !result.outcome.OK {
		if oerr := result.outcome.Err; oerr != nil {
			if isInputFailure(oerr) {
				fmt.Fprintf(err, "%s: %v\n", result.commandName, oerr)
				return 2
			}
			fmt.Fprintf(err, "%s: %v\n", result.commandName, oerr)
		}
		return 1
	}
	return 0
}

// Build constructs a fresh, unexported *cobra.Command for c each time it is
// called — Node.Build is invoked once per Execute call precisely so that no
// *cobra.Command, and no pflag.FlagSet state such as a flag's Changed bit, is
// ever shared or reused across executions. That is what keeps concurrent
// Execute calls (and concurrent test executions using independently injected
// output buffers) from cross-contaminating each other, without any locking.
func (c command) Build(result *Result) *cobra.Command {
	d := c.def
	var accs map[param.AnyDescriptor]*accumulator
	cmd := &cobra.Command{
		Use:           c.name,
		Short:         d.description,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.MaximumNArgs(len(d.positional)),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw := make(map[param.AnyDescriptor]param.RawValue, len(d.uses))
			for _, b := range d.named {
				raw[b.descriptor()] = accs[b.descriptor()].raw()
			}
			for i, b := range d.positional {
				if i < len(args) {
					raw[b.descriptor()] = param.RawValue{Value: args[i], Present: true}
				}
			}

			values, err := param.Prepare(d.uses, raw)
			if err != nil {
				return &inputError{err: d.externalize(err)}
			}

			ctx := invocation.New(cmd.Context(), values, result.in, result.out, result.err)
			outcome, err := invoke(ctx, d.handler)
			if err != nil {
				return err
			}
			result.outcome = outcome
			result.hasOutcome = true
			result.commandName = c.name
			return nil
		},
	}
	accs = make(map[param.AnyDescriptor]*accumulator, len(d.named))
	for _, b := range d.named {
		accs[b.descriptor()] = registerFlags(cmd, b)
	}
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &flagError{msg: firstNameMissingValue(err.Error(), d.named)}
	})
	cmd.SetHelpFunc(func(c *cobra.Command, _ []string) {
		fmt.Fprint(c.OutOrStdout(), d.help(c.CommandPath()))
	})
	return cmd
}

// accumulator is the single raw-value and presence slot shared by all of one
// binding's registered aliases; every Set overwrites it in argv order.
type accumulator struct {
	value   string
	present bool
	isBool  bool
}

func (a *accumulator) raw() param.RawValue {
	return param.RawValue{Value: a.value, Present: a.present}
}

// aliasValue is the pflag.Value of one alias, backed by the shared slot.
type aliasValue struct{ acc *accumulator }

func (v aliasValue) String() string { return v.acc.value }
func (v aliasValue) Set(s string) error {
	if v.acc.isBool {
		b, err := strconv.ParseBool(s)
		if err != nil {
			return err
		}
		s = strconv.FormatBool(b)
	}
	v.acc.value, v.acc.present = s, true
	return nil
}
func (v aliasValue) Type() string {
	if v.acc.isBool {
		return "bool"
	}
	return "string"
}

// registerFlags registers every alias of b against one shared accumulator.
// Short-only names use an internal registration name starting with NUL, which
// a real argv cannot contain. Booleans keep native bare-flag syntax through
// NoOptDefVal.
func registerFlags(cmd *cobra.Command, b Binding) *accumulator {
	acc := &accumulator{isBool: b.isBool()}
	for _, n := range b.options {
		var f *pflag.Flag
		if isShort(n) {
			cmd.Flags().VarP(aliasValue{acc}, "\x00"+n, n, "")
			f = cmd.Flags().Lookup("\x00" + n)
		} else {
			cmd.Flags().Var(aliasValue{acc}, n, "")
			f = cmd.Flags().Lookup(n)
		}
		if acc.isBool {
			f.NoOptDefVal = "true"
		}
	}
	return acc
}

// programName derives the root command's displayed program name from the
// executing process, so the internal Cobra root command needs no public
// configuration surface for it.
func programName() string {
	if len(os.Args) == 0 {
		return "cli"
	}
	return filepath.Base(os.Args[0])
}

// flagError carries an already-rewritten parser diagnostic.
type flagError struct{ msg string }

func (e *flagError) Error() string { return e.msg }

var (
	needsLong  = regexp.MustCompile(`^flag needs an argument: --(.+)$`)
	needsShort = regexp.MustCompile(`^flag needs an argument: '(.)' in -.$`)
)

// firstNameMissingValue rewrites pflag's missing-value diagnostics so they
// name the binding's first declared public name rather than the alias typed.
func firstNameMissingValue(msg string, named []Binding) string {
	var alias string
	if m := needsLong.FindStringSubmatch(msg); m != nil {
		alias = m[1]
	} else if m := needsShort.FindStringSubmatch(msg); m != nil {
		alias = m[1]
	} else {
		return msg
	}
	for _, b := range named {
		for _, n := range b.options {
			if n == alias {
				return "flag needs an argument: " + optionLabel(b.options[0])
			}
		}
	}
	return msg
}

var internalFlagPair = regexp.MustCompile(`-([A-Za-z]), --(?:\x00|\\x00)[A-Za-z]`)

// publicParserError removes internal short-only registration names from parser
// diagnostics so only public labels (such as -v) are shown.
func publicParserError(msg string) string {
	msg = internalFlagPair.ReplaceAllString(msg, "-$1")
	msg = strings.ReplaceAll(msg, "--\x00", "-")
	return strings.ReplaceAll(msg, `--\x00`, "-")
}
