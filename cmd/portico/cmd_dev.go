package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hurtener/Portico_gateway/internal/config"
)

func runDev(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("dev", flag.ExitOnError)
	bind := fs.String("bind", "127.0.0.1:8080", "host:port to bind (must be localhost in dev mode)")
	dataDir := fs.String("data-dir", "", "directory for SQLite + logs (default: cwd)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if !isLocalhostBind(*bind) {
		return fmt.Errorf("dev: bind must be 127.0.0.1, ::1, or localhost; got %q", *bind)
	}

	dsn := "file:./portico.db?cache=shared"
	if *dataDir != "" {
		dsn = "file:" + filepath.Join(*dataDir, "portico.db") + "?cache=shared"
	}

	cfg := &config.Config{
		Server: config.ServerConfig{
			Bind: *bind,
		},
		// Auth nil => dev mode (the bind check above guarantees safety).
		Storage: config.StorageConfig{
			Driver: "sqlite",
			DSN:    dsn,
		},
		Logging: config.LoggingConfig{
			Level:  "info",
			Format: "text",
		},
		Skills: config.SkillsConfig{
			Sources: devSkillSources(),
		},
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	return runWithConfig(ctx, cfg, "")
}

func isLocalhostBind(bind string) bool {
	bind = strings.ToLower(bind)
	host, _ := splitHostPort(bind)
	switch host {
	case "127.0.0.1", "::1", "localhost", "[::1]":
		return true
	default:
		return false
	}
}

// devSkillSources returns the local-dir sources to load in dev mode.
// Searches `./skills` first, then `./examples/skills` so a fresh
// checkout has skills loaded out of the box. When neither directory
// exists the returned slice is empty and the runtime stays disabled.
func devSkillSources() []config.SkillSourceConfig {
	out := make([]config.SkillSourceConfig, 0, 2)
	for _, p := range []string{"./skills", "./examples/skills"} {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			out = append(out, config.SkillSourceConfig{Type: "local", Path: p})
		}
	}
	return out
}

// splitHostPort tolerates IPv6 bracketed form and bare host.
func splitHostPort(s string) (host, port string) {
	if strings.HasPrefix(s, "[") {
		if end := strings.Index(s, "]"); end > 0 {
			host = s[1:end]
			if end+1 < len(s) && s[end+1] == ':' {
				port = s[end+2:]
			}
			return host, port
		}
	}
	if i := strings.LastIndex(s, ":"); i > 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}
