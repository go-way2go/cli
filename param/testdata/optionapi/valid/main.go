package main

import (
	"errors"

	"github.com/go-way2go/cli/param"
	"github.com/go-way2go/cli/validate"
)

var _ = param.String("Search text",
	validate.NonEmpty(),
	validate.Check(func(s string) error { return errors.New("custom") }))

func main() {}
