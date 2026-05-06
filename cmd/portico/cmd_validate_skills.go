package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"github.com/hurtener/Portico_gateway/internal/skills/loader"
	"github.com/hurtener/Portico_gateway/internal/skills/source"
)

// runValidateSkills walks each path argument: if it's a directory it's
// treated as a Skill Pack root (or a tree of them); if it's a file it's
// treated as a manifest.yaml directly.
//
// Output: one line per pack with status (OK / WARNING / ERROR).
// Exits 1 when any pack has errors. Optional tools missing only warn.
func runValidateSkills(args []string) error {
	fs := flag.NewFlagSet("validate-skills", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	paths := fs.Args()
	if len(paths) == 0 {
		return errors.New("validate-skills: at least one path is required")
	}

	logger := slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	// Build a no-source loader so we can run schema + semantic checks
	// against arbitrary bytes. Per-pack file probing is done explicitly
	// below using a LocalDir over each pack's parent directory.
	bareLoader, err := loader.New(nil, nil, logger)
	if err != nil {
		return err
	}

	hadErrors := false
	results := make([]validateResult, 0)
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			results = append(results, validateResult{Path: p, Errors: []error{err}})
			hadErrors = true
			continue
		}
		info, err := os.Stat(abs)
		if err != nil {
			results = append(results, validateResult{Path: p, Errors: []error{err}})
			hadErrors = true
			continue
		}
		if !info.IsDir() {
			body, err := os.ReadFile(abs)
			if err != nil {
				results = append(results, validateResult{Path: p, Errors: []error{err}})
				hadErrors = true
				continue
			}
			r := bareLoader.LoadFromBytes(body)
			results = append(results, validateResult{
				Path:     p,
				ID:       idFromManifest(r),
				Version:  versionFromManifest(r),
				Errors:   r.Errors,
				Warnings: r.Warnings,
			})
			if len(r.Errors) > 0 {
				hadErrors = true
			}
			continue
		}

		// Directory: discover packs underneath.
		packs := walkPacks(abs)
		if len(packs) == 0 {
			// Try treating abs as a single pack root.
			if _, err := os.Stat(filepath.Join(abs, "manifest.yaml")); err == nil {
				packs = []string{abs}
			}
		}
		for _, pack := range packs {
			r := validateOnePack(pack, logger)
			results = append(results, r)
			if len(r.Errors) > 0 {
				hadErrors = true
			}
		}
	}

	sort.Slice(results, func(i, j int) bool { return results[i].Path < results[j].Path })
	for _, r := range results {
		printResult(r)
	}
	if hadErrors {
		return errors.New("validate-skills: at least one pack has errors")
	}
	return nil
}

type validateResult struct {
	Path     string
	ID       string
	Version  string
	Errors   []error
	Warnings []string
}

func validateOnePack(packDir string, logger *slog.Logger) validateResult {
	parent := filepath.Dir(filepath.Dir(packDir))
	src, err := source.NewLocalDir(parent, logger)
	if err != nil {
		return validateResult{Path: packDir, Errors: []error{err}}
	}
	refs, err := src.List(context.Background())
	if err != nil {
		return validateResult{Path: packDir, Errors: []error{err}}
	}
	var match source.Ref
	for _, r := range refs {
		if r.Loc == packDir {
			match = r
			break
		}
	}
	if match.Loc == "" {
		// Fall back to a manifest read at packDir.
		body, err := os.ReadFile(filepath.Join(packDir, "manifest.yaml"))
		if err != nil {
			return validateResult{Path: packDir, Errors: []error{err}}
		}
		bare, _ := loader.New(nil, nil, logger)
		r := bare.LoadFromBytes(body)
		return validateResult{
			Path:     packDir,
			ID:       idFromManifest(r),
			Version:  versionFromManifest(r),
			Errors:   r.Errors,
			Warnings: r.Warnings,
		}
	}
	l, _ := loader.New([]source.Source{src}, nil, logger)
	r := l.LoadOne(context.Background(), src, match)
	return validateResult{
		Path:     packDir,
		ID:       idFromManifest(r),
		Version:  versionFromManifest(r),
		Errors:   r.Errors,
		Warnings: r.Warnings,
	}
}

func walkPacks(root string) []string {
	var out []string
	entries, err := os.ReadDir(root)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		ns := filepath.Join(root, e.Name())
		nsEntries, err := os.ReadDir(ns)
		if err != nil {
			continue
		}
		for _, ne := range nsEntries {
			if !ne.IsDir() {
				continue
			}
			pack := filepath.Join(ns, ne.Name())
			if _, err := os.Stat(filepath.Join(pack, "manifest.yaml")); err == nil {
				out = append(out, pack)
			}
		}
	}
	return out
}

func idFromManifest(r loader.LoadResult) string {
	if r.Manifest != nil {
		return r.Manifest.ID
	}
	return ""
}

func versionFromManifest(r loader.LoadResult) string {
	if r.Manifest != nil {
		return r.Manifest.Version
	}
	return ""
}

func printResult(r validateResult) {
	tag := "OK     "
	if len(r.Errors) > 0 {
		tag = "ERROR  "
	} else if len(r.Warnings) > 0 {
		tag = "WARN   "
	}
	id := r.ID
	if id == "" {
		id = filepath.Base(r.Path)
	}
	version := r.Version
	if version == "" {
		version = "?"
	}
	fmt.Fprintf(os.Stdout, "%s %s %s  (%s)\n", tag, id, version, r.Path)
	for _, e := range r.Errors {
		fmt.Fprintf(os.Stdout, "        ERROR: %v\n", e)
	}
	for _, w := range r.Warnings {
		fmt.Fprintf(os.Stdout, "        WARN:  %s\n", w)
	}
}
