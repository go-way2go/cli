package main

import "github.com/go-way2go/cli/param"

type option struct{}

var _ = param.String("", option{})

func main() {}
