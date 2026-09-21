package main

import (
	"github.com/go-way2go/cli/param"
	"github.com/go-way2go/cli/validate"
)

var typ = param.DefineType("even", func(string) (int, error) { return 0, nil })

var _ = param.Of(typ, "", validate.NonEmpty())

func main() {}
