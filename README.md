# github.com/go-way2go/cli

Build command-line applications with declarative commands, typed parameters,
validation, and middleware.

## Installation

Requires Go 1.25.2 or newer.

```sh
go get github.com/go-way2go/cli
```

## Example

Save as `main.go` in a module that requires the library:

```go
package main

import (
	"fmt"

	"github.com/go-way2go/cli"
	"github.com/go-way2go/cli/param"
	"github.com/go-way2go/cli/validate"
)

var (
	name  = param.String("Who to greet.")
	count = param.Int("Number of greetings.", validate.Min(1), param.Default(1))
)

func greet(ctx cli.Context) cli.Outcome {
	for range cli.Read(ctx, count) {
		fmt.Fprintf(ctx.Stdout(), "Hello, %s!\n", cli.Read(ctx, name))
	}
	return cli.OK()
}

var greetDef = cli.Define("Greet someone.", greet,
	cli.WithArgument(name, "name"),
	cli.WithOption(count, "count", "c"),
)

func main() {
	cli.Run(cli.Command("greet", greetDef))
}
```

```sh
go run . greet Ada             # Hello, Ada!
go run . greet Ada -c 2        # Prints the greeting twice.
go run . greet Ada --count 0   # Validation error; handler does not run.
go run . greet --help          # Generated command help.
```

`cli.Define` creates a reusable, nameless command definition and
`cli.Command(name, definition)` registers it, so the command tree is visible where
it is assembled. `cli.Run(nodes...)` starts the process; `cli.New(nodes...).Execute(...)`
provides injectable execution for tests. Exit codes are 0 (success), 1 (failure),
2 (invalid input), and 130/143 (SIGINT/SIGTERM). A parameter without
`param.Default` is required; with one it is optional, including zero values.
Defaults are retained by reference and must not be mutated after declaration.

## API

[Package documentation](https://pkg.go.dev/github.com/go-way2go/cli) covers
commands, groups, middleware, contexts, and outcomes. Public helpers:
[param](https://pkg.go.dev/github.com/go-way2go/cli/param),
[validate](https://pkg.go.dev/github.com/go-way2go/cli/validate),
[prompt](https://pkg.go.dev/github.com/go-way2go/cli/prompt), and
[file](https://pkg.go.dev/github.com/go-way2go/cli/file).
See also the [executable examples](example_test.go).

[CI](.github/workflows/ci.yml) checks formatting, build, vet, tests and race
conditions on Linux and macOS. Windows is untested. Secret-prompt terminal
behavior uses simulated tests; real-TTY echo suppression/restoration is not
automatically verified. Signal tests use subprocesses, not a real terminal.

[MIT license](LICENSE) · [way2go.dev](https://way2go.dev).
