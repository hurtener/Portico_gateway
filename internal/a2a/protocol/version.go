// Package protocol defines the on-the-wire A2A (Agent-to-Agent) types
// Portico produces and consumes. Hand-rolled (no SDK dependency) so the
// project owns the wire format. Single source of truth — no other package
// defines A2A messages (AGENTS.md §13).
package protocol

// SpecVersion is the A2A protocol revision Portico targets.
// Bumping the version is an RFC change — see AGENTS.md §8 / phase-16 plan §6.2.
//
// History:
//   - 0.2.5 (Phase 16, unit P16-B) — initial A2A wire types land; discovery
//     half (AgentCard, AgentSkill, AgentProvider, AgentCapabilities) +
//     JSON-RPC envelope, method-name constants, and error codes. Task/Message/
//     Part/Artifact land in a separate later unit.
const SpecVersion = "0.2.5"
