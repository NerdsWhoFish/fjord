# Fjord

Review GitHub pull requests with semantic code navigation and optional AI assistance.

Keep the diff, surrounding code, and your review together.
Follow symbols to their definitions or implementations, inspect references, and submit reviews to GitHub.
This is an early local application.
Go is the first language supported for semantic navigation; the UI displays diffs in other languages too.

## Run

Requirements: Go 1.27+, Node.js 22+, GitHub CLI, and `gopls` on `PATH`.
Navigation requires macOS `sandbox-exec` or Linux Bubblewrap (`bwrap` with user namespaces enabled).
Fjord refuses to run an unconfined language server.

```sh
gh auth login --hostname github.com
go install golang.org/x/tools/gopls@v0.24.0
make build
./bin/fjord
```

Open the loopback URL printed by the CLI, normally `http://127.0.0.1:4727`, and paste a GitHub PR URL.
The demo explores the UI without accessing GitHub and cannot submit reviews.
`-addr 127.0.0.1:4728` selects another local port.

`make check` runs frontend tests/build, Go vet, race tests, real sandboxed `gopls` integration tests, and formatting checks.
`make install` checks and installs the embedded native application to `~/bin/fjord`.
Make targets disable inherited Go workspaces.

## Review workflow

- Load PR metadata and paginated file diffs using your GitHub CLI identity.
- Read code at exact revisions. Deleted lines use the merge base; renamed files use their previous path on the left side.
- Navigate definitions, implementations, references, and hover information with `gopls` in isolated revision workspaces.
- Draft line comments and explicitly submit a comment review, approval, or request for changes. Submission checks the head and merge-base SHAs and pins GitHub's `commit_id` to the reviewed head.
- Read inline review comments and reply to them.
- Ask an optional AI provider about context you explicitly choose to send.

GitHub can receive a push after the freshness check.
Branch protection remains responsible for dismissing stale approvals.
GitHub remains the source of truth.

## Navigation limits

The first version supports a Go module at the repository root with standard-library, local, or vendored dependencies.
Network access, CGO, automatic toolchain downloads, and VCS dependency fetching are disabled.
Missing dependencies are reported rather than presenting incomplete results as complete.

Archive extraction rejects symlinks, hardlinks, special files, traversal, and oversized archives.
The source pane cannot open files outside the extracted tree, including standard-library and dependency-cache files.
Workspaces are temporary, bounded, and removed at shutdown.

TypeScript, multi-module projects, dependency provisioning, hosted accounts, check-run views, issue comments, and thread resolution remain future work.
The GitHub PR link provides actions not yet exposed here.

## AI assistance

AI is disabled until the server is configured.
Set `FJORD_AI_ENDPOINT` to a full Chat Completions-compatible endpoint URL, `FJORD_AI_MODEL` to your chosen model, and `FJORD_AI_API_KEY` through your secret manager when required.
Local model servers can use loopback HTTP; remote endpoints require HTTPS.

The provider receives the question and selected code/diff context only when you ask.
Check its data policy before sending private code.
AI has no tools, filesystem access, GitHub token, or review-submission privileges.
Its answers are suggestions for the human reviewer, rendered as text.
Provider errors do not expose response bodies or credentials.

The adapter follows the [Chat Completions message contract](https://developers.openai.com/api/reference/resources/chat/subresources/completions/methods/create).
Compatibility and model availability depend on the configured provider.

## Security and telemetry

Fjord binds only to loopback.
Exact Host and Origin checks, JSON-only API requests, and a restrictive Content Security Policy protect the authenticated local service.
Do not expose it through a reverse proxy; it is not a multi-user security boundary.
GitHub credentials stay in the backend.
Browser draft storage contains review text, so use a trusted browser profile.

JSON logs contain trace/span IDs.
Set `OTEL_EXPORTER_OTLP_ENDPOINT` and `OTEL_EXPORTER_OTLP_HEADERS` through the runtime environment to export logs and traces using OTLP HTTP/protobuf.
Without an endpoint, telemetry stays local.
W3C TraceContext and Baggage are supported.
Never put ingestion credentials in Git, command arguments, logs, or browser code.

Optional RUM uses the shared `@nerdswhofish/browser-telemetry` Faro package and a same-origin relay.
Configure `FJORD_FARO_URL` on the server.
The relay rebuilds allowlisted error, event, and Web Vitals envelopes.
It discards user/session metadata, arbitrary text, source paths, and browser trace payloads.
Code, review text, credentials, raw repository identities, and provider bodies are never logged.

## Development

The Go packages separate GitHub, navigation, AI transport, telemetry, and HTTP boundaries.
`web/` contains React/TypeScript.
`make check` and CI require real `gopls` and a supported sandbox.
`make test` runs Go unit tests; use `FJORD_TEST_GOPLS=1 make test` to include language-server integration tests.

Goblin currently provides a foundation and UI concepts, not an implemented PR engine.
Plannotator's edit-approval lifecycle differs from GitHub reviews.
Fjord reuses language servers, GitHub APIs, OpenTelemetry, and the shared browser telemetry package.
Architectural tradeoffs live in [adr/](adr/README.md).

Changes require human review before publication.
A passing local build does not imply a hosted deployment or production telemetry verification.

## License

MIT. See [LICENSE](LICENSE).
