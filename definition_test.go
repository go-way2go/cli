package cli_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/go-way2go/cli"
	"github.com/go-way2go/cli/param"
)

// TestDefineFailsBeforeRegistration proves invalid definitions panic in Define
// itself, without a name and without executing the handler.
func TestDefineFailsBeforeRegistration(t *testing.T) {
	a, b := param.String(""), param.String("")
	optional, required := param.String("", param.Default("")), param.String("")
	for name, tc := range map[string]struct {
		contains string
		f        func()
	}{
		"nil handler": {"handler must not be nil", func() { cli.Define("", nil) }},
		"conflicting option": {"already declared", func() {
			cli.Define("", noop, cli.WithOption(a, "x"), cli.WithOption(b, "x"))
		}},
		"positional order": {"must be declared before", func() {
			cli.Define("", noop, cli.WithArgument(optional, "o"), cli.WithArgument(required, "r"))
		}},
		"nil middleware handler": {"nil handler", func() {
			cli.Define("", noop, cli.WithMiddleware(cli.DefineMiddleware("m", func(cli.HandlerFunc) cli.HandlerFunc { return nil })))
		}},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatal("expected panic")
				}
				msg := fmt.Sprint(r)
				if !strings.Contains(msg, tc.contains) {
					t.Fatalf("panic = %q, want %q", msg, tc.contains)
				}
				if strings.Contains(msg, "command \"") {
					t.Fatalf("definition diagnostic invents a command name: %q", msg)
				}
			}()
			tc.f()
		})
	}
}

func TestCommandRejectsEmptyNameAndZeroDefinition(t *testing.T) {
	def := cli.Define("", noop)
	mustPanic(t, "command name must not be empty", func() { cli.Command("", def) })
	mustPanic(t, "command name must not be empty", func() { cli.Command("  \t", def) })
	mustPanic(t, "created with Define", func() { cli.Command("x", cli.CommandDefinition{}) })
}

// TestDefinitionReuseAcrossNames proves one definition registered under
// several names and groups keeps each registration's name, help path and error
// prefix, shares no parser state, and composes middleware exactly once.
func TestDefinitionReuseAcrossNames(t *testing.T) {
	who := param.String("Who.")
	composed := 0
	mw := cli.DefineMiddleware("count", func(next cli.HandlerFunc) cli.HandlerFunc {
		composed++
		return next
	})
	var ran atomic.Int64
	def := cli.Define("Greets.", func(c cli.Context) cli.Outcome {
		ran.Add(1)
		if cli.Read(c, who) == "boom" {
			return cli.Error(fmt.Errorf("failed"))
		}
		fmt.Fprintf(c.Stdout(), "hi %s\n", cli.Read(c, who))
		return cli.OK()
	}, cli.WithMiddleware(mw), cli.WithArgument(who, "who"))
	if composed != 1 {
		t.Fatalf("composed = %d after Define, want 1", composed)
	}
	app := cli.New(
		cli.Command("hello", def),
		cli.Command("hi", def),
		cli.Group("team", cli.Command("greet", def), cli.Group("deep", cli.Command("greet", def))),
	)
	if composed != 1 || ran.Load() != 0 {
		t.Fatalf("registration composed %d / ran %d", composed, ran.Load())
	}

	for _, path := range [][]string{{"hello"}, {"hi"}, {"team", "greet"}, {"team", "deep", "greet"}} {
		var out, errb bytes.Buffer
		if code := app.Execute(context.Background(), append(append([]string(nil), path...), "ada"), nil, &out, &errb); code != 0 || out.String() != "hi ada\n" {
			t.Fatalf("%v: code %d out %q err %q", path, code, out.String(), errb.String())
		}
		out.Reset()
		errb.Reset()
		leaf := path[len(path)-1]
		if code := app.Execute(context.Background(), append(append([]string(nil), path...), "boom"), nil, &out, &errb); code != 1 || errb.String() != leaf+": failed\n" {
			t.Fatalf("%v: code %d err %q", path, code, errb.String())
		}
		out.Reset()
		if code := app.Execute(context.Background(), append(append([]string(nil), path...), "--help"), nil, &out, &errb); code != 0 {
			t.Fatalf("%v help: code %d", path, code)
		}
		if !strings.Contains(out.String(), "Greets.") || !strings.Contains(out.String(), strings.Join(path, " ")) {
			t.Fatalf("%v help lacks description or path:\n%s", path, out.String())
		}
	}
	if composed != 1 {
		t.Fatalf("execution recomposed middleware: %d", composed)
	}
}

func TestDefinitionConcurrentExecution(t *testing.T) {
	who := param.String("")
	def := cli.Define("", func(c cli.Context) cli.Outcome {
		fmt.Fprint(c.Stdout(), cli.Read(c, who))
		return cli.OK()
	}, cli.WithArgument(who, "who"))
	app := cli.New(cli.Command("a", def), cli.Command("b", def))
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			name, arg := "a", fmt.Sprint("v", i)
			if i%2 == 1 {
				name = "b"
			}
			var out bytes.Buffer
			if code := app.Execute(context.Background(), []string{name, arg}, nil, &out, nil); code != 0 || out.String() != arg {
				t.Errorf("%s %s: code %d out %q", name, arg, code, out.String())
			}
		}()
	}
	wg.Wait()
}

func TestDefinitionReuseUnderDifferentNames(t *testing.T) {
	count := param.Int("Times", param.Default(3))
	var got int
	def := cli.Define("Does things.", func(c cli.Context) cli.Outcome {
		got = cli.Read(c, count)
		if got < 0 {
			return cli.Error(fmt.Errorf("negative"))
		}
		return cli.OK()
	}, cli.WithOption(count, "count", "c"))
	app := cli.New(cli.Command("one", def), cli.Group("g", cli.Command("two", def)))
	if code, _, _ := run(app, "one", "-c", "5"); code != 0 || got != 5 {
		t.Fatalf("one: %d", got)
	}
	if code, _, _ := run(app, "g", "two"); code != 0 || got != 3 {
		t.Fatalf("two: %d", got)
	}
	if code, _, _ := run(app, "one"); code != 0 || got != 3 {
		t.Fatalf("one again: %d", got)
	}
	for name, path := range map[string][]string{"one": {"one"}, "two": {"g", "two"}} {
		_, out, _ := run(app, append(path, "--help")...)
		if !strings.Contains(out, strings.Join(path, " ")+" [options]") || !strings.Contains(out, "--count, -c <int>") {
			t.Fatalf("%s help:\n%s", name, out)
		}
		code, _, se := run(app, append(path, "--count=-1")...)
		if code != 1 || !strings.HasPrefix(se, name+": ") {
			t.Fatalf("%s error prefix: code=%d %q", name, code, se)
		}
	}
}

func TestDuplicateRegistrationNames(t *testing.T) {
	def := cli.Define("", noop)
	for _, tc := range []struct {
		name  string
		nodes func() []cli.Node
	}{
		{"commands", func() []cli.Node { return []cli.Node{cli.Command("x", def), cli.Command("x", def)} }},
		{"groups", func() []cli.Node {
			return []cli.Node{cli.Group("x", cli.Command("a", def)), cli.Group("x", cli.Command("b", def))}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("root", func(t *testing.T) { mustPanic(t, "duplicate", func() { cli.New(tc.nodes()...) }) })
			t.Run("group", func(t *testing.T) { mustPanic(t, "duplicate", func() { cli.Group("parent", tc.nodes()...) }) })
		})
	}
}
