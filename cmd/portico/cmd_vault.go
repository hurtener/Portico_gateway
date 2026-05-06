package main

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/hurtener/Portico_gateway/internal/secrets"
)

const vaultUsage = `Usage: portico vault <subcommand> [flags]

Subcommands:
  put         --tenant <id> --name <key> [--value <v> | --from-file <path> | --from-stdin]
  get         --tenant <id> --name <key>
  delete      --tenant <id> --name <key>
  list        --tenant <id>
  rotate-key  --new-key <base64-32B>

The PORTICO_VAULT_KEY env var must be set to a base64-encoded 32-byte key.
The vault file path defaults to ./vault.yaml; override with --path.`

func runVault(ctx context.Context, args []string) error {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, vaultUsage)
		return errors.New("vault: missing subcommand")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "put":
		return runVaultPut(ctx, rest)
	case "get":
		return runVaultGet(ctx, rest)
	case "delete":
		return runVaultDelete(ctx, rest)
	case "list":
		return runVaultList(ctx, rest)
	case "rotate-key":
		return runVaultRotate(ctx, rest)
	case "-h", "--help", "help":
		fmt.Fprintln(os.Stdout, vaultUsage)
		return nil
	default:
		return fmt.Errorf("vault: unknown subcommand %q", sub)
	}
}

func openVaultFromFlags(path string) (*secrets.FileVault, error) {
	key, err := secrets.LoadKeyFromEnv()
	if err != nil {
		return nil, err
	}
	if key == nil {
		return nil, errors.New("PORTICO_VAULT_KEY env var must be set (base64-encoded 32 bytes)")
	}
	return secrets.NewFileVault(path, key)
}

func runVaultPut(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("vault put", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant id")
	name := fs.String("name", "", "secret name")
	value := fs.String("value", "", "secret value (literal)")
	fromFile := fs.String("from-file", "", "read value from file path")
	fromStdin := fs.Bool("from-stdin", false, "read value from stdin")
	path := fs.String("path", "./vault.yaml", "vault file path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tenant == "" || *name == "" {
		return errors.New("vault put: --tenant and --name required")
	}
	v, err := readSecretValue(*value, *fromFile, *fromStdin)
	if err != nil {
		return err
	}
	vault, err := openVaultFromFlags(*path)
	if err != nil {
		return err
	}
	defer vault.Close()
	return vault.Put(ctx, *tenant, *name, v)
}

func runVaultGet(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("vault get", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant id")
	name := fs.String("name", "", "secret name")
	path := fs.String("path", "./vault.yaml", "vault file path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tenant == "" || *name == "" {
		return errors.New("vault get: --tenant and --name required")
	}
	vault, err := openVaultFromFlags(*path)
	if err != nil {
		return err
	}
	defer vault.Close()
	val, err := vault.Get(ctx, *tenant, *name)
	if err != nil {
		return err
	}
	// Plain print to stdout. Stderr warns the caller — `vault get` prints
	// the secret in cleartext, useful for ops but easy to footgun in CI.
	fmt.Fprintln(os.Stderr, "warning: printing secret value to stdout in cleartext")
	_, _ = io.WriteString(os.Stdout, val)
	if !endsWithNewline(val) {
		_, _ = io.WriteString(os.Stdout, "\n")
	}
	return nil
}

func runVaultDelete(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("vault delete", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant id")
	name := fs.String("name", "", "secret name")
	path := fs.String("path", "./vault.yaml", "vault file path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tenant == "" || *name == "" {
		return errors.New("vault delete: --tenant and --name required")
	}
	vault, err := openVaultFromFlags(*path)
	if err != nil {
		return err
	}
	defer vault.Close()
	return vault.Delete(ctx, *tenant, *name)
}

func runVaultList(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("vault list", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant id")
	path := fs.String("path", "./vault.yaml", "vault file path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tenant == "" {
		return errors.New("vault list: --tenant required")
	}
	vault, err := openVaultFromFlags(*path)
	if err != nil {
		return err
	}
	defer vault.Close()
	names, err := vault.List(ctx, *tenant)
	if err != nil {
		return err
	}
	for _, n := range names {
		fmt.Println(n)
	}
	return nil
}

func runVaultRotate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("vault rotate-key", flag.ContinueOnError)
	newKey := fs.String("new-key", "", "new master key (base64-encoded 32 bytes)")
	path := fs.String("path", "./vault.yaml", "vault file path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *newKey == "" {
		return errors.New("vault rotate-key: --new-key required")
	}
	decoded, err := base64.StdEncoding.DecodeString(*newKey)
	if err != nil {
		return fmt.Errorf("vault rotate-key: --new-key must be base64: %w", err)
	}
	if len(decoded) != 32 {
		return fmt.Errorf("vault rotate-key: --new-key must decode to 32 bytes (got %d)", len(decoded))
	}
	vault, err := openVaultFromFlags(*path)
	if err != nil {
		return err
	}
	defer vault.Close()
	if err := vault.RotateKey(ctx, decoded); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "vault: key rotated. update PORTICO_VAULT_KEY before next start.")
	return nil
}

func readSecretValue(literal, fromFile string, fromStdin bool) (string, error) {
	count := 0
	if literal != "" {
		count++
	}
	if fromFile != "" {
		count++
	}
	if fromStdin {
		count++
	}
	if count != 1 {
		return "", errors.New("provide exactly one of --value, --from-file, or --from-stdin")
	}
	if literal != "" {
		return literal, nil
	}
	if fromFile != "" {
		b, err := os.ReadFile(fromFile)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func endsWithNewline(s string) bool { return len(s) > 0 && s[len(s)-1] == '\n' }
