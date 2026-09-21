package main

import "github.com/go-way2go/cli"

var def = cli.Define("", func(cli.Context) cli.Outcome { return cli.OK() })

func main() { cli.Run(def) }
