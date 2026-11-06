# 5. Isolate public reviews by browser supplied GitHub credential

Date: 2026-11-06

## Status

Accepted.

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
