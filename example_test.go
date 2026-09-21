package cli_test

import (
	"bytes"
	"context"
	"fmt"

	"github.com/go-way2go/cli"
	"github.com/go-way2go/cli/param"
)

// exampleQuery is a required string bound to a long CLI option.
var exampleQuery = param.String("Search query text.")

// search reads typed input and writes to its invocation output.
func search(ctx cli.Context) cli.Outcome {
	q := cli.Read(ctx, exampleQuery)
	fmt.Fprintf(ctx.Stdout(), "results for %q\n", q)
	return cli.OK()
}

// searchDef defines the command and its parameter binding; exampleSearch
// registers it under the name "search".
var searchDef = cli.Define("Searches for things.", search,
	cli.WithOption(exampleQuery, "query"))

var exampleSearch = cli.Command("search", searchDef)

// Example_search runs the command tree built from one CLI command exactly
// as a real process invocation would, but through Execute's explicit
// args/output-sink parameters so the example stays deterministic: no
// os.Args, no real process stdout.
func Example_search() {
	app := cli.New(exampleSearch)

	var out, errOut bytes.Buffer
	code := app.Execute(context.Background(), []string{"search", "--query", "go"}, nil, &out, &errOut)

	fmt.Print(out.String())
	fmt.Println("exit code:", code)
	// Output:
	// results for "go"
	// exit code: 0
}
