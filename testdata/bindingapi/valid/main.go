package main

import (
	"errors"
	"strings"

	"github.com/go-way2go/cli"
	"github.com/go-way2go/cli/param"
	"github.com/go-way2go/cli/validate"
)

type ID string

type Config struct{ Name string }

var (
	query   = param.String("Search text", validate.NonEmpty())
	limit   = param.Int("Maximum results", validate.Min(1), param.Default(20))
	verbose = param.Bool("Verbose output", param.Default(false))
	id      = param.Of(param.DefineType("id", func(s string) (ID, error) { return ID(s), nil }), "", param.Default(ID("default")))
	cfg     = param.Of(param.DefineType("config", func(s string) (*Config, error) {
		if s == "" {
			return nil, errors.New("empty")
		}
		return &Config{strings.TrimSpace(s)}, nil
	}), "", param.Default[*Config](nil))
)

// Mixed-type bindings share one non-generic facade.
var _ = cli.Command("search", cli.Define("", func(cli.Context) cli.Outcome { return cli.OK() },
	cli.WithArgument(query, "text"),
	cli.WithOption(limit, "limit", "l"),
	cli.WithOption(verbose, "v", "verbose"),
	cli.WithOption(id, "id"),
	cli.WithOption(cfg, "config", "c", "cfg")))

func main() {}
