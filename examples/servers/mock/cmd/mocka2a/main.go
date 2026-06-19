// Command mocka2a is the standalone mock A2A peer used by Portico's
// integration tests + future preflight smoke. It speaks A2A over HTTP.
//
// Usage:
//
//	mocka2a [--addr HOST:PORT] [--name NAME]
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hurtener/Portico_gateway/examples/servers/mock/a2amock"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8090", "listen address")
	name := flag.String("name", "mocka2a", "agent name to advertise in the agent card")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	handler := a2amock.Handler(a2amock.Options{Name: *name})
	srv := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Print the actual listen address to stderr before serving so callers
	// (tests, scripts) can use it. When --addr has port 0, the resolved
	// port is reported back via srv.Addr after Listen; we listen manually
	// so we don't pin the port when callers want ephemeral.
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mocka2a: listen %s: %v\n", *addr, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "mocka2a: listening on %s\n", ln.Addr())

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "mocka2a: serve: %v\n", err)
		os.Exit(1)
	}
}
