package command

import (
	"strings"
	"testing"

	"github.com/go-way2go/cli/internal/invocation"
	"github.com/go-way2go/cli/param"
)

func noop(invocation.Context) invocation.Outcome { return invocation.Outcome{OK: true} }

func mustPanic(t *testing.T, contains string, f func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic")
		}
		if msg, _ := r.(string); !strings.Contains(msg, contains) {
			t.Fatalf("panic = %v, want %q", r, contains)
		}
	}()
	f()
}

func TestDefinitionEffectiveMetadata(t *testing.T) {
	source, dest, n := param.String(""), param.String("", param.Default("out")), param.Int("", param.Default(1))
	executed := false
	d := NewDefinition("Copies.", func(invocation.Context) invocation.Outcome { executed = true; return invocation.Outcome{OK: true} }, func(b *Builder) {
		b.Bind(NewArgument(source, "source"))
		b.Bind(NewOption(n, "n", []string{"count"}))
		b.Bind(NewOption(n, "count", []string{"n"})) // equal set in another order deduplicates
		b.Bind(NewArgument(dest, "destination"))
		b.AddMiddleware(func(next invocation.Handler) invocation.Handler { return next }, nil)
		b.AddMiddleware(func(next invocation.Handler) invocation.Handler { return next }, nil)
	})
	if d.IsZero() || d.description != "Copies." {
		t.Fatalf("zero/description = %v/%q", d.IsZero(), d.description)
	}
	if len(d.uses) != 3 || len(d.named) != 1 || len(d.positional) != 2 {
		t.Fatalf("uses %d named %d positional %d", len(d.uses), len(d.named), len(d.positional))
	}
	if d.positional[0].Argument() != "source" || d.positional[1].Argument() != "destination" {
		t.Fatalf("positional order: %+v", d.positional)
	}
	if d.positional[0].hasDefault() || !d.positional[1].hasDefault() || d.positional[1].desc.Default() != "out" {
		t.Fatal("required/default policy not retained from descriptors")
	}
	if got := d.named[0].options; len(got) != 2 || got[0] != "n" || got[1] != "count" {
		t.Fatalf("first declared alias order not kept: %v", got)
	}
	if d.named[0].label() != "-n" || d.positional[0].label() != "argument source" {
		t.Fatalf("labels: %q %q", d.named[0].label(), d.positional[0].label())
	}
	if executed {
		t.Fatal("declaration executed the handler")
	}
}

func TestBindingDeclarationChecks(t *testing.T) {
	p := param.String("")
	mustPanic(t, "non-zero", func() { NewOption(param.Descriptor[string]{}, "x", nil) })
	mustPanic(t, "non-zero", func() { NewOption(nil, "x", nil) })
	mustPanic(t, "NUL", func() { NewOption(p, "a\x00", nil) })
	mustPanic(t, "reserved", func() { NewOption(p, "x", []string{"help"}) })
	mustPanic(t, "repeated", func() { NewOption(p, "ab", []string{"ab"}) })
	mustPanic(t, "cannot declare positional", func() {
		var b Builder
		b.inMiddleware = true
		b.Bind(NewArgument(p, "x"))
	})
	mustPanic(t, "conflicting", func() {
		var b Builder
		b.Bind(NewOption(p, "a", []string{"bb"}))
		b.Bind(NewOption(p, "a", nil))
	})
}
