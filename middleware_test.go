package cli_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/go-way2go/cli"
	"github.com/go-way2go/cli/param"
	"github.com/go-way2go/cli/validate"
)

// TestMiddlewareOrderAndPreMiddlewareParamResolution proves end to end that
// middleware runs in declaration order (first
// declared outermost), and every declared Param is already resolved and
// readable from within the first (outermost) middleware — i.e. before any
// user middleware or the handler runs.
func TestMiddlewareOrderAndPreMiddlewareParamResolution(t *testing.T) {
	var trace []string
	limit := param.Int("", param.Default(1))

	auth := cli.DefineMiddleware("auth", func(next cli.HandlerFunc) cli.HandlerFunc {
		return func(ctx cli.Context) cli.Outcome {
			trace = append(trace, "enter:auth")
			v := cli.Read(ctx, limit) // proves resolution happened before this, the outermost middleware
			trace = append(trace, fmt.Sprintf("auth-saw-limit:%d", v))
			r := next(ctx)
			trace = append(trace, "exit:auth")
			return r
		}
	})
	audit := cli.DefineMiddleware("audit", func(next cli.HandlerFunc) cli.HandlerFunc {
		return func(ctx cli.Context) cli.Outcome {
			trace = append(trace, "enter:audit")
			r := next(ctx)
			trace = append(trace, "exit:audit")
			return r
		}
	})

	act := cli.Command("run", cli.Define("", func(ctx cli.Context) cli.Outcome {
		trace = append(trace, "handler")
		return cli.OK()
	}, cli.WithOption(limit, "limit"), cli.WithMiddleware(auth, audit)))

	app := cli.New(act)
	var out, errBuf bytes.Buffer
	if code := app.Execute(context.Background(), []string{"run", "--limit=42"}, nil, &out, &errBuf); code != 0 {
		t.Fatalf("code = %d, want 0; stderr=%s", code, errBuf.String())
	}

	want := []string{"enter:auth", "auth-saw-limit:42", "enter:audit", "handler", "exit:audit", "exit:auth"}
	if len(trace) != len(want) {
		t.Fatalf("trace = %v, want %v", trace, want)
	}
	for i := range want {
		if trace[i] != want[i] {
			t.Fatalf("trace = %v, want %v", trace, want)
		}
	}
}

func newPaginationMiddleware(limit param.Descriptor[int], trace *[]string) cli.MiddlewareDefinition {
	return cli.DefineMiddleware("pagination", func(next cli.HandlerFunc) cli.HandlerFunc {
		return func(c cli.Context) cli.Outcome {
			if trace != nil {
				*trace = append(*trace, "pagination")
			}
			return next(c)
		}
	}, cli.WithOption(limit, "limit"))
}

func TestMiddlewareDeclarations(t *testing.T) {
	limit := param.Int("", param.Default(1))
	var handlerCalls, middlewareCalls int
	mw := cli.DefineMiddleware("count", func(next cli.HandlerFunc) cli.HandlerFunc {
		return func(c cli.Context) cli.Outcome { middlewareCalls++; return next(c) }
	}, cli.WithOption(limit, "limit", "l"))
	d := cli.Command("count", cli.Define("", func(cli.Context) cli.Outcome { handlerCalls++; return cli.OK() }, cli.WithMiddleware(mw, mw), cli.WithOption(limit, "l", "limit")))
	var help bytes.Buffer
	cli.New(d).Execute(context.Background(), []string{"count", "--help"}, nil, &help, nil)
	if n := strings.Count(help.String(), "--limit"); n != 1 {
		t.Fatalf("help lists --limit %d times, want 1 after dedup:\n%s", n, help.String())
	}
	if handlerCalls != 0 || middlewareCalls != 0 {
		t.Fatal("declaration executed handler")
	}
	for name, declare := range map[string]func(){
		"nil wrapper":     func() { cli.DefineMiddleware("nil", nil) },
		"zero middleware": func() { cli.Command("x", cli.Define("", noop, cli.WithMiddleware(cli.MiddlewareDefinition{}))) },
		"positional": func() {
			cli.DefineMiddleware("pos", func(next cli.HandlerFunc) cli.HandlerFunc { return next }, cli.WithArgument(param.String(""), "x"))
		},
		"argument conflict": func() {
			cli.Command("x", cli.Define("", noop, cli.WithMiddleware(mw), cli.WithArgument(limit, "limit")))
		},
		"name subset conflict": func() {
			cli.Command("x", cli.Define("", noop, cli.WithMiddleware(mw), cli.WithOption(limit, "limit")))
		},
		"token collision": func() {
			cli.Command("x", cli.Define("", noop, cli.WithMiddleware(mw), cli.WithOption(param.Int("", param.Default(1)), "limit")))
		},
		"middleware conflict": func() {
			cli.Command("x", cli.Define("", noop, cli.WithMiddleware(mw, cli.DefineMiddleware("o", func(n cli.HandlerFunc) cli.HandlerFunc { return n }, cli.WithOption(limit, "limit")))))
		},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected declaration panic")
				}
			}()
			declare()
		})
	}
}

func TestMiddlewareShortCircuit(t *testing.T) {
	innerCalled := false
	outer := cli.DefineMiddleware("stop", func(cli.HandlerFunc) cli.HandlerFunc { return func(cli.Context) cli.Outcome { return cli.NOK() } })
	inner := cli.DefineMiddleware("inner", func(next cli.HandlerFunc) cli.HandlerFunc {
		return func(c cli.Context) cli.Outcome { innerCalled = true; return next(c) }
	})
	app := cli.New(cli.Command("stop", cli.Define("", func(cli.Context) cli.Outcome { t.Fatal("handler called"); return cli.OK() }, cli.WithMiddleware(outer, inner))))
	if code := app.Execute(nil, []string{"stop"}, nil, nil, nil); code != 1 || innerCalled {
		t.Fatalf("code=%d inner=%v", code, innerCalled)
	}
}

func TestMiddlewareContributedParamOnCLI(t *testing.T) {
	limit := param.Int("", validate.Check(func(v int) error {
		if v < 0 {
			return fmt.Errorf("limit must be >= 0")
		}
		return nil
	}), param.Default(10))
	mw := newPaginationMiddleware(limit, nil)

	var got int
	app := cli.New(cli.Command("list", cli.Define("", func(ctx cli.Context) cli.Outcome {
		got = cli.Read(ctx, limit)
		return cli.OK()
	}, cli.WithMiddleware(mw))))

	run := func(args ...string) (int, string) {
		var out, errBuf bytes.Buffer
		code := app.Execute(context.Background(), args, nil, &out, &errBuf)
		return code, errBuf.String()
	}
	if code, se := run("list"); code != 0 || got != 10 {
		t.Fatalf("default: code=%d got=%d stderr=%s", code, got, se)
	}
	if code, se := run("list", "--limit=5"); code != 0 || got != 5 {
		t.Fatalf("supplied: code=%d got=%d stderr=%s", code, got, se)
	}
	if code, se := run("list", "--limit=-1"); code != 2 {
		t.Fatalf("invalid: code=%d want 2; stderr=%s", code, se)
	}
}

func TestMiddlewareAliasSetDeduplication(t *testing.T) {
	var validations, mwReads, got int
	limit := param.Int("Page size", validate.Check(func(int) error { validations++; return nil }), param.Default(10))
	mw := cli.DefineMiddleware("page", func(next cli.HandlerFunc) cli.HandlerFunc {
		return func(c cli.Context) cli.Outcome { mwReads = cli.Read(c, limit); return next(c) }
	}, cli.WithOption(limit, "limit", "l", "size"))
	def := cli.Define("", func(c cli.Context) cli.Outcome { got = cli.Read(c, limit); return cli.OK() },
		cli.WithOption(limit, "size", "l", "limit"), cli.WithMiddleware(mw))
	app := cli.New(cli.Command("run", def))
	base := validations
	if code, _, se := run(app, "run", "--limit=1", "-l", "2", "--size", "3"); code != 0 || got != 3 || mwReads != 3 {
		t.Fatalf("code=%d got=%d mw=%d %s", code, got, mwReads, se)
	}
	if validations-base != 1 {
		t.Fatalf("validated %d times, want once", validations-base)
	}
	_, help, _ := run(app, "run", "--help")
	if n := strings.Count(help, "--limit"); n != 1 {
		t.Fatalf("help:\n%s", help)
	}
	// First declaration's display order is retained.
	if !strings.Contains(squash(help), "--size, -l, --limit <int> Page size") {
		t.Fatalf("display order:\n%s", help)
	}
	// A different alias set for the same identity fails.
	defer func() {
		if recover() == nil {
			t.Fatal("expected conflict for differing alias set")
		}
	}()
	cli.Define("", func(cli.Context) cli.Outcome { return cli.OK() }, cli.WithOption(limit, "size", "l"), cli.WithMiddleware(mw))
}

func TestStaleDefaultFailsBeforeCommandMiddleware(t *testing.T) {
	path := t.TempDir() + "/result"
	p := param.OutputFile("", param.Default(path))
	ran := false
	app := cli.New(cli.Command("write", cli.Define("", func(cli.Context) cli.Outcome { ran = true; return cli.OK() }, cli.WithOption(p, "result"), cli.WithMiddleware(cli.DefineMiddleware("probe", func(next cli.HandlerFunc) cli.HandlerFunc {
		return func(c cli.Context) cli.Outcome { ran = true; return next(c) }
	})))))
	if err := os.WriteFile(path, []byte("occupied"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := app.Execute(context.Background(), []string{"write"}, strings.NewReader(""), &out, &stderr); code != 2 {
		t.Fatalf("status %d: %s", code, stderr.String())
	}
	if ran || out.Len() != 0 {
		t.Fatal("invalid default reached interaction")
	}
}

// TestMiddlewarePositionalRejected proves middleware cannot contribute a
// positional argument.
func TestMiddlewarePositionalRejected(t *testing.T) {
	mustPanic(t, "cannot declare positional", func() {
		cli.Define("", noop, cli.WithMiddleware(cli.DefineMiddleware("m",
			func(next cli.HandlerFunc) cli.HandlerFunc { return next },
			cli.WithArgument(param.String(""), "x"))))
	})
}
