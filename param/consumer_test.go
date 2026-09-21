package param_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestIndependentLeafConsumer(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	mod := "module leafconsumer\n\ngo 1.25.2\n\nrequire github.com/go-way2go/cli v0.0.0\nreplace github.com/go-way2go/cli => " + filepath.ToSlash(root) + "\n"
	source := `package leafconsumer
import (
 "context"
 "errors"
 "testing"
 "github.com/go-way2go/cli/param"
 "github.com/go-way2go/cli/validate"
)
func TestSharedValidation(t *testing.T) {
 shared := validate.Check(func(s string) error { if s=="bad" { return errors.New("bad") }; return nil })
 first := param.String("",shared, param.Default("fallback"))
 second := param.String("",shared, validate.NonEmpty())
 parameters := []param.AnyDescriptor{first, second}
 values,err:=param.Prepare(parameters,map[param.AnyDescriptor]param.RawValue{second:{Present:true,Value:"x"}})
 if err!=nil { t.Fatal(err) }
 ctx:=param.NewContext(context.Background(),values)
 if param.Read(ctx,first)!="fallback" || param.Read(ctx,second)!="x" { t.Fatal("presence semantics changed") }
 for _, p:=range []param.Descriptor[string]{first,second} {
  if _,err:=param.Prepare([]param.AnyDescriptor{p},map[param.AnyDescriptor]param.RawValue{p:{Present:true,Value:"bad"}});err==nil { t.Fatal("shared validator not applied") }
 }
 if _,err:=param.Prepare([]param.AnyDescriptor{second},map[param.AnyDescriptor]param.RawValue{second:{Present:true,Value:""}});err==nil { t.Fatal("explicit empty must be validated") }
}
`
	for name, content := range map[string]string{"go.mod": mod, "consumer_test.go": source} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("go", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOWORK=off")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("go %v: %v\n%s", args, err, output)
		}
		return string(output)
	}
	run("test", "-mod=mod", ".")
	deps := run("list", "-deps", "-test", ".")
	for _, dep := range strings.Fields(deps) {
		if dep == "github.com/go-way2go/cli" || strings.HasPrefix(dep, "github.com/go-way2go/gui") {
			t.Fatalf("execution dependency: %s", dep)
		}
	}
}
