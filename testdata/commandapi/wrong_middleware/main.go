package main

import (
	"github.com/go-way2go/cli"
)

var _ = cli.DefineMiddleware("web", func(next func(int) int) func(int) int { return next })

func main() {}
