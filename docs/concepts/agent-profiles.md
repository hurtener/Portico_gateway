# Agent Profiles

> Status: building (Phase 14). Tenant-scoped. **Back-compatible** — a principal with no
> profile bound sees the tenant's full surface, so every pre-Phase-14 client is unaffected.

An **Agent Profile** is the named, tenant-scoped binding that answers one question:
*"what is this agent allowed to do in our environment?"* Operators think in agents — "Agent A
talks to github/jira/slack, uses the `code-review` Skill, calls `gpt-4o`" — not in listeners
and routes. A profile makes that sentence a single first-class object instead of a composition
smeared across four surfaces (Phase 5 scopes + Phase 6 snapshot scoping + Phase 4 Skill
enablement + Phase 15.5 Virtual-Key allowlists).

This is the primitive Portico has that neither `agentgateway` (which models traffic on the
wire, with no consumer abstraction) nor `bifrost` (which has VK allowlists but no unified
profile object) offers.

## What a profile contains

| Field | Meaning |
|---|---|
| `allowed_mcp_servers` | subset of the tenant's registered MCP servers the agent may reach |
| `allowed_tools` | optional finer-grain allowlist of namespaced `server.tool` ids; empty = all tools in the allowed servers |
| `allowed_skills` | subset of Skill Packs the agent may enable |
| `allowed_model_aliases` | subset of LLM aliases the agent may call |
| `scopes` | the scope set this profile grants when used as the effective scope set |
| `policy_bundle_ref` | optional reference to the policy rules that apply |
| attached Virtual Keys | N VKs for environment/lifecycle separation (dev/staging/prod), added in Phase 15.5 |

A profile is the **source of truth for consumer entitlement.** Any code path that gates "which
servers/tools/skills/models can this caller see" routes through the profile resolver; no
parallel allowlist lives on any other surface.

## How it is enforced

A single middleware step resolves the profile (from the JWT subject's binding, or a VK's
profile id) and writes it into the request context, *after* tenant + auth and *before* policy.
Every downstream just reads it:

- **MCP `tools/list`** returns tools only from `allowed_mcp_servers` (∩ `allowed_tools`); other
  servers' tools are **absent** from the JSON, not merely hidden.
- **MCP `tools/call`** for a tool outside the surface returns a typed `agent_profile_violation`
  error (with `profile_id`, `tool`, `reason`) and records an audit event.
- **LLM `/v1/*`** rejects a `model` alias not in `allowed_model_aliases`; `/v1/models` lists
  only allowed aliases.
- **Skills runtime** enables only the packs in `allowed_skills`.
- **Catalog snapshots** project the per-session catalog through the profile, so drift detection
  runs against what the agent can actually see.

## The default profile (back-compat)

A principal with no profile bound resolves to a **synthesised default profile** — the tenant's
*full* surface (every registered server, Skill, and alias). This is the seam that keeps every
pre-Phase-14 client working unchanged: operators opt **into** restriction by creating a profile
and binding a principal to it; they never have to opt out of a restriction they didn't ask for.

## Intersection: most-restrictive wins

When more than one allowlist layer applies, the effective surface is the **intersection** — a
profile may be further narrowed by a VK allowlist or by Phase 5 scopes, but never widened:

```
effective_servers = profile.allowed_mcp_servers
                  ∩ (vk.allowed_mcp_servers, if the VK carries its own)
                  ∩ (scope-implied surface from policy)

effective_tools   = (profile.allowed_tools if non-empty, else all tools in effective_servers)
                  ∩ (vk.allowed_tools, if any)
                  ∩ (policy-allowed tools)
```

The profile is the headline; VK / scope / snapshot may restrict but never relax. This is the
cross-cutting V2 rule.

## Why it is built first

Virtual Keys attach to profiles (Phase 15.5); A2A consumer-side gating reads the profile
(Phase 16); tool-poisoning policies attach to the profile (Phase 17); GitOps reconciles
profiles as resources and CRDs map to them (Phase 18/19). Building the profile primitive first
means every later phase has the right object to attach to — one resolver, one REST surface
(`/api/agent-profiles`), one CLI (`portico agents`), one Console route (`/agents`), one place
in the middleware chain.
