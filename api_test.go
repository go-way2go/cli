package cli_test

import (
	"os/exec"
	"strings"
	"testing"
)

func goOutput(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("go", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// TestPublicPackageInventory proves the module offers only the root and the
// param, validate, prompt and file packages outside internal/ and testdata.
func TestPublicPackageInventory(t *testing.T) {
	got := strings.Fields(goOutput(t, "list", "./..."))
	want := map[string]bool{
		"github.com/go-way2go/cli":          true,
		"github.com/go-way2go/cli/file":     true,
		"github.com/go-way2go/cli/param":    true,
		"github.com/go-way2go/cli/prompt":   true,
		"github.com/go-way2go/cli/validate": true,
	}
	for _, pkg := range got {
		if strings.Contains(pkg, "/internal/") {
			continue
		}
		if !want[pkg] {
			t.Errorf("unexpected public package %s", pkg)
		}
		delete(want, pkg)
	}
	for pkg := range want {
		t.Errorf("missing public package %s", pkg)
	}
}

// TestRootPublicEntryPoints proves the root documents the public execution
// entry points.
func TestRootPublicEntryPoints(t *testing.T) {
	doc := goOutput(t, "doc", "-all", "github.com/go-way2go/cli")
	for _, required := range []string{"func Define(", "func Command(", "func New(", "func Run(", "func (a App) Run()", "func (a App) Execute("} {
		if !strings.Contains(doc, required) {
			t.Errorf("public API lacks %q", required)
		}
	}
}

// TestDependencyDirection proves the dependency graph points from the root
// toward internals, and that leaf packages and internals never import upward.
func TestDependencyDirection(t *testing.T) {
	const mod = "github.com/go-way2go/cli"
	forbidden := map[string][]string{
		"/param":               {mod, mod + "/internal/command", mod + "/internal/invocation", mod + "/internal/process"},
		"/validate":            {mod, mod + "/internal/command", mod + "/internal/invocation", mod + "/internal/process"},
		"/file":                {mod, mod + "/internal/command", mod + "/internal/invocation", mod + "/internal/process"},
		"/prompt":              {mod, mod + "/internal/command", mod + "/internal/invocation", mod + "/internal/process"},
		"/internal/input":      {mod, mod + "/internal/command", mod + "/internal/invocation", mod + "/internal/process"},
		"/internal/output":     {mod, mod + "/internal/command", mod + "/internal/invocation", mod + "/internal/process"},
		"/internal/process":    {mod, mod + "/internal/command", mod + "/internal/invocation"},
		"/internal/invocation": {mod, mod + "/internal/command", mod + "/internal/process"},
		"/internal/command":    {mod, mod + "/internal/process"},
	}
	for suffix, banned := range forbidden {
		deps := map[string]bool{}
		for _, d := range strings.Fields(goOutput(t, "list", "-deps", mod+suffix)) {
			deps[d] = true
		}
		for _, b := range banned {
			if deps[b] {
				t.Errorf("%s depends on %s", suffix, b)
			}
		}
	}
}
