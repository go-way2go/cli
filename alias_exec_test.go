package cli_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/go-way2go/cli"
	"github.com/go-way2go/cli/param"
	"github.com/go-way2go/cli/validate"
)

// countApp binds a validated int through the given names and records reads and
// validator invocations.
func countApp(names ...string) (cli.App, *int, *int) {
	var validations, got int
	count := param.Int("Repeat count", validate.Check(func(int) error { validations++; return nil }), param.Default(7))
	app := cli.New(cli.Command("run", cli.Define("", func(c cli.Context) cli.Outcome {
		got = cli.Read(c, count)
		return cli.OK()
	}, cli.WithOption(count, names[0], names[1:]...))))
	return app, &got, &validations
}

func TestMultiAliasMatrix(t *testing.T) {
	type tc struct {
		args []string
		want int
	}
	cases := map[string][]tc{
		"count,c,times,n": {
			{[]string{"--times", "2", "-c", "3", "--count", "4", "-n", "5"}, 5},
			{[]string{"-n", "5", "--count", "4", "-c", "3", "--times", "2"}, 2},
			{[]string{"--count=1", "-n9"}, 9},
			{[]string{"-n9", "--count=1"}, 1},
			{[]string{"-c=6"}, 6},
			{[]string{"--times=0"}, 0},
			{nil, 7},
		},
		"c,count,n": {
			{[]string{"--count", "2", "-n3"}, 3},
			{[]string{"-n3", "--count", "2"}, 2},
			{[]string{"-c", "8"}, 8},
		},
		"n,times,count,c": {
			{[]string{"--times", "2", "-c", "3", "--count", "4", "-n", "5"}, 5},
		},
	}
	for spec, list := range cases {
		for _, c := range list {
			t.Run(fmt.Sprint(spec, c.args), func(t *testing.T) {
				app, got, validations := countApp(strings.Split(spec, ",")...)
				code, _, se := run(app, append([]string{"run"}, c.args...)...)
				if code != 0 || *got != c.want || *validations != 2 {
					// 1 validation of the default at construction + 1 for this invocation.
					t.Fatalf("code=%d got=%d want=%d validations=%d stderr=%s", code, *got, c.want, *validations, se)
				}
			})
		}
	}
}

func TestAliasValidationRunsOncePerInvocation(t *testing.T) {
	app, _, validations := countApp("count", "c", "times", "n")
	base := *validations // default validated at construction
	if code, _, se := run(app, "run", "--times", "2", "-c", "3", "--count", "4", "-n", "5"); code != 0 {
		t.Fatalf("code=%d %s", code, se)
	}
	if *validations-base != 1 {
		t.Fatalf("validations per invocation = %d, want 1", *validations-base)
	}
}

func TestOnlyFinalAliasValueIsParsed(t *testing.T) {
	app, got, _ := countApp("count", "c")
	if code, _, se := run(app, "run", "--count", "junk", "-c", "4"); code != 0 || *got != 4 {
		t.Fatalf("code=%d got=%d %s", code, *got, se)
	}
	code, _, se := run(app, "run", "-c", "4", "--count", "junk")
	if code != 2 || !strings.Contains(se, "--count: ") {
		t.Fatalf("code=%d %s", code, se)
	}
	// Errors name the first declared binding name whichever alias was typed.
	app, _, _ = countApp("c", "count")
	if code, _, se = run(app, "run", "--count", "junk"); code != 2 || !strings.Contains(se, "-c: invalid") {
		t.Fatalf("code=%d %s", code, se)
	}
}

func TestMissingValueUsesFirstDeclaredName(t *testing.T) {
	app, _, _ := countApp("count", "c", "times", "n")
	for _, args := range [][]string{{"--times"}, {"-n"}, {"-c"}, {"--count"}, {"--times", "2", "-n"}, {"-cn"}} {
		code, _, se := run(app, append([]string{"run"}, args...)...)
		if args[len(args)-1] == "-cn" {
			// -c takes "n" as its attached value: not a missing value.
			if code != 2 || strings.Contains(se, "needs an argument") {
				t.Fatalf("%v: code=%d %s", args, code, se)
			}
			continue
		}
		if code != 2 || strings.TrimSpace(se) != "flag needs an argument: --count" {
			t.Fatalf("%v: code=%d stderr=%q", args, code, se)
		}
	}
	app, _, _ = countApp("c", "count")
	if code, _, se := run(app, "run", "--count"); code != 2 || strings.TrimSpace(se) != "flag needs an argument: -c" {
		t.Fatalf("code=%d stderr=%q", code, se)
	}
}

func TestShortOnlyAliasesAndClusters(t *testing.T) {
	verbose := param.Bool("", param.Default(false))
	quiet := param.Bool("", param.Default(false))
	name := param.String("", param.Default("d"))
	var v, q bool
	var n string
	app := cli.New(cli.Command("run", cli.Define("", func(c cli.Context) cli.Outcome {
		v, q, n = cli.Read(c, verbose), cli.Read(c, quiet), cli.Read(c, name)
		return cli.OK()
	}, cli.WithOption(verbose, "v"), cli.WithOption(quiet, "q", "quiet"), cli.WithOption(name, "n"))))
	for args, want := range map[string][3]any{
		"-vq":       {true, true, "d"},
		"-qv":       {true, true, "d"},
		"-vqnfoo":   {true, true, "foo"},
		"-vnfoo":    {true, false, "foo"},
		"-n=foo -q": {false, true, "foo"},
		"-n= -v":    {true, false, "="}, // pflag: "-n=" alone is the literal value "="
	} {
		v, q, n = false, false, ""
		code, _, se := run(app, append([]string{"run"}, strings.Fields(args)...)...)
		if code != 0 || v != want[0] || q != want[1] || n != want[2] {
			t.Fatalf("%q: code=%d v=%v q=%v n=%q %s", args, code, v, q, n, se)
		}
	}
	// -vn followed by separate value.
	if code, _, se := run(app, "run", "-vn", "foo"); code != 0 || !v || n != "foo" {
		t.Fatalf("code=%d v=%v n=%q %s", code, v, n, se)
	}
	// Missing value in a cluster.
	if code, _, se := run(app, "run", "-vn"); code != 2 || strings.TrimSpace(se) != "flag needs an argument: -n" {
		t.Fatalf("code=%d %q", code, se)
	}
	// Short-only registrations have no long spelling.
	for _, arg := range []string{"--v", "--n", "--verbose", "--\x00v", "--\x00q", "--\x00n=x"} {
		code, _, se := run(app, "run", arg)
		if code != 2 || !strings.Contains(se, "unknown flag") || strings.Contains(se, "\x00") {
			t.Fatalf("%q: code=%d %q", arg, code, se)
		}
	}
	if code, _, se := run(app, "run", "-x"); code != 2 || strings.Contains(se, "\x00") {
		t.Fatalf("code=%d %q", code, se)
	}
	if code, _, se := run(app, "run", "-vx"); code != 2 || !strings.Contains(se, "unknown shorthand flag: 'x' in -x") {
		t.Fatalf("code=%d %q", code, se)
	}
}

func TestBooleanAliasOrdering(t *testing.T) {
	for _, req := range []bool{false, true} {
		verbose := param.Bool("")
		if !req {
			verbose = param.Bool("", param.Default(false))
		}
		var got bool
		app := cli.New(cli.Command("run", cli.Define("", func(c cli.Context) cli.Outcome { got = cli.Read(c, verbose); return cli.OK() },
			cli.WithOption(verbose, "verbose", "v"))))
		for args, want := range map[string]bool{
			"--verbose=false -v":       true,
			"-v --verbose=false":       false,
			"--verbose=false":          false,
			"-v=false":                 false,
			"-v=false --verbose":       true,
			"--verbose=true -v=false":  false,
			"--verbose=false -v=false": false,
			"--verbose=0":              false,
			"--verbose=T":              true,
		} {
			got = !want
			if code, _, se := run(app, append([]string{"run"}, strings.Fields(args)...)...); code != 0 || got != want {
				t.Fatalf("required=%v %q: code=%d got=%v %s", req, args, code, got, se)
			}
		}
		if code, _, se := run(app, "run", "--verbose=maybe"); code != 2 || strings.Contains(se, "\x00") {
			t.Fatalf("code=%d %s", code, se)
		}
		code, _, se := run(app, "run")
		if req && (code != 2 || !strings.Contains(se, "--verbose: value is required")) {
			t.Fatalf("code=%d %s", code, se)
		}
		if !req && (code != 0 || got) {
			t.Fatalf("code=%d got=%v", code, got)
		}
	}
}

func TestEmptyAndAttachedInputs(t *testing.T) {
	name := param.String("", param.Default("dflt"))
	nonEmpty := param.String("", validate.NonEmpty(), param.Default("x"))
	var got, gotNE string
	app := cli.New(cli.Command("run", cli.Define("", func(c cli.Context) cli.Outcome {
		got, gotNE = cli.Read(c, name), cli.Read(c, nonEmpty)
		return cli.OK()
	}, cli.WithOption(name, "name", "n"), cli.WithOption(nonEmpty, "ne"))))
	for args, want := range map[string]string{
		"":           "dflt",
		"--name=":    "",
		"-n=":        "=",  // pflag short-option token semantics
		"--name ''":  "''", // literal, no shell
		"-nabc":      "abc",
		"--name=a=b": "a=b",
		"-n=a=b":     "a=b",
		"-n -x":      "-x",
	} {
		if code, _, se := run(app, append([]string{"run"}, strings.Fields(args)...)...); code != 0 || got != want {
			t.Fatalf("%q: code=%d got=%q %s", args, code, got, se)
		}
	}
	if code, _, _ := run(app, "run", "--name", ""); code != 0 || got != "" {
		t.Fatalf("separate empty value: got=%q", got)
	}
	if code, _, _ := run(app, "run", "-n", "z", "--name", ""); code != 0 || got != "" {
		t.Fatalf("later empty overrides: got=%q", got)
	}
	// A present empty value is validated, not defaulted.
	if code, _, se := run(app, "run", "--ne="); code != 2 || !strings.Contains(se, "--ne: ") {
		t.Fatalf("code=%d %s", code, se)
	}
	if code, _, _ := run(app, "run"); code != 0 || gotNE != "x" {
		t.Fatalf("default: %q", gotNE)
	}
}

func TestAliasIsolationRepeatedAndConcurrent(t *testing.T) {
	count := param.Int("", param.Default(7))
	flag := param.Bool("", param.Default(false))
	type result struct {
		n int
		f bool
	}
	var got sync.Map
	tag := param.String("", param.Default(""))
	app := cli.New(cli.Command("run", cli.Define("", func(c cli.Context) cli.Outcome {
		got.Store(cli.Read(c, tag), result{cli.Read(c, count), cli.Read(c, flag)})
		return cli.OK()
	}, cli.WithOption(count, "count", "c", "times", "n"), cli.WithOption(flag, "f", "flag"), cli.WithOption(tag, "tag"))))
	type inv struct {
		args []string
		want result
	}
	invs := []inv{
		{[]string{"--tag=a", "-c", "1"}, result{1, false}},
		{[]string{"--tag=b", "--times=2", "-f"}, result{2, true}},
		{[]string{"--tag=c"}, result{7, false}},
		{[]string{"--tag=d", "-n3", "--count=4", "--flag=false"}, result{4, false}},
		{[]string{"--tag=e", "-f", "-c9"}, result{9, true}},
	}
	var wg sync.WaitGroup
	for round := 0; round < 20; round++ {
		for _, iv := range invs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if code, _, se := run(app, append([]string{"run"}, iv.args...)...); code != 0 {
					t.Errorf("%v: code=%d %s", iv.args, code, se)
					return
				}
				tagName := strings.TrimPrefix(iv.args[0], "--tag=")
				if r, _ := got.Load(tagName); r != iv.want {
					t.Errorf("%v: got %v want %v", iv.args, r, iv.want)
				}
			}()
		}
	}
	wg.Wait()
	// Sequential repetition: supplied alias never leaks into a later default run.
	for i := 0; i < 3; i++ {
		for _, iv := range invs {
			run(app, append([]string{"run"}, iv.args...)...)
			run(app, "run", "--tag=z")
			if r, _ := got.Load("z"); r != (result{7, false}) {
				t.Fatalf("state leaked after %v: %v", iv.args, r)
			}
		}
	}
}
