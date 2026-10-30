# 2. Load operator-defined language servers through isolated stdio profiles

Date: 2026-10-30

## Status

Accepted.

## Context and Problem Statement

Fjord currently requires application changes to add each language server.
Operators need to supply their own servers and dependencies without allowing a reviewed PR to select executable commands.

## Considered Options

1. Operator-defined YAML profiles with built-in defaults
2. Hardcoded per-language server registry

## Decision Outcome

Chosen: **option 1**.

Load trusted operator YAML at startup and launch matching language servers through the existing isolated stdio transport. Preserve Go-specific dependency checks through an explicit adapter. Select actions from initialize capabilities and require UTF-16 positions. Reviewed repositories cannot supply Fjord process profiles.

## Consequences

### Good

- Additional languages do not require Fjord code changes.
- Existing Go and Terraform users keep defaults.
- The UI only offers actions advertised by the active server.

### Bad

- Operators must install compatible runtimes and declare the read paths their server requires.
- Runtime read paths expand the trusted filesystem surface.
- Server-specific initialization and settings still require operator knowledge; network access remains unavailable.

### Rejected because

- A hardcoded registry requires a Fjord release for each additional server and prevents operators from bringing their preferred implementation.
