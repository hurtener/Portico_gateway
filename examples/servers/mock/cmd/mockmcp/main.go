// Command mockmcp is the standalone mock MCP server used by Portico's
// integration tests + the preflight smoke. It speaks MCP over stdio.
//
// Usage:
//
//	mockmcp [--name NAME] [--version VERSION]
package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/hurtener/Portico_gateway/examples/servers/mock"
)

func main() {
	name := flag.String("name", "mockmcp", "server name to advertise in initialize")
	version := flag.String("version", "0.1.0", "server version to advertise")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	srv := &mock.Server{Name: *name, Version: *version}
	if err := srv.Run(ctx, os.Stdin, os.Stdout); err != nil && err != context.Canceled {
		// Errors go to stderr; stdout is the JSON-RPC channel.
		os.Stderr.WriteString("mockmcp: " + err.Error() + "\n")
		os.Exit(1)
	}
}
