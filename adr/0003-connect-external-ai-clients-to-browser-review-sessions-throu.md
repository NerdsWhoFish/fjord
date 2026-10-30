# 3. Connect external AI clients to browser review sessions through MCP

Date: 2026-10-30

## Status

Accepted.

## Context and Problem Statement

The reviewer wants to use their own AI client rather than an embedded model-provider chat.
An AI client needs to read review comments, inspect code, show navigation results in the browser, and propose comments or suggestions without submitting a review.
Browser drafts currently survive reloads in sessionStorage and must not be overwritten by asynchronous AI actions.

## Considered Options

1. Streamable HTTP MCP backed by bounded browser-session snapshots and acknowledged action queues
2. Move all review state into a persistent database and make the browser a thin client
3. Keep provider-specific AI chat embedded in Fjord

## Decision Outcome

Chosen: **option 1**.

Use the official Go MCP SDK on /mcp. Reuse the existing GitHub and language-server services and pin every session to the loaded head and merge-base revisions. Browser-owned drafts are mirrored to an expiring in-memory session; MCP mutations enqueue idempotent draft or navigation actions, and the browser acknowledges them after applying them. Tools distinguish queued actions from applied actions. MCP offers no GitHub submission or arbitrary command execution. The trusted-network boundary remains explicit, with exact configured Host and Origin validation and no reliance on forwarded headers. This replaces the embedded-provider AI direction in the initial architecture.

## Consequences

### Good

- Any compatible AI client can inspect the same review context and use real language servers.
- AI drafts merge without replacing concurrent reviewer edits and require explicit browser submission.
- Existing immutable workspaces, review validation, and telemetry are reused.

### Bad

- Pending actions and session discovery are local to one server process and are lost on restart; acknowledged browser drafts remain in sessionStorage.
- A browser tab must be connected to apply navigation or draft actions; tool responses must expose delivery state.
- The unauthenticated service is for a trusted network and is not a multi-user tenant boundary.

### Rejected because

- A database migration would add storage operations and conflict resolution beyond the requested trusted-network review service; browser persistence already protects acknowledged drafts.
- Embedded provider chat prevents users from bringing their preferred MCP-capable client and duplicates their AI interface.
