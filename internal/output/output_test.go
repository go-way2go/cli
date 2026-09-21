package output_test

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/go-way2go/cli/internal/output"
)

func TestPrintlnAndErrorlnWriteToInjectedSinks(t *testing.T) {
	var out, errBuf bytes.Buffer
	ctx := output.NewContext(context.Background(), &out, &errBuf)

	output.Println(ctx, "hello", 42)
	output.Errorln(ctx, "oops")

	if got, want := out.String(), "hello 42\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if got, want := errBuf.String(), "oops\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}

func TestPrintfAndErrorfFormat(t *testing.T) {
	var out, errBuf bytes.Buffer
	ctx := output.NewContext(context.Background(), &out, &errBuf)

	output.Printf(ctx, "n=%d\n", 7)
	output.Errorf(ctx, "err=%s\n", "bad")

	if got, want := out.String(), "n=7\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if got, want := errBuf.String(), "err=bad\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}

// TestConcurrentContextsAreIsolated proves the package's central guarantee:
// two independently created contexts, each carrying its own buffer, never
// see each other's writes, even under concurrent use — output holds no
// package-global writer state that could let that happen.
func TestConcurrentContextsAreIsolated(t *testing.T) {
	const n = 50
	var wg sync.WaitGroup
	results := make([]string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var buf bytes.Buffer
			ctx := output.NewContext(context.Background(), &buf, nil)
			output.Println(ctx, "n", i)
			results[i] = buf.String()
		}(i)
	}
	wg.Wait()

	for i, got := range results {
		want := fmt.Sprintf("n %d\n", i)
		if got != want {
			t.Errorf("goroutine %d: got %q, want %q", i, got, want)
		}
	}
}

func TestContextWithoutSinksFallsBackWithoutPanicking(t *testing.T) {
	// A handler-shaped function invoked directly, outside any cli execution
	// boundary, must not panic just because no sinks were injected.
	output.Println(context.Background(), "no sinks bound, should not panic")
}
