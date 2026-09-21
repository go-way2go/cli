package main

import (
	"github.com/go-way2go/cli"
	"github.com/go-way2go/cli/param"
)

var _ = cli.WithArgument(param.String(""), "a", "b")

func main() {}
