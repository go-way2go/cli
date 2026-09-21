package cli_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/go-way2go/cli"
	"github.com/go-way2go/cli/internal/input"
	"github.com/go-way2go/cli/internal/output"
	"github.com/go-way2go/cli/param"
	"github.com/go-way2go/cli/prompt"
)

func TestContextResourcesAndDerivation(t *testing.T) {
	p := param.Int("", param.Default(7))
	values, err := param.Prepare([]param.AnyDescriptor{p}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	type key struct{}
	parent, cancel := context.WithDeadline(context.WithValue(context.Background(), key{}, "caller"), time.Now().Add(time.Minute))
	defer cancel()
	c := cli.NewContext(input.MarkInteractive(parent), values, strings.NewReader("first\nXYZlast\n"), &out, &stderr)
	if cli.Read(c, p) != 7 || param.Read(c.Context(), p) != 7 {
		t.Fatal("lost parameters")
	}
	if c.Context().Value(key{}) != "caller" || !input.Interactive(c.Context()) {
		t.Fatal("lost caller metadata")
	}
	if got, ok := c.Context().Deadline(); !ok {
		t.Fatal("lost deadline")
	} else if want, _ := parent.Deadline(); got != want {
		t.Fatal("changed deadline")
	}
	line, err := input.ReadLine(c.Context())
	if err != nil || line != "first" {
		t.Fatalf("first: %q %v", line, err)
	}
	derived := c.WithContext(context.WithValue(c.Context(), key{}, "derived"))
	b := make([]byte, 3)
	if _, err := io.ReadFull(derived.Stdin(), b); err != nil || string(b) != "XYZ" {
		t.Fatalf("direct: %q %v", b, err)
	}
	line, err = input.ReadLine(c.Context())
	if err != nil || line != "last" {
		t.Fatalf("remaining: %q %v", line, err)
	}
	io.WriteString(derived.Stdout(), "direct ")
	output.Println(derived.Context(), "helper")
	io.WriteString(derived.Stderr(), "error ")
	output.Errorln(derived.Context(), "helper")
	if out.String() != "direct helper\n" || stderr.String() != "error helper\n" {
		t.Fatal("streams diverged")
	}
	if derived.Context().Value(key{}) != "derived" || cli.Read(derived, p) != 7 {
		t.Fatal("lost derived state")
	}
	cancel()
	if !errors.Is(derived.Context().Err(), context.Canceled) {
		t.Fatal("lost cancellation")
	}
	replacement := c.WithContext(nil)
	if replacement.Context().Err() != nil || replacement.Context().Value(key{}) != nil || cli.Read(replacement, p) != 7 {
		t.Fatal("replacement parent did not preserve only resources")
	}
}

func TestContextZeroAndNilAreSafe(t *testing.T) {
	for _, c := range []cli.Context{{}, cli.NewContext(nil, nil, nil, nil, nil)} {
		if c.Context() == nil || c.Context().Err() != nil {
			t.Fatal("invalid standard context")
		}
		if _, err := c.Stdin().Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
			t.Fatalf("input: %v", err)
		}
		if _, err := input.ReadLine(c.Context()); !errors.Is(err, io.EOF) {
			t.Fatalf("helper input: %v", err)
		}
		output.Println(c.Context(), "discard")
		output.Errorln(c.Context(), "discard")
		io.WriteString(c.Stdout(), "discard")
		io.WriteString(c.Stderr(), "discard")
		func() {
			defer func() {
				if _, ok := recover().(*param.UndeclaredReadError); !ok {
					t.Fatal("expected undeclared-read error")
				}
			}()
			cli.Read(c, param.String(""))
		}()
	}
}

func TestContextPromptResources(t *testing.T) {
	var out, stderr bytes.Buffer
	c := cli.NewContext(nil, nil, strings.NewReader("secret\n"), &out, &stderr)
	got, err := prompt.ReadSecret(c.Context(), "Secret", func(s string) (string, error) { return s, nil })
	if err != nil || got != "secret" {
		t.Fatalf("secret: %q %v", got, err)
	}
	if strings.Contains(out.String()+stderr.String(), "secret") {
		t.Fatal("secret leaked")
	}
	_, err = prompt.Read(c.Context(), "Again", func(s string) (string, error) { return s, nil })
	if !errors.Is(err, io.EOF) {
		t.Fatalf("EOF: %v", err)
	}
	parent, cancel := context.WithCancel(c.Context())
	cancel()
	out.Reset()
	stderr.Reset()
	_, err = prompt.Read(c.WithContext(parent).Context(), "Canceled", func(s string) (string, error) { return s, nil })
	if !errors.Is(err, context.Canceled) || out.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("canceled: %v stdout %q stderr %q", err, out.String(), stderr.String())
	}
}

func TestContextSharedDirectAndPromptedInput(t *testing.T) {
	var out, stderr bytes.Buffer
	c := cli.NewContext(nil, nil, strings.NewReader("one\ntwo\nthree\nfour\n"), &out, &stderr)
	identity := func(s string) (string, error) { return s, nil }
	got, err := prompt.Read(c.Context(), "First", identity)
	if err != nil || got != "one" {
		t.Fatalf("first: %q %v", got, err)
	}
	b := make([]byte, 4)
	if _, err := io.ReadFull(c.Stdin(), b); err != nil || string(b) != "two\n" {
		t.Fatalf("direct: %q %v", b, err)
	}
	type key struct{}
	derived := c.WithContext(context.WithValue(context.Background(), key{}, "derived"))
	got, err = prompt.Read(derived.Context(), "Third", identity)
	if err != nil || got != "three" {
		t.Fatalf("derived: %q %v", got, err)
	}
	if _, err := io.ReadFull(derived.Stdin(), b[:1]); err != nil || b[0] != 'f' {
		t.Fatalf("after derived: %q %v", b[:1], err)
	}
	got, err = prompt.Read(c.Context(), "Rest", identity)
	if err != nil || got != "our" {
		t.Fatalf("rest: %q %v", got, err)
	}
	if out.Len() != 0 || stderr.String() != "First\nThird\nRest\n" {
		t.Fatalf("stdout %q stderr %q", out.String(), stderr.String())
	}
}
