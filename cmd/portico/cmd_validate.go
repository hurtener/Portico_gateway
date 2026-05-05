package main

import (
	"errors"
	"flag"
	"fmt"

	"github.com/hurtener/Portico_gateway/internal/config"
)

func runValidate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ExitOnError)
	configPath := fs.String("config", "", "path to portico.yaml (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *configPath == "" {
		return errors.New("validate: --config is required")
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	fmt.Printf("config OK: bind=%s tenants=%d storage=%s dev_mode=%v\n",
		cfg.Server.Bind, len(cfg.Tenants), cfg.Storage.Driver, cfg.IsDevMode())
	return nil
}
