package loader

import (
	"fmt"

	"github.com/hurtener/Portico_gateway/internal/registry"
	"github.com/hurtener/Portico_gateway/internal/skills/manifest"
)

// ValidateSemantic runs the post-schema checks against a manifest.
//
//   - probeFiles, when true, calls probe(rel) for every instructions /
//     resources / prompts entry to confirm the file is readable.
//   - probe is the file-existence callback the loader supplies; nil-safe.
//
// Tool / server dependency checks are intentionally NOT done here —
// the registry view is per-tenant and the validator runs at load time
// (no tenant context). The runtime's index generator surfaces missing
// tools accurately per-tenant via Loader.AnnotateMissingTools.
//
// The function never panics and never short-circuits on the first
// error; callers get every problem in one pass.
func ValidateSemantic(m *manifest.Manifest, _ *registry.Registry, probeFiles bool, probe func(rel string) error) ([]error, []string) {
	var errs []error
	var warns []string

	if m == nil {
		return []error{fmt.Errorf("nil manifest")}, nil
	}

	if probeFiles {
		if probe == nil {
			warns = append(warns, "probeFiles requested but probe is nil; skipping file existence checks")
		} else {
			if m.Instructions != "" {
				if err := probe(m.Instructions); err != nil {
					errs = append(errs, fmt.Errorf("instructions %q: %w", m.Instructions, err))
				}
			}
			for _, p := range m.Resources {
				if err := probe(p); err != nil {
					errs = append(errs, fmt.Errorf("resources[%q]: %w", p, err))
				}
			}
			for _, p := range m.Prompts {
				if err := probe(p); err != nil {
					errs = append(errs, fmt.Errorf("prompts[%q]: %w", p, err))
				}
			}
		}
	}

	// Approval-flagged tools must be declared as required or optional.
	// Phase 5 turns this list into the live approval gate; Phase 4 just
	// checks consistency.
	declared := make(map[string]bool)
	for _, t := range m.Binding.RequiredTools {
		declared[t] = true
	}
	for _, t := range m.Binding.OptionalTools {
		declared[t] = true
	}
	for _, t := range m.Binding.Policy.RequiresApproval {
		if !declared[t] {
			errs = append(errs, fmt.Errorf("binding.policy.requires_approval[%q] is not in required_tools or optional_tools", t))
		}
	}
	// risk_classes references declared tools (warning only — operators
	// may stage policy ahead of tool registration).
	for t := range m.Binding.Policy.RiskClasses {
		if !declared[t] {
			warns = append(warns, fmt.Sprintf("binding.policy.risk_classes[%q] is not in required_tools or optional_tools", t))
		}
	}

	return errs, warns
}
