// Package namespace handles the qualified-name conventions Portico applies
// when aggregating downstream MCP servers. Tools are prefixed with the server
// id so multiple servers exposing same-named tools coexist without collision.
//
// Tool name convention:
//
//	{server_id}.{tool_name}
//
// Tool names that already contain dots are preserved on the wire — split
// returns (server, "rest.with.dots") given the *first* '.'.
package namespace

import (
	"errors"
	"regexp"
	"strings"
)

// serverIDRegexp matches the allowed server id format used in namespaced names.
var serverIDRegexp = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// ValidateServerID rejects ids that would produce ambiguous or unsafe joins.
func ValidateServerID(id string) error {
	if !serverIDRegexp.MatchString(id) {
		return errors.New("namespace: server id must match ^[a-z0-9][a-z0-9_-]{0,31}$")
	}
	return nil
}

// JoinTool produces the on-the-wire tool name. Caller must have already
// validated serverID (Phase 0/1: validation runs at config time).
func JoinTool(serverID, toolName string) string {
	return serverID + "." + toolName
}

// SplitTool inverts JoinTool. ok=false means the input lacks a "." separator
// and cannot be routed.
func SplitTool(qualified string) (serverID, toolName string, ok bool) {
	i := strings.IndexByte(qualified, '.')
	if i <= 0 || i == len(qualified)-1 {
		return "", "", false
	}
	return qualified[:i], qualified[i+1:], true
}
