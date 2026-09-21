package main

import (
	"github.com/go-way2go/cli"
)

type custom struct{}

var _ cli.CommandOption = custom{}

func main() {}
