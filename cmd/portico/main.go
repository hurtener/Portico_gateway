// Command portico is the Portico Gateway / Skill Runtime binary.
//
// Subcommands:
//   serve     — run with a config file (production)
//   dev       — run in dev mode (localhost, synthetic dev tenant, no JWT)
//   validate  — validate a config file and exit
//   version   — print version info
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

// version is set via -ldflags by the release build. CI/local builds report
// "dev" by default.
var (
	version     = "dev"
	buildCommit = ""
)

func main() {
	if len(os.Args) < 2 {
		printUsage(os.Stderr)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "serve":
		err = runServe(ctx, args)
	case "dev":
		err = runDev(ctx, args)
	case "validate":
		err = runValidate(args)
	case "version", "--version", "-v":
		printVersion()
	case "help", "--help", "-h":
		printUsage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "portico: unknown command %q\n\n", cmd)
		printUsage(os.Stderr)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "portico: %v\n", err)
		os.Exit(1)
	}
}

func printUsage(w *os.File) {
	fmt.Fprintln(w, `Usage: portico <command> [flags]

Commands:
  serve     --config <path>           Run with a YAML config (production).
  dev       [--bind <addr>] [--data-dir <path>]
                                       Run in dev mode (localhost only).
  validate  --config <path>           Validate a config file and exit.
  version                              Print version info.

Run 'portico <command> -h' for command-specific flags.`)
}

func printVersion() {
	fmt.Printf("portico %s", version)
	if buildCommit != "" {
		fmt.Printf(" (%s)", buildCommit)
	}
	fmt.Println()
}
