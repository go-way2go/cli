package cli_test

// Testing real SIGINT/SIGTERM delivery to App.Run requires a real OS
// process: Run calls os.Exit, which would kill the test binary itself if
// invoked in-process. This file uses the standard Go subprocess-test-helper
// idiom (the same pattern used throughout the Go standard library's own
// os/exec tests): TestHelperProcess is a normal-looking test, gated at its
// very top by an env var so it is a no-op under an ordinary `go test` run,
// but when re-executed as a subprocess with that env var set, it runs a
// tiny App.Run() setup for real — including Run's real os.Exit call. Each
// parent test below starts that subprocess, waits for it to reach its
// blocking input.ReadLine call, sends a real signal, and asserts the
// resulting process exit code matches the conventional shell signal-exit
// convention App.Run implements: SIGINT -> 130, SIGTERM -> 143.

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/go-way2go/cli"
	"github.com/go-way2go/cli/internal/input"
)

// TestHelperProcess is not a real test: under a plain `go test` run
// WAY2GO_CLI_HELPER is unset, so it returns immediately and asserts
// nothing. Re-executed as a subprocess (see runSignalHelper below) with
// WAY2GO_CLI_HELPER=1, it builds a one-Command App whose handler blocks on
// input.ReadLine(ctx.Context()) — reading through App.Run's real os.Stdin — and calls
// App.Run(), which never returns: it always concludes with os.Exit.
func TestHelperProcess(t *testing.T) {
	entry := os.Getenv("WAY2GO_CLI_HELPER")
	if entry == "" {
		return
	}

	os.Args = []string{"way2go-cli-signal-helper", os.Getenv("WAY2GO_CLI_ARG")}
	wait := cli.Command("wait", cli.Define("", func(ctx cli.Context) cli.Outcome {
		_, _ = input.ReadLine(ctx.Context())
		return cli.OK()
	}))
	ok := cli.Command("ok", cli.Define("", func(cli.Context) cli.Outcome { return cli.OK() }))
	nok := cli.Command("nok", cli.Define("", func(cli.Context) cli.Outcome { return cli.NOK() }))

	// Tell the parent we are about to enter the process entry point (and
	// shortly after, the blocking ReadLine) so it knows it is safe to signal us.
	os.Stderr.WriteString("ready\n")
	if entry == "run" {
		cli.Run(wait, ok, nok)
	}
	cli.New(wait, ok, nok).Run()
}

// safeBuffer is a mutex-guarded bytes.Buffer, used as cmd.Stderr: os/exec
// writes to it from its own internal copy goroutine while the parent test
// goroutine concurrently reads it back (to look for the child's "ready"
// marker, and to include it in failure messages), so it needs its own
// locking rather than a bare bytes.Buffer.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// runSignalHelper starts TestHelperProcess as a subprocess, waits for its
// "ready" marker, sends sig, and returns the subprocess's final exit code.
// It is robust against ordinary scheduling jitter: the "ready" marker,
// polled for rather than assumed after a fixed sleep, proves the child has
// actually started before anything is signaled, and both waits below use a
// generous timeout so a slow CI machine does not produce a flaky failure.
func runSignalHelper(t *testing.T, entry string, sig syscall.Signal) int {
	t.Helper()

	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
	cmd.Env = append(os.Environ(), "WAY2GO_CLI_HELPER="+entry, "WAY2GO_CLI_ARG=wait")

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("StdinPipe: %v", err)
	}
	defer stdin.Close()

	var stderrBuf safeBuffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	waitForReadyMarker(t, &stderrBuf)

	// A brief additional pause so the child has time to actually reach its
	// blocking ReadLine call (past the "ready" print, through Cobra's
	// dispatch) before we signal it.
	time.Sleep(200 * time.Millisecond)

	if err := cmd.Process.Signal(sig); err != nil {
		t.Fatalf("Signal(%v): %v", sig, err)
	}

	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()

	select {
	case err := <-waitDone:
		if err != nil {
			if _, ok := err.(*exec.ExitError); !ok {
				t.Fatalf("Wait: unexpected error type: %v (stderr=%s)", err, stderrBuf.String())
			}
		}
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		t.Fatalf("child did not exit within timeout after signal %v; stderr=%s", sig, stderrBuf.String())
	}

	return cmd.ProcessState.ExitCode()
}

// waitForReadyMarker polls buf for the child's "ready\n" line, failing the
// test if it never appears within a generous timeout.
func waitForReadyMarker(t *testing.T, buf *safeBuffer) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), "ready\n") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("child never printed its ready marker; stderr so far=%s", buf.String())
}

// processEntries are the two process entry points, which must behave
// identically: App.Run ("app") and package-level cli.Run ("run").
var processEntries = []string{"app", "run"}

// TestRunExitsWithConventionalCodeOnSIGINT proves both process entry points map
// a received SIGINT to exit code 130 (128+SIGINT), not whatever Execute itself
// would have returned, and do so promptly even while the handler's ReadLine
// call remains blocked on a stdin that never produces data or EOF.
func TestRunExitsWithConventionalCodeOnSIGINT(t *testing.T) {
	for _, entry := range processEntries {
		if got, want := runSignalHelper(t, entry, syscall.SIGINT), 130; got != want {
			t.Fatalf("%s: exit code = %d, want %d", entry, got, want)
		}
	}
}

// TestRunExitsWithConventionalCodeOnSIGTERM is SIGINT's counterpart for
// SIGTERM -> 143 (128+SIGTERM).
func TestRunExitsWithConventionalCodeOnSIGTERM(t *testing.T) {
	for _, entry := range processEntries {
		if got, want := runSignalHelper(t, entry, syscall.SIGTERM), 143; got != want {
			t.Fatalf("%s: exit code = %d, want %d", entry, got, want)
		}
	}
}

// TestRunExitsWithExecuteStatus proves both process entry points exit with the
// status Execute maps the outcome to.
func TestRunExitsWithExecuteStatus(t *testing.T) {
	for _, entry := range processEntries {
		for arg, want := range map[string]int{"ok": 0, "nok": 1, "unknown": 2} {
			cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
			cmd.Env = append(os.Environ(), "WAY2GO_CLI_HELPER="+entry, "WAY2GO_CLI_ARG="+arg)
			err := cmd.Run()
			got := 0
			if ee, ok := err.(*exec.ExitError); ok {
				got = ee.ExitCode()
			} else if err != nil {
				t.Fatalf("%s %s: %v", entry, arg, err)
			}
			if got != want {
				t.Fatalf("%s %s: exit code = %d, want %d", entry, arg, got, want)
			}
		}
	}
}
