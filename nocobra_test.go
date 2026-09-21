package cli_test

import (
	"encoding/json"
	"go/importer"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Parser types must remain behind the public API, including exported fields,
// aliases, promoted methods and generic constraints in all public packages.
func TestNoCobraOrPflagInExportedSurface(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "-export", "-json", "./...").Output()
	if err != nil {
		t.Fatal(err)
	}
	var public []string
	exports := map[string]string{}
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for dec.More() {
		var p struct {
			ImportPath, Export string
			DepOnly            bool
		}
		if err := dec.Decode(&p); err != nil {
			t.Fatal(err)
		}
		exports[p.ImportPath] = p.Export
		if !p.DepOnly && !strings.Contains(p.ImportPath, "/internal/") {
			public = append(public, p.ImportPath)
		}
	}
	imp := importer.ForCompiler(token.NewFileSet(), "gc", func(path string) (io.ReadCloser, error) {
		return os.Open(exports[path])
	})
	for _, path := range public {
		pkg, err := imp.Import(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range pkg.Scope().Names() {
			obj := pkg.Scope().Lookup(name)
			if obj.Exported() {
				checkParserTypes(t, obj.Type(), obj.String(), map[types.Type]bool{})
			}
		}
	}
}

func checkParserTypes(t *testing.T, typ types.Type, entry string, seen map[types.Type]bool) {
	t.Helper()
	if typ == nil || seen[typ] {
		return
	}
	seen[typ] = true
	visit := func(typ types.Type) { checkParserTypes(t, typ, entry, seen) }
	checkName := func(obj *types.TypeName) {
		if pkg := obj.Pkg(); pkg != nil && (pkg.Path() == "github.com/spf13/cobra" || pkg.Path() == "github.com/spf13/pflag") {
			t.Errorf("%s exposes parser type %s", entry, obj)
		}
	}
	switch typ := typ.(type) {
	case *types.Alias:
		checkName(typ.Obj())
		visit(types.Unalias(typ))
	case *types.Named:
		checkName(typ.Obj())
		for i := 0; i < typ.TypeArgs().Len(); i++ {
			visit(typ.TypeArgs().At(i))
		}
		for i := 0; i < typ.TypeParams().Len(); i++ {
			visit(typ.TypeParams().At(i))
		}
		// A pointer method set includes value, pointer and promoted methods.
		methods := types.NewMethodSet(types.NewPointer(typ))
		for i := 0; i < methods.Len(); i++ {
			if method := methods.At(i).Obj(); method.Exported() {
				visit(method.Type())
			}
		}
		visit(typ.Underlying())
	case *types.Pointer:
		visit(typ.Elem())
	case *types.Slice:
		visit(typ.Elem())
	case *types.Array:
		visit(typ.Elem())
	case *types.Map:
		visit(typ.Key())
		visit(typ.Elem())
	case *types.Chan:
		visit(typ.Elem())
	case *types.Struct:
		for i := 0; i < typ.NumFields(); i++ {
			if field := typ.Field(i); field.Exported() || field.Embedded() {
				visit(field.Type())
			}
		}
	case *types.Signature:
		visit(typ.Params())
		visit(typ.Results())
		for i := 0; i < typ.TypeParams().Len(); i++ {
			visit(typ.TypeParams().At(i))
		}
	case *types.Tuple:
		for i := 0; i < typ.Len(); i++ {
			visit(typ.At(i).Type())
		}
	case *types.Interface:
		for i := 0; i < typ.NumMethods(); i++ {
			if method := typ.Method(i); method.Exported() {
				visit(method.Type())
			}
		}
		for i := 0; i < typ.NumEmbeddeds(); i++ {
			visit(typ.EmbeddedType(i))
		}
	case *types.TypeParam:
		visit(typ.Constraint())
	case *types.Union:
		for i := 0; i < typ.Len(); i++ {
			visit(typ.Term(i).Type())
		}
	}
}
