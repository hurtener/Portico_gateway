package api

import "net/http"

// gatewayInfoResponse is the public read-only DTO returned from
// GET /api/gateway/info. It carries the facts the Console + external
// operators need to point an MCP client at the gateway: the bind
// address, the northbound transport path, and the auth requirement.
//
// Everything here is observable from any client that can reach the
// listener (you can `nc` the port and watch a TLS hello to confirm
// the bind), so this endpoint is intentionally unauthenticated. It
// does NOT expose secrets — the JWKS URL is a public discovery
// endpoint and the issuer / audiences are public by design.
type gatewayInfoResponse struct {
	Bind        string         `json:"bind"`
	MCPPath     string         `json:"mcp_path"`
	Version     string         `json:"version"`
	BuildCommit string         `json:"build_commit"`
	DevMode     bool           `json:"dev_mode"`
	DevTenant   string         `json:"dev_tenant,omitempty"`
	Auth        gatewayAuthDTO `json:"auth"`
}

type gatewayAuthDTO struct {
	// Mode is "dev" when no JWT validator is configured (localhost
	// only per Phase 0); "jwt" otherwise.
	Mode        string   `json:"mode"`
	Issuer      string   `json:"issuer,omitempty"`
	Audiences   []string `json:"audiences,omitempty"`
	JWKSURL     string   `json:"jwks_url,omitempty"`
	TenantClaim string   `json:"tenant_claim,omitempty"`
	ScopeClaim  string   `json:"scope_claim,omitempty"`
}

// gatewayInfoHandler returns the connection facts. Mounted on the
// public router (no auth wrapper) — see GatewayInfo's docstring for
// why that's safe.
func gatewayInfoHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		mode := "jwt"
		if d.DevMode {
			mode = "dev"
		}
		writeJSON(w, http.StatusOK, gatewayInfoResponse{
			Bind:        d.Gateway.Bind,
			MCPPath:     d.Gateway.MCPPath,
			Version:     d.Version,
			BuildCommit: d.BuildCommit,
			DevMode:     d.DevMode,
			DevTenant:   d.DevTenant,
			Auth: gatewayAuthDTO{
				Mode:        mode,
				Issuer:      d.Gateway.JWTIssuer,
				Audiences:   d.Gateway.JWTAudiences,
				JWKSURL:     d.Gateway.JWTJWKSURL,
				TenantClaim: d.Gateway.JWTTenantClaim,
				ScopeClaim:  d.Gateway.JWTScopeClaim,
			},
		})
	}
}
