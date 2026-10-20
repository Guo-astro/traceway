// Command twevals runs the Traceway skill and MCP evals with the fake model
// that plays each case's oracle. It makes no paid model calls.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

const usage = `twevals: Traceway skill and MCP evals (oracle runs only, free)

  twevals list    [--preset P] [--suites S] [--tags T] [--cases GLOB] [--arms A]
  twevals oracle  [--all | <case-id>... | --suites S] [--arms A] [-j N] [--keep] [--out DIR]

Selection flags: --preset smoke|core|full, --cases (comma-separated ids or globs; overrides the preset), --suites, --tags, --arms.
list defaults to --preset full. oracle writes results.json and summary.md to --out
(default .cache/oracle/<timestamp>).
Exit codes: 0 ok, 1 a failed oracle or an error, 2 bad usage.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	args := os.Args[2:]
	switch os.Args[1] {
	case "list":
		err = cmdList(args)
	case "oracle":
		err = cmdOracle(ctx, args)
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	default:
		err = &exitError{2, fmt.Sprintf("unknown command %q (twevals help lists them)", os.Args[1])}
	}
	if err != nil {
		var ex *exitError
		if errors.As(err, &ex) {
			if ex.msg != "" {
				fmt.Fprintln(os.Stderr, "twevals:", ex.msg)
			}
			os.Exit(ex.code)
		}
		fmt.Fprintln(os.Stderr, "twevals:", err)
		os.Exit(1)
	}
}

type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return e.msg }
