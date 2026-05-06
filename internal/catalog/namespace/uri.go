package namespace

import (
	"encoding/base64"
	"net/url"
	"strings"
)

// Resource URI namespacing rules (Phase 3):
//
//   - "ui://..."           -> "ui://{server}/{rest}"
//   - "file://..."         -> "mcp+server://{server}/file/{path}"
//   - "https://host/path"  -> "mcp+server://{server}/https/{authority}/{path}"
//   - "http://host/path"   -> "mcp+server://{server}/http/{authority}/{path}"
//   - any other scheme     -> "mcp+server://{server}/raw/{base64url(originalURI)}"
//
// Rewrites are idempotent: invoking RewriteResourceURI on an already-
// namespaced URI returns it unchanged (no double-prefixing).
//
// All callers must preserve _meta.upstreamURI on resources for transparency
// and to satisfy the protocol's expectation that URIs round-trip.

const (
	// ResourceScheme is the Portico-internal scheme for non-ui resources.
	// Clients pass URIs back unchanged; the gateway parses them.
	ResourceScheme = "mcp+server"

	// UIScheme is the canonical scheme for MCP App resources.
	UIScheme = "ui"
)

// PromptNameSeparator is the character that splits server-id from prompt
// name in the on-the-wire prompt name. Tools and prompts use the same
// convention (server.name) so a single import covers both.
const PromptNameSeparator = "."

// RewriteResourceURI maps a downstream URI into the Portico namespace.
// sandbox=true means the URI is in the ui:// scheme — the caller may use
// this to feed the apps registry / apply CSP wrapping.
//
// If serverID is empty or original is empty, the original is returned
// unchanged (defensive: callers should validate, but a no-op preserves
// correctness when an aggregator races a registry change).
func RewriteResourceURI(serverID, original string) (rewritten string, sandbox bool) {
	if serverID == "" || original == "" {
		return original, false
	}

	// Idempotency: a URI that's already namespaced under our schemes is
	// returned unchanged. We detect this by parsing and matching the host
	// against the supplied serverID. Mismatched hosts are NOT rewritten —
	// returning the original signals an error to the caller.
	if u, err := url.Parse(original); err == nil {
		switch u.Scheme {
		case ResourceScheme:
			return original, false
		case UIScheme:
			if u.Host != "" && u.Host == serverID {
				return original, true
			}
			// ui://something else → treat as a downstream URI to rewrite.
		}
	}

	if strings.HasPrefix(original, "ui://") {
		rest := strings.TrimPrefix(original, "ui://")
		// Trim leading slashes so the server-id sits cleanly.
		rest = strings.TrimLeft(rest, "/")
		return "ui://" + serverID + "/" + rest, true
	}
	if strings.HasPrefix(original, "file://") {
		rest := strings.TrimPrefix(original, "file://")
		rest = strings.TrimLeft(rest, "/")
		return ResourceScheme + "://" + serverID + "/file/" + rest, false
	}
	if strings.HasPrefix(original, "https://") {
		rest := strings.TrimPrefix(original, "https://")
		return ResourceScheme + "://" + serverID + "/https/" + rest, false
	}
	if strings.HasPrefix(original, "http://") {
		rest := strings.TrimPrefix(original, "http://")
		return ResourceScheme + "://" + serverID + "/http/" + rest, false
	}
	// Unknown scheme: opaque base64url envelope so any URL-unsafe
	// characters survive the round-trip.
	enc := base64.RawURLEncoding.EncodeToString([]byte(original))
	return ResourceScheme + "://" + serverID + "/raw/" + enc, false
}

// RestoreResourceURI parses a namespaced URI back to its (server, original)
// pair. isUI=true means the URI is a ui:// app resource; the original
// scheme is "ui://" and the original URI omits the server segment.
//
// ok=false means the URI does not look namespaced — the caller should
// reject it (404 in `resources/read` paths).
func RestoreResourceURI(rewritten string) (serverID, original string, isUI, ok bool) {
	if rewritten == "" {
		return "", "", false, false
	}
	switch {
	case strings.HasPrefix(rewritten, "ui://"):
		rest := strings.TrimPrefix(rewritten, "ui://")
		i := strings.IndexByte(rest, '/')
		if i <= 0 {
			return "", "", false, false
		}
		serverID = rest[:i]
		original = "ui://" + rest[i+1:]
		return serverID, original, true, true
	case strings.HasPrefix(rewritten, ResourceScheme+"://"):
		// mcp+server://{server}/{kind}/{...}
		rest := strings.TrimPrefix(rewritten, ResourceScheme+"://")
		// First segment: server id.
		i := strings.IndexByte(rest, '/')
		if i <= 0 {
			return "", "", false, false
		}
		serverID = rest[:i]
		rest = rest[i+1:]
		// Second segment: kind.
		j := strings.IndexByte(rest, '/')
		if j <= 0 {
			return "", "", false, false
		}
		kind := rest[:j]
		body := rest[j+1:]
		switch kind {
		case "file":
			original = "file:///" + body
		case "https":
			original = "https://" + body
		case "http":
			original = "http://" + body
		case "raw":
			dec, err := base64.RawURLEncoding.DecodeString(body)
			if err != nil {
				return "", "", false, false
			}
			original = string(dec)
		default:
			return "", "", false, false
		}
		return serverID, original, false, true
	default:
		return "", "", false, false
	}
}

// RewritePromptName joins a server id and a downstream prompt name into
// the namespaced on-the-wire form. Server ids are validated by
// ValidateServerID at config time so dot-injection is impossible.
func RewritePromptName(serverID, original string) string {
	return serverID + PromptNameSeparator + original
}

// RestorePromptName splits a namespaced prompt name back into its
// components. Mirrors SplitTool: indexes on the *first* '.' so prompt
// names with embedded dots are preserved.
func RestorePromptName(qualified string) (serverID, original string, ok bool) {
	i := strings.IndexByte(qualified, '.')
	if i <= 0 || i == len(qualified)-1 {
		return "", "", false
	}
	return qualified[:i], qualified[i+1:], true
}
