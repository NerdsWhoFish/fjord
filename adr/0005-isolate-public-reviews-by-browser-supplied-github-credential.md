# 5. Isolate public reviews by browser supplied GitHub credential

Date: 2026-11-06

## Status

Accepted. Amended, see [Amendments](#amendments).

## Context and Problem Statement

Fjord previously trusted one operator's GitHub credential and shared review sessions within a process.
Public hosting must keep visitors' private repositories and language-server state separate while preserving the local deployment.

## Considered Options

1. Separate bounded tenants keyed by a credential hash, validating GitHub access before cached reads
2. Attach browser tokens to the existing shared session and workspace stores
3. Create one Kubernetes workload per visitor

## Decision Outcome

Chosen: **option 1**.

Add an explicit public mode that receives bearer tokens on same-origin API requests and never falls back to a server credential. The browser persists its token in local storage as requested and offers a forget action. Isolate navigation managers by credential, validate token identity on every API request, and reauthorize repository access before cached reads. Limit active tenants and workspaces and clean up idle tenants. Run the public instance separately with bundled offline language servers and without private shared caches. Keep public MCP disabled until it has a dedicated authenticated client flow.

## Consequences

### Good

- Private code and language-server state are not shared across visitors.
- Token revocation and repository permission changes are checked before returning cached private data.
- Existing private installations keep their current behavior.

### Bad

- GitHub authorization checks add latency and API usage.
- Local storage is readable by same-origin JavaScript, so an XSS bug could expose a saved token.
- Bounded capacity can reject new visitors while existing tenants are active; in-memory state is lost on restart.
- Public MCP is unavailable initially.

### Rejected because

- Adding tokens to shared stores leaves ownership and cached source boundaries unsafe.
- Per-visitor Kubernetes workloads add lifecycle and infrastructure complexity beyond the bounded initial service.

## Amendments

### 2026-11-06: Include authenticated MCP in the public rollout

The requested initial rollout now includes authenticated MCP. A native MCP client supplies the same GitHub bearer token as its browser session. The credential boundary applies to session discovery, cached PR snapshots, draft actions, and code navigation. Current GitHub permission checks precede cached reads. Native clients may omit Origin; supplied origins must match. This fulfills the authenticated-client prerequisite in the original decision, so public MCP is available in the initial release. Browser polling is less frequent in public mode to bound authorization API usage; browser display of MCP actions can consequently take longer. No MCP tool submits a GitHub review.
