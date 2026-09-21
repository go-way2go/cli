package main

import (
	"github.com/go-way2go/cli/param"
	"github.com/go-way2go/cli/validate"
)

var _ = param.String("", validate.Min(1))

func main() {}
