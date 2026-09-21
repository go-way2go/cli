package cli_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/go-way2go/cli"
	"github.com/go-way2go/cli/param"
)

// TestDeclarationDoesNotExecuteHandler proves declaring a command, and asking
// for its help, never runs the handler or prepares invocation values.
func TestDeclarationDoesNotExecuteHandler(t *testing.T) {
	name := param.String("who to greet")
	executed := false
	def := cli.Command("greet", cli.Define("Greets someone.", func(ctx cli.Context) cli.Outcome {
		executed = true
		return cli.OK()
	},
		cli.WithArgument(name, "name")))
	var out bytes.Buffer
	if code := cli.New(def).Execute(context.Background(), []string{"greet", "--help"}, nil, &out, nil); code != 0 {
		t.Fatalf("help code = %d", code)
	}
	for _, want := range []string{"Greets someone.", "<name>", "who to greet", "(required)"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("help lacks %q:\n%s", want, out.String())
		}
	}
	if executed {
		t.Fatal("declaration or help executed the handler")
	}
}

// TestArgumentRequiredAfterOptionalPanics proves required positionals must
// precede those whose parameter has a default, ignoring intervening named
// bindings. Requiredness derives from the descriptor.
func TestArgumentRequiredAfterOptionalPanics(t *testing.T) {
	optionalFirst := param.String("", param.Default("x"))
	requiredSecond, named := param.String(""), param.Int("", param.Default(1))
	mustPanic(t, "second", func() {
		cli.Command("x", cli.Define("", noop,
			cli.WithArgument(optionalFirst, "first"),
			cli.WithOption(named, "n"),
			cli.WithArgument(requiredSecond, "second")))
	})
}

// TestArgumentsKeepDeclarationOrder proves positionals follow declaration
// order with named bindings between them, and required-then-optional is valid.
func TestArgumentsKeepDeclarationOrder(t *testing.T) {
	source, dest, n := param.String(""), param.String("", param.Default("out")), param.Int("", param.Default(1))
	def := cli.Command("cp", cli.Define("", noop,
		cli.WithArgument(source, "source"),
		cli.WithOption(n, "n"),
		cli.WithArgument(dest, "destination")))
	var out bytes.Buffer
	cli.New(def).Execute(context.Background(), []string{"cp", "--help"}, nil, &out, nil)
	if !strings.Contains(out.String(), "cp [options] <source> [destination]") {
		t.Fatalf("usage does not follow declaration order:\n%s", out.String())
	}
}

// TestBindingNameValidation proves invalid, empty, repeated and reserved
// names and zero descriptors are rejected at declaration, at every alias
// position.
func TestBindingNameValidation(t *testing.T) {
	p := param.String("")
	for name, tc := range map[string]struct {
		want string
		f    func()
	}{
		"empty name":          {"nonempty", func() { cli.WithOption(p, "") }},
		"empty alias":         {"nonempty", func() { cli.WithOption(p, "a", "") }},
		"empty alias last":    {"nonempty", func() { cli.WithOption(p, "a", "bb", "") }},
		"dash name":           {"leading dash", func() { cli.WithOption(p, "--x") }},
		"dash alias":          {"leading dash", func() { cli.WithOption(p, "x", "-y") }},
		"equals name":         {"'='", func() { cli.WithOption(p, "a=b") }},
		"equals alias":        {"'='", func() { cli.WithOption(p, "aa", "b=c") }},
		"space name":          {"whitespace", func() { cli.WithOption(p, "a b") }},
		"space alias":         {"whitespace", func() { cli.WithOption(p, "aa", "b c") }},
		"NUL name":            {"NUL", func() { cli.WithOption(p, "a\x00b") }},
		"NUL alias":           {"NUL", func() { cli.WithOption(p, "aa", "b\x00") }},
		"NUL short":           {"NUL", func() { cli.WithOption(p, "\x00") }},
		"digit short":         {"single ASCII letter", func() { cli.WithOption(p, "1") }},
		"digit short alias":   {"single ASCII letter", func() { cli.WithOption(p, "one", "1") }},
		"non-ASCII short":     {"single ASCII letter", func() { cli.WithOption(p, "\xc3") }},
		"non-ASCII letter":    {"single ASCII letter", func() { cli.WithOption(p, "é") }},
		"non-ASCII alias":     {"single ASCII letter", func() { cli.WithOption(p, "verbose", "é") }},
		"unicode space name":  {"whitespace", func() { cli.WithOption(p, "a\u00a0b") }},
		"unicode space alias": {"whitespace", func() { cli.WithOption(p, "aa", "b\u2003c") }},
		"unicode space lone":  {"whitespace", func() { cli.WithOption(p, "\u00a0") }},
		"unicode space arg":   {"whitespace", func() { cli.WithArgument(p, "a\u00a0b") }},
		"repeated long":       {"repeated", func() { cli.WithOption(p, "aa", "aa") }},
		"repeated short":      {"repeated", func() { cli.WithOption(p, "a", "bb", "a") }},
		"reserved help":       {"reserved", func() { cli.WithOption(p, "help") }},
		"reserved help late":  {"reserved", func() { cli.WithOption(p, "x", "yy", "help") }},
		"reserved h":          {"reserved", func() { cli.WithOption(p, "h") }},
		"reserved h alias":    {"reserved", func() { cli.WithOption(p, "verbose", "h") }},
		"empty argument":      {"nonempty", func() { cli.WithArgument(p, "") }},
		"space argument":      {"whitespace", func() { cli.WithArgument(p, "a b") }},
		"NUL argument":        {"NUL", func() { cli.WithArgument(p, "a\x00") }},
		"nil descriptor":      {"non-zero", func() { cli.WithOption(nil, "x") }},
		"zero descriptor":     {"non-zero", func() { cli.WithOption(param.Descriptor[string]{}, "x") }},
		"zero argument":       {"non-zero", func() { cli.WithArgument(param.Descriptor[int]{}, "x") }},
	} {
		t.Run(name, func(t *testing.T) { mustPanic(t, tc.want, tc.f) })
	}
	// Single letters at any position, uppercase, and long-only are valid.
	cli.WithOption(p, "s")
	cli.WithOption(p, "S")
	cli.WithOption(p, "s", "long")
	cli.WithOption(p, "long", "s", "other", "x")
	cli.WithOption(p, "n", "N")
	// A reserved name on a different kind is fine: -help is not a thing, but
	// a long h-less name containing h is.
	cli.WithOption(p, "helper", "hh")
}

// TestExternalTokenCollisionsAcrossIdentities proves option and argument
// tokens conflict across identities at any alias position, and that argument
// and option namespaces are separate.
func TestExternalTokenCollisionsAcrossIdentities(t *testing.T) {
	a, b := param.String(""), param.String("")
	mustPanic(t, "option --xx", func() {
		cli.Command("x", cli.Define("", noop, cli.WithOption(a, "xx"), cli.WithOption(b, "xx")))
	})
	mustPanic(t, "option -x", func() {
		cli.Command("x", cli.Define("", noop, cli.WithOption(a, "x"), cli.WithOption(b, "yy", "x")))
	})
	mustPanic(t, "option --long", func() {
		cli.Command("x", cli.Define("", noop, cli.WithOption(a, "aa", "long"), cli.WithOption(b, "bb", "s", "long")))
	})
	mustPanic(t, "argument \"x\"", func() {
		cli.Command("x", cli.Define("", noop, cli.WithArgument(a, "x"), cli.WithArgument(b, "x")))
	})
	cli.Command("x", cli.Define("", noop, cli.WithOption(a, "x"), cli.WithArgument(b, "x")))
	// Long and short namespaces are separate: -x versus --xx.
	cli.Command("x", cli.Define("", noop, cli.WithOption(a, "x"), cli.WithOption(b, "xx")))
}

// TestSameIdentityDeduplicatesEqualNameSets proves repeats deduplicate
// regardless of name order, and differing sets, kinds or positional/named
// mixes conflict.
func TestSameIdentityDeduplicatesEqualNameSets(t *testing.T) {
	limit := param.Int("", param.Default(1))
	def := cli.Command("x", cli.Define("", noop,
		cli.WithOption(limit, "limit", "l"),
		cli.WithOption(limit, "l", "limit")))
	var out bytes.Buffer
	cli.New(def).Execute(context.Background(), []string{"x", "--help"}, nil, &out, nil)
	if n := strings.Count(out.String(), "--limit"); n != 1 {
		t.Fatalf("help lists --limit %d times, want 1 after dedup:\n%s", n, out.String())
	}
	cli.Command("y", cli.Define("", noop,
		cli.WithOption(limit, "limit", "l", "max"),
		cli.WithOption(limit, "max", "limit", "l")))
	for name, other := range map[string]cli.BindingOption{
		"subset":   cli.WithOption(limit, "limit"),
		"superset": cli.WithOption(limit, "limit", "l", "max"),
		"disjoint": cli.WithOption(limit, "max"),
		"argument": cli.WithArgument(limit, "limit"),
	} {
		t.Run(name, func(t *testing.T) {
			mustPanic(t, "conflicting", func() {
				cli.Command("x", cli.Define("", noop, cli.WithOption(limit, "limit", "l"), other))
			})
		})
	}
}

// TestDefaultsBelongToDescriptors proves distinct identities with different
// defaults may not share option names, and that copies of a descriptor keep
// its identity for deduplication.
func TestDefaultsBelongToDescriptors(t *testing.T) {
	mustPanic(t, "different param", func() {
		cli.Command("x", cli.Define("", noop,
			cli.WithOption(param.String("", param.Default("a")), "limit"),
			cli.WithOption(param.String("", param.Default("b")), "limit")))
	})
	p := param.String("", param.Default("a"))
	q := p
	cli.Command("x", cli.Define("", noop, cli.WithOption(p, "f"), cli.WithOption(q, "f")))
}
