# 1. Start with a local review service and isolated language servers

Date: 2026-10-30

## Status

Proposed.

## Context and Problem Statement

Fjord must combine GitHub review actions, language-server navigation and optional AI assistance.
The first implementation must validate real semantic navigation without requiring a multi-user hosting platform.

## Considered Options

1. Local Go service with a React browser UI and sandboxed language servers
2. Hosted multi-user service with remotely provisioned workspaces
3. Browser-only UI using syntax matching for navigation

## Decision Outcome

Chosen: **option 1**.

Implement a loopback-only Go service with a React UI, GitHub CLI authentication, and gopls workspaces pinned to exact PR base and head revisions. Require OS isolation for language servers. Submit reviews to GitHub only after explicit confirmation and a fresh head-SHA check. AI is optional, uses a configured compatible endpoint, receives only explicitly selected context, and has no tool or review-submission privileges.

## Consequences

### Good

- Semantic navigation can be validated with real language servers.
- Credentials remain on the host and GitHub remains the review source of truth.
- The local boundary avoids building tenant management before the core review workflow works.

### Bad

- Users must install the CLI and supported language-server sandbox dependencies.
- Offline dependency resolution can limit navigation when dependencies are unavailable.
- Hosted access and languages beyond Go require subsequent work.

### Rejected because

- Hosted workspaces are deferred because tenancy, credentials and resource lifecycle would broaden the first implementation substantially.
- Syntax matching cannot reliably implement definitions, implementations or references and does not meet the core requirement.
