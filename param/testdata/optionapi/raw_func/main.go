package main

import "github.com/go-way2go/cli/param"

var _ = param.String("", func(string) error { return nil })

func main() {}
