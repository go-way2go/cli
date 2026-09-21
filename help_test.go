package cli_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/go-way2go/cli"
	"github.com/go-way2go/cli/param"
	"github.com/go-way2go/cli/validate"
)

func TestHelpFromBindingMetadata(t *testing.T) {
	query := param.String("Search text")
	limit := param.Int("Maximum results", param.Default(20))
	verbose := param.Bool("Verbose output", param.Default(false))
	force := param.Bool("Force")
	source := param.String("Source path")
	dest := param.String("Destination", param.Default("out"))
	called := false
	app := cli.New(cli.Command("search", cli.Define("Searches.", func(cli.Context) cli.Outcome { called = true; return cli.OK() },
		cli.WithArgument(source, "source"),
		cli.WithOption(query, "query", "q"),
		cli.WithOption(limit, "limit", "l"),
		cli.WithOption(verbose, "v"),
		cli.WithOption(force, "force"),
		cli.WithArgument(dest, "destination"))))
	code, out, se := run(app, "search", "--help")
	if code != 0 || called || se != "" {
		t.Fatalf("code=%d called=%v stderr=%s", code, called, se)
	}
	for _, want := range []string{
		"Searches.",
		"search [options] <source> [destination]",
		"source <string>  Source path (required)",
		"destination <string>  Destination (optional, default \"out\")",
		"--query, -q <string>  Search text (required)",
		"--limit, -l <int>  Maximum results (optional, default 20)",
		"-v  Verbose output (optional, default false)",
		"--force  Force (required)",
	} {
		if !strings.Contains(squash(out), squash(want)) {
			t.Errorf("help missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x00") || strings.Contains(out, "--v ") {
		t.Errorf("help leaks internal names:\n%s", out)
	}
}

func squash(s string) string { return strings.Join(strings.Fields(s), " ") }

type helpEndpoint struct{ host string }

func TestHelpDefaultRendering(t *testing.T) {
	endpointType := param.DefineType("endpoint", func(s string) (*helpEndpoint, error) { return &helpEndpoint{s}, nil })
	zeroInt := param.Int("Zero int", param.Default(0))
	falseBool := param.Bool("Off flag", param.Default(false))
	emptyStr := param.String("Empty string", param.Default(""))
	nilPtr := param.Of(endpointType, "Endpoint", param.Default[*helpEndpoint](nil))
	optArg := param.String("Optional arg", param.Default("x"))
	reqArg := param.String("Required arg")
	var handlerCalls, validations int
	checked := param.Int("Checked", validate.Check(func(int) error { validations++; return nil }), param.Default(5))
	var base int
	app := cli.New(cli.Command("run", cli.Define("Runs.", func(cli.Context) cli.Outcome { handlerCalls++; return cli.OK() },
		cli.WithOption(zeroInt, "zero", "z"),
		cli.WithOption(falseBool, "off", "o"),
		cli.WithOption(emptyStr, "empty", "e"),
		cli.WithOption(nilPtr, "endpoint"),
		cli.WithOption(checked, "checked"),
		cli.WithArgument(reqArg, "req"),
		cli.WithArgument(optArg, "opt"))))
	base = validations
	code, out, se := run(app, "run", "--help")
	if code != 0 || se != "" || handlerCalls != 0 || validations != base {
		t.Fatalf("code=%d stderr=%q handlers=%d validations=%d", code, se, handlerCalls, validations-base)
	}
	flat := squash(out)
	for _, want := range []string{
		"run [options] <req> [opt]",
		"--zero, -z <int> Zero int (optional, default 0)",
		"--off, -o Off flag (optional, default false)",
		`--empty, -e <string> Empty string (optional, default "")`,
		"--endpoint <endpoint> Endpoint (optional, default nil)",
		"--checked <int> Checked (optional, default 5)",
		"req <string> Required arg (required)",
		`opt <string> Optional arg (optional, default "x")`,
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("help missing %q:\n%s", want, out)
		}
	}
	if strings.Index(flat, "<req>") > strings.Index(flat, "[opt]") {
		t.Errorf("positional order:\n%s", out)
	}
	// Help with otherwise invalid or missing inputs still succeeds without preparing.
	for _, args := range [][]string{{"run", "--zero=bad", "--help"}, {"run", "--checked", "1", "--help"}} {
		if code, _, _ := run(app, args...); code != 0 || handlerCalls != 0 || validations != base {
			t.Fatalf("%v: code=%d handlers=%d validations=%d", args, code, handlerCalls, validations-base)
		}
	}
}

func TestEmptyDescriptionIsPreserved(t *testing.T) {
	var out bytes.Buffer
	app := cli.New(cli.Command("plain", cli.Define("", noop)))
	if code := app.Execute(context.Background(), []string{"plain", "--help"}, nil, &out, nil); code != 0 {
		t.Fatalf("code %d", code)
	}
}
