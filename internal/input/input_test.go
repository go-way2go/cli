package input_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/go-way2go/cli/internal/input"
)

func TestReadLineReadsSuccessiveLinesFromBoundSource(t *testing.T) {
	ctx := input.NewContext(context.Background(), strings.NewReader("first\nsecond\n"))

	got, err := input.ReadLine(ctx)
	if err != nil {
		t.Fatalf("ReadLine #1: err = %v, want nil", err)
	}
	if got != "first" {
		t.Fatalf("ReadLine #1 = %q, want %q", got, "first")
	}

	got, err = input.ReadLine(ctx)
	if err != nil {
		t.Fatalf("ReadLine #2: err = %v, want nil", err)
	}
	if got != "second" {
		t.Fatalf("ReadLine #2 = %q, want %q", got, "second")
	}

	// A third call against the same, now-exhausted, bound source proves the
	// persistent *bufio.Reader carries no stale buffered data forward: after
	// consuming both lines, the source reads as truly exhausted.
	got, err = input.ReadLine(ctx)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("ReadLine #3: err = %v, want io.EOF", err)
	}
	if got != "" {
		t.Fatalf("ReadLine #3 = %q, want empty", got)
	}
}

// TestReadLineWithLongLinesSurvivesAcrossCalls is the regression test for
// the "own one persistent buffered reader" requirement: a single NewContext
// call followed by several ReadLine calls against input containing lines
// longer than bufio's default 4096-byte buffer size must still round-trip
// every line correctly. If ReadLine ever regressed to constructing a fresh
// *bufio.Reader per call, a fresh reader's own internal read-ahead past the
// first newline into later lines would be silently discarded when that
// reader is dropped, corrupting later calls — this test would catch that.
func TestReadLineWithLongLinesSurvivesAcrossCalls(t *testing.T) {
	long := func(b byte, n int) string { return strings.Repeat(string(b), n) }
	lines := []string{
		long('a', 100),
		long('b', 5000), // longer than bufio's default 4096-byte buffer
		long('c', 200),
		"short",
	}
	ctx := input.NewContext(context.Background(), strings.NewReader(strings.Join(lines, "\n")+"\n"))

	for i, want := range lines {
		got, err := input.ReadLine(ctx)
		if err != nil {
			t.Fatalf("ReadLine #%d: err = %v, want nil", i, err)
		}
		if got != want {
			t.Fatalf("ReadLine #%d: len(got) = %d, len(want) = %d, equal = %v", i, len(got), len(want), got == want)
		}
	}
}

func TestReadLineStripsTrailingCarriageReturn(t *testing.T) {
	ctx := input.NewContext(context.Background(), strings.NewReader("windows\r\nunix\n"))

	got, err := input.ReadLine(ctx)
	if err != nil {
		t.Fatalf("ReadLine #1: err = %v, want nil", err)
	}
	if got != "windows" {
		t.Fatalf("ReadLine #1 = %q, want %q", got, "windows")
	}

	got, err = input.ReadLine(ctx)
	if err != nil {
		t.Fatalf("ReadLine #2: err = %v, want nil", err)
	}
	if got != "unix" {
		t.Fatalf("ReadLine #2 = %q, want %q", got, "unix")
	}
}

func TestReadLineOnUnterminatedTrailingDataReturnsSuccessfulLine(t *testing.T) {
	ctx := input.NewContext(context.Background(), strings.NewReader("no newline"))

	got, err := input.ReadLine(ctx)
	if err != nil {
		t.Fatalf("err = %v, want nil (unterminated trailing data is a successful final line)", err)
	}
	if got != "no newline" {
		t.Fatalf("got = %q, want %q", got, "no newline")
	}
}

func TestReadLineOnExhaustedSourceReturnsEOF(t *testing.T) {
	ctx := input.NewContext(context.Background(), strings.NewReader(""))

	got, err := input.ReadLine(ctx)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want io.EOF", err)
	}
	if got != "" {
		t.Fatalf("got = %q, want empty", got)
	}
}

func TestNewContextWithNilReaderReadsAsExhausted(t *testing.T) {
	ctx := input.NewContext(context.Background(), nil)

	got, err := input.ReadLine(ctx)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want io.EOF", err)
	}
	if got != "" {
		t.Fatalf("got = %q, want empty", got)
	}
}

// TestReadLineReturnsPromptlyWhenContextAlreadyCancelled proves ReadLine's
// cooperative cancellation check: a context cancelled before ReadLine is
// ever called must return ctx.Err() immediately, without hanging and
// without attempting to read the bound source.
func TestReadLineReturnsPromptlyWhenContextAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ctx = input.NewContext(ctx, strings.NewReader("unread\n"))
	cancel()

	got, err := input.ReadLine(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if got != "" {
		t.Fatalf("got = %q, want empty", got)
	}
}

func TestReadLineReturnsCancellationAfterBlockedReaderIsReleased(t *testing.T) {
	reader := &gateReader{started: make(chan struct{}), release: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	ctx = input.NewContext(ctx, reader)

	result := make(chan error, 1)
	go func() {
		_, err := input.ReadLine(ctx)
		result <- err
	}()
	<-reader.started
	cancel()
	close(reader.release)

	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

type gateReader struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *gateReader) Read(p []byte) (int, error) {
	first := false
	r.once.Do(func() {
		first = true
		close(r.started)
	})
	if !first {
		return 0, io.EOF
	}
	<-r.release
	copy(p, "line\x0a")
	return len("line\x0a"), io.EOF
}

func TestContextWithoutSourceFallsBackWithoutPanicking(t *testing.T) {
	// A handler-shaped function invoked directly, outside any cli execution
	// boundary, must not panic just because no source was injected. It
	// falls back to os.Stdin, which in a non-interactive test process reads
	// as exhausted rather than blocking.
	_, err := input.ReadLine(context.Background())
	if err == nil {
		t.Fatal("expected a non-nil error reading os.Stdin fallback in a test process")
	}
}

// TestConcurrentContextsAreIsolated proves the package's central guarantee,
// mirroring output.TestConcurrentContextsAreIsolated: two independently
// created contexts, each carrying its own reader, never see each other's
// input, even under concurrent use — input holds no package-global reader
// state that could let that happen.
func TestConcurrentContextsAreIsolated(t *testing.T) {
	const n = 50
	var wg sync.WaitGroup
	results := make([]string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx := input.NewContext(context.Background(), strings.NewReader(fmt.Sprintf("n-%d\n", i)))
			line, err := input.ReadLine(ctx)
			if err != nil {
				t.Errorf("goroutine %d: err = %v, want nil", i, err)
				return
			}
			results[i] = line
		}(i)
	}
	wg.Wait()

	for i, got := range results {
		want := fmt.Sprintf("n-%d", i)
		if got != want {
			t.Errorf("goroutine %d: got %q, want %q", i, got, want)
		}
	}
}
