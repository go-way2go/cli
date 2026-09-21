package main

import (
	"github.com/go-way2go/cli"
)

type custom struct{}

var _ cli.Node = custom{}

func main() {}
