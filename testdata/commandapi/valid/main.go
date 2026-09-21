package main

import (
	"github.com/go-way2go/cli"
)

var command = cli.Command("x", cli.Define("", func(cli.Context) cli.Outcome { return cli.OK() }, cli.WithMiddleware(cli.DefineMiddleware("audit", func(next cli.HandlerFunc) cli.HandlerFunc { return next }))))

var app = cli.New(command, cli.Group("g", command))

// Both process entry points compile; neither runs here.
func main() {
	if app.Execute(nil, nil, nil, nil, nil) > 0 {
		app.Run()
	}
	cli.Run(command)
}
