package cli_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandPublicAPICompilation(t *testing.T) {
	for name, want := range map[string]string{
		"wrong_handler":        "cannot use",
		"raw_definition_new":   "cannot use def",
		"raw_definition_run":   "cannot use def",
		"raw_definition_group": "cannot use def",
		"wrong_middleware":     "cannot use",
		"sealed_node":          "missing method node",
		"sealed_option":        "missing method applyCommand",
		"valid":                "",
	} {
		t.Run(name, func(t *testing.T) {
			out, err := exec.Command("go", "build", "-o", filepath.Join(t.TempDir(), "fixture"), "./testdata/commandapi/"+name).CombinedOutput()
			if want == "" {
				if err != nil {
					t.Fatalf("valid API failed: %v\n%s", err, out)
				}
				return
			}
			if err == nil || !strings.Contains(string(out), want) {
				t.Fatalf("expected compilation failure containing %q: %v\n%s", want, err, out)
			}
		})
	}
}

func TestBindingPublicAPICompilation(t *testing.T) {
	for name, want := range map[string]string{
		"valid":            "",
		"non_descriptor":   "cannot use",
		"argument_aliases": "too many arguments",
		"option_no_name":   "not enough arguments",
	} {
		t.Run(name, func(t *testing.T) {
			out, err := exec.Command("go", "build", "-o", filepath.Join(t.TempDir(), "fixture"), "./testdata/bindingapi/"+name).CombinedOutput()
			if want == "" {
				if err != nil {
					t.Fatalf("valid API failed: %v\n%s", err, out)
				}
				return
			}
			if err == nil || !strings.Contains(string(out), want) {
				t.Fatalf("expected compilation failure containing %q: %v\n%s", want, err, out)
			}
		})
	}
}
