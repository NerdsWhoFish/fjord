# 4. Prepare pinned Go dependencies before starting the offline language server

Date: 2026-11-02

## Status

Accepted.

## Context and Problem Statement

Go package navigation currently rejects repositories whose dependencies are not vendored because the language server has no network access.
Reviewing a PR must not grant its source access to host credentials or allow arbitrary repository setup commands.

## Considered Options

1. Prepare pinned modules separately and expose a read-only cache to gopls
2. Allow gopls unrestricted network and inherited module credentials
3. Require every reviewed Go repository to vendor dependencies

## Decision Outcome

Chosen: **option 1**.

Prepare exact required module versions with committed checksums before launching gopls. Keep toolchain selection local and disable VCS fallback. Use a bounded public module proxy or an operator-provided module cache; do not inherit GitHub credentials. Keep the revision immutable and the language server offline. Reject local module replacements that leave the extracted revision. Report dependency and toolchain failures separately.

## Consequences

### Good

- Normal Go repositories can be analyzed without committing vendor directories.
- The language server retains its existing network and filesystem isolation.
- Private modules can use explicitly provisioned cache artifacts without giving repository code credentials.

### Bad

- Cold navigation must wait for dependency preparation and can fail at resource or time limits.
- Projects missing required checksums or needing a newer Go toolchain require operator or repository changes.
- Loading dependencies does not enable the UI to navigate outside the revision into dependency source.

### Rejected because

- Unrestricted gopls networking and inherited credentials widen the untrusted review process's access to host and private services.
- Requiring vendoring excludes common Go module repositories and leaves the reported navigation failure unresolved.
