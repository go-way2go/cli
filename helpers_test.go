package cli_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/go-way2go/cli"
)

func noop(ctx cli.Context) cli.Outcome { return cli.OK() }

func mustPanic(t *testing.T, contains string, f func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic")
		}
		if msg := fmt.Sprint(r); !strings.Contains(msg, contains) {
			t.Fatalf("panic = %q, want it to contain %q", msg, contains)
		}
	}()
	f()
}

func run(app cli.App, args ...string) (int, string, string) {
	var out, errBuf bytes.Buffer
	code := app.Execute(context.Background(), args, nil, &out, &errBuf)
	return code, out.String(), errBuf.String()
}
