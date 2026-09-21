package main

import (
	"errors"

	"github.com/go-way2go/cli/file"
	"github.com/go-way2go/cli/param"
	"github.com/go-way2go/cli/validate"
)

type endpoint struct{ host string }

var endpointType = param.DefineType("endpoint", func(s string) (*endpoint, error) { return &endpoint{s}, nil })

var (
	_ = param.Int("Limit", validate.Min(1), param.Default(10))
	_ = param.Bool("Verbose", param.Default(false))
	_ = param.String("Name", param.Default(""), validate.NonEmpty())
	_ = param.File("Path", file.Extension(".txt"), param.Default("a.txt"))
	_ = param.OutputFile("Output", validate.Check(func(string) error { return errors.New("x") }))
	_ = param.Of(endpointType, "Endpoint", param.Default[*endpoint](nil),
		validate.Check(func(*endpoint) error { return nil }))
)

func main() {}
