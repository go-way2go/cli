package cli_test

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/go-way2go/cli"
	"github.com/go-way2go/cli/param"
)

func TestScalarAliasesLastOccurrenceWins(t *testing.T) {
	limit := param.Int("", param.Default(20))
	for name, names := range map[string][]string{
		"long first":  {"limit", "l"},
		"short first": {"l", "limit"},
	} {
		t.Run(name, func(t *testing.T) {
			var got int
			app := cli.New(cli.Command("list", cli.Define("", func(c cli.Context) cli.Outcome { got = cli.Read(c, limit); return cli.OK() },
				cli.WithOption(limit, names[0], names[1:]...))))
			for args, want := range map[string]int{
				"--limit=1 -l 2": 2, "-l 2 --limit=1": 1, "-l 3": 3, "--limit 4": 4, "-l=5": 5, "": 20,
			} {
				got = 0
				argv := append([]string{"list"}, strings.Fields(args)...)
				if code, _, se := run(app, argv...); code != 0 || got != want {
					t.Fatalf("%q: code=%d got=%d want=%d stderr=%s", args, code, got, want, se)
				}
			}
		})
	}
}

func TestShortOnlyHasNoSyntheticLongName(t *testing.T) {
	verbose := param.Bool("Verbose output", param.Default(false))
	var got bool
	app := cli.New(cli.Command("run", cli.Define("", func(c cli.Context) cli.Outcome { got = cli.Read(c, verbose); return cli.OK() },
		cli.WithOption(verbose, "v"))))
	if code, _, se := run(app, "run", "-v"); code != 0 || !got {
		t.Fatalf("code=%d got=%v stderr=%s", code, got, se)
	}
	for _, arg := range []string{"--v", "--verbose", "--short-v", "--\x00v"} {
		if code, _, se := run(app, "run", arg); code != 2 || !strings.Contains(se, "unknown flag") {
			t.Fatalf("%q: code=%d stderr=%s", arg, code, se)
		}
	}
	_, out, _ := run(app, "run", "--help")
	if strings.Contains(out, "\x00") || strings.Contains(out, "--") && strings.Contains(out, "--v") {
		t.Fatalf("help leaks internal name: %q", out)
	}
}

func TestBooleanPresenceSemantics(t *testing.T) {
	verbose := param.Bool("", param.Default(true))
	note := param.String("", param.Default("none"))
	var got bool
	var gotNote string
	app := cli.New(cli.Command("run", cli.Define("", func(c cli.Context) cli.Outcome {
		got, gotNote = cli.Read(c, verbose), cli.Read(c, note)
		return cli.OK()
	}, cli.WithOption(verbose, "verbose", "v"), cli.WithArgument(note, "note"))))
	for name, tc := range map[string]struct {
		args []string
		want bool
		note string
	}{
		"absent default":     {nil, true, "none"},
		"bare long":          {[]string{"--verbose"}, true, "none"},
		"long false":         {[]string{"--verbose=false"}, false, "none"},
		"short false":        {[]string{"-v=false"}, false, "none"},
		"false then true":    {[]string{"--verbose=false", "-v"}, true, "none"},
		"true then false":    {[]string{"-v", "--verbose=false"}, false, "none"},
		"separate not eaten": {[]string{"--verbose", "false"}, true, "false"},
		"short separate":     {[]string{"-v", "false"}, true, "false"},
	} {
		t.Run(name, func(t *testing.T) {
			code, _, se := run(app, append([]string{"run"}, tc.args...)...)
			if code != 0 || got != tc.want || gotNote != tc.note {
				t.Fatalf("code=%d got=%v note=%q stderr=%s", code, got, gotNote, se)
			}
		})
	}
}

func TestRequiredBooleanNeedsPresenceNotTruth(t *testing.T) {
	confirm := param.Bool("")
	var got bool
	app := cli.New(cli.Command("run", cli.Define("", func(c cli.Context) cli.Outcome { got = cli.Read(c, confirm); return cli.OK() },
		cli.WithOption(confirm, "y"))))
	if code, _, se := run(app, "run", "-y=false"); code != 0 || got {
		t.Fatalf("code=%d got=%v stderr=%s", code, got, se)
	}
	if code, _, se := run(app, "run"); code != 2 || !strings.Contains(se, "-y: value is required") {
		t.Fatalf("code=%d stderr=%s", code, se)
	}
}

func TestParserInputErrors(t *testing.T) {
	name := param.String("", param.Default("n"))
	limit := param.Int("Max", param.Default(1))
	arg := param.String("", param.Default("a"))
	app := cli.New(cli.Command("run", cli.Define("", func(c cli.Context) cli.Outcome { return cli.OK() },
		cli.WithOption(name, "name", "n"),
		cli.WithOption(limit, "l"),
		cli.WithArgument(arg, "item"))))
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"unknown long":  {[]string{"--nope"}, "unknown flag"},
		"unknown short": {[]string{"-z"}, "unknown shorthand"},
		"missing value": {[]string{"--name"}, "needs an argument"},
		"missing short": {[]string{"-n"}, "needs an argument"},
		"excess":        {[]string{"a", "b"}, "accepts at most 1"},
		"bad int":       {[]string{"-l", "x"}, "-l: invalid int value"},
		"help name":     {[]string{"--help=false", "--name"}, "needs an argument"},
	} {
		t.Run(name, func(t *testing.T) {
			if code, _, se := run(app, append([]string{"run"}, tc.args...)...); code != 2 || !strings.Contains(se, tc.want) {
				t.Fatalf("code=%d stderr=%s", code, se)
			}
		})
	}
	var gotItem string
	app = cli.New(cli.Command("run", cli.Define("", func(c cli.Context) cli.Outcome { gotItem = cli.Read(c, arg); return cli.OK() },
		cli.WithArgument(arg, "item"), cli.WithOption(name, "name"))))
	if code, _, se := run(app, "run", "--", "--name"); code != 0 || gotItem != "--name" {
		t.Fatalf("end of options: code=%d item=%q stderr=%s", code, gotItem, se)
	}
	// Explicit empty input bypasses the default.
	if code, _, _ := run(app, "run", ""); code != 0 || gotItem != "" {
		t.Fatalf("explicit empty argument: item=%q", gotItem)
	}
}

func TestIndependentCommandDefaults(t *testing.T) {
	limitA, limitB := param.Int("", param.Default(10)), param.Int("", param.Default(20))
	var a, b atomic.Int64
	app := cli.New(
		cli.Command("a", cli.Define("", func(c cli.Context) cli.Outcome { a.Store(int64(cli.Read(c, limitA))); return cli.OK() }, cli.WithOption(limitA, "limit"))),
		cli.Command("b", cli.Define("", func(c cli.Context) cli.Outcome { b.Store(int64(cli.Read(c, limitB))); return cli.OK() }, cli.WithOption(limitB, "l"))),
	)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			run(app, "a")
			run(app, "b")
		}()
	}
	wg.Wait()
	if a.Load() != 10 || b.Load() != 20 {
		t.Fatalf("a=%d b=%d", a.Load(), b.Load())
	}
}

func TestStablePublicErrorLabels(t *testing.T) {
	src := param.String("")
	limit := param.Int("")
	app := cli.New(cli.Command("run", cli.Define("", func(cli.Context) cli.Outcome { return cli.OK() },
		cli.WithArgument(src, "source"), cli.WithOption(limit, "l", "limit"))))
	if code, _, se := run(app, "run", "-l", "1"); code != 2 || !strings.Contains(se, "argument source: value is required") {
		t.Fatalf("code=%d %q", code, se)
	}
	if code, _, se := run(app, "run", "s"); code != 2 || !strings.Contains(se, "-l: value is required") {
		t.Fatalf("code=%d %q", code, se)
	}
	if code, _, se := run(app, "run", "s", "--limit=x"); code != 2 || !strings.Contains(se, "-l: invalid int value") {
		t.Fatalf("code=%d %q", code, se)
	}
}

func TestParserErrorsHideInternalNames(t *testing.T) {
	app := cli.New(cli.Command("run", cli.Define("", noop,
		cli.WithOption(param.Bool("", param.Default(false)), "v"),
		cli.WithOption(param.Bool("", param.Default(false)), "force", "f"),
		cli.WithOption(param.Int("", param.Default(1)), "l"),
		cli.WithOption(param.Int("", param.Default(1)), "n", "num"))))
	for _, args := range [][]string{
		{"-v=oops"}, {"-f=oops"}, {"--force=oops"}, {"-l", "x"}, {"-l=x"},
		{"-l"}, {"-vl"}, {"-nx"}, {"--num=x"}, {"--num"}, {"-vz"}, {"--zzz"},
		{"--l"}, {"--help=oops"}, {"-v", "--\x00v"}, {"--", "--\x00v", "x", "y"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			code, _, stderr := run(app, append([]string{"run"}, args...)...)
			if code != 2 || stderr == "" || strings.Contains(stderr, "\x00") || strings.Contains(stderr, `\x00`) {
				t.Fatalf("code=%d stderr=%q", code, stderr)
			}
			// User-supplied unknown long options may appear verbatim in diagnostics.
			switch strings.Join(args, " ") {
			case "-v=oops", "-f=oops", "--force=oops", "-l x", "-l=x":
				if strings.Contains(stderr, "--v") || strings.Contains(stderr, "--l") {
					t.Fatalf("synthetic long option in stderr: %q", stderr)
				}
			}
			if args[0] == "-v=oops" && !strings.Contains(stderr, `"-v"`) {
				t.Fatalf("stderr=%q", stderr)
			}
		})
	}
}
