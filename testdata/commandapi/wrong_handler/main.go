package main

import (
	"context"
	"github.com/go-way2go/cli"
)

var _ = cli.Command("x", cli.Define("", func(context.Context) cli.Outcome { return cli.OK() }))

func main() {}
