package policy

import "github.com/hurtener/Portico_gateway/internal/registry"

// getServerRisk returns the per-server default risk class declared in
// spec.Auth.DefaultRiskClass. Empty string when no Auth block is set.
func getServerRisk(spec registry.ServerSpec) string {
	if spec.Auth == nil {
		return ""
	}
	return spec.Auth.DefaultRiskClass
}
