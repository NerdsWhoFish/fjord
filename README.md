# Fjord

Review GitHub pull requests with semantic code navigation and optional AI assistance.

Keep the diff, surrounding code, and your review together.
Follow symbols to their definitions or implementations, inspect references, and submit reviews to GitHub.
This is an early local application.
Go and Terraform navigation work out of the box; operator-defined language servers add other languages.

## Run

Requirements: Go 1.27+, Node.js 22+, GitHub CLI, and `gopls` for Go navigation or `tofu-ls` for Terraform navigation on `PATH`.
Navigation requires macOS `sandbox-exec` or Linux Bubblewrap (`bwrap` with user namespaces enabled).
Fjord refuses to run an unconfined language server.

```sh
gh auth login --hostname github.com
go install golang.org/x/tools/gopls@v0.24.0
go install github.com/opentofu/tofu-ls@v0.5.3
make build
./bin/fjord
```

Open the loopback URL printed by the CLI, normally `http://127.0.0.1:4727`, and paste a GitHub PR URL.
The demo explores the UI without accessing GitHub and cannot submit reviews.
`-addr 127.0.0.1:4728` selects another local port.

To browse from another device on a trusted LAN, bind all interfaces and specify the server IP or hostname used in your browser:

```sh
./bin/fjord -addr 0.0.0.0:4727 -host 192.168.1.10
```

Replace the example IP with your server's LAN address, then open the printed URL.
There is no Fjord login; connected users act through the server's GitHub CLI identity.

`make check` requires both language servers and runs frontend tests/build, Go vet, race tests, real sandboxed `gopls` and `tofu-ls` integration tests, and formatting checks.
`make install` checks and installs the embedded native application to `~/bin/fjord`.
Make targets disable inherited Go workspaces.

## Review workflow

- Load PR metadata and paginated file diffs using your GitHub CLI identity.
- Read code at exact revisions. Deleted lines use the merge base; renamed files use their previous path on the left side.
- Follow symbols using contextual definition, reference, and type-information actions in isolated revision workspaces. Go also supports implementation navigation.
- Draft single-line or multiline comments and explicitly submit a comment review, approval, or request for changes. Submission checks the head and merge-base SHAs and pins GitHub's `commit_id` to the reviewed head.
- Read inline review comments and reply to them.
- Ask an optional AI provider about context you explicitly choose to send.

GitHub can receive a push after the freshness check.
Branch protection remains responsible for dismissing stale approvals.
GitHub remains the source of truth.

Hover or activate a symbol to open its code actions.
Use the line-number gutter to select a line or drag a range, then choose the comment action.
Shift-click extends a selection without dragging.
Multiline comments stay on one side within one diff hunk; ranges cannot span omitted code.
Choose **Add to review**, then submit the accumulated drafts from the **Review** panel.
The hamburger **Settings** menu contains the **Theme** selector, offering System, Light, Dark, and Dracula.
System follows your OS preference; an explicit choice is remembered and synchronized across browser tabs.

Changed files appear in an expandable folder tree with status icons, full-path tooltips, and viewed markers.
Search matches current and previous paths, and the filter menu supports change status and unviewed files.
The diff area scrolls through all changed files; selecting a tree entry jumps to that file.
Use the context controls to reveal omitted lines above, below, or between hunks in increments of 20.
Expanded context supports semantic navigation, while review comments remain limited to GitHub's original diff lines.
Reference results populate the destination selector above the code so you can choose where to jump without scrolling to the end of a file.
The source pane remains available for further symbol navigation from each destination.

Diffs and source panes include syntax highlighting for Go, JavaScript/TypeScript and JSX/TSX, Terraform/HCL, Python, shell, JSON, YAML, HTML/XML, CSS, Markdown, SQL, Rust, Ruby, Java, C/C++, C#, Swift, Dockerfiles, and TOML.
Highlighting preserves symbol-click positions and runs in a disposable browser worker.
Unknown formats remain plain text; inputs over 250,000 characters or expensive grammars also fall back to plain text so review controls remain responsive.
Diff highlighting uses separate base/head text within each hunk; omitted context can limit grammar accuracy, while full source provides continuous lexical context.
Syntax colors do not imply language-server support: semantic actions depend on the configured server's capabilities.

## Bring your own language server

Pass `-config /path/to/language-servers.yaml` or set `FJORD_CONFIG` to an operator-owned YAML file.
Fjord reads it at startup, never from the reviewed repository. Restart after changes.
The [Python example](examples/language-servers.yaml) configures Pyright through an explicit Node executable.
Go (`go`), Terraform (`terraform`), and Terraform variables (`terraform-vars`) profiles remain available unless replaced or disabled.

```yaml
languageServers:
  python:
    command: [/opt/lsp/node, --openssl-config=/dev/null, /opt/lsp/node_modules/pyright/dist/pyright-langserver.js, --stdio]
    files: ["**/*.py", "**/*.pyi"]
    languageId: python
    rootMarkers: [pyproject.toml, pyrightconfig.json, setup.py]
    runtimePaths: [/opt/lsp]
    settings:
      python:
        analysis:
          typeCheckingMode: basic
```

Profiles support these fields:

| Field | Purpose |
| --- | --- |
| `command` | Executable and literal arguments, with no shell expansion. Use an explicit interpreter for scripts. |
| `files` | Repository-relative globs with `*`, `?`, and whole-directory `**`; first matching profile wins. |
| `languageId` | LSP document language identifier. |
| `rootMarkers` | Basenames used to find the nearest project root, falling back to the revision root. |
| `rootRequired` | Fail if none of the root markers exist. |
| `initializationOptions` | Server-specific options sent with `initialize`. |
| `settings` | Server configuration sent after initialization and returned for configuration requests. |
| `runtimePaths` | Explicit absolute read-only paths for runtime libraries, scripts, and installed dependencies. |
| `environment` | Explicit non-sensitive server environment values; isolation and loader variables cannot be overridden. |
| `adapter` | `generic` by default; `go` preserves Go's toolchain and dependency checks. |
| `disabled` | Remove a named profile, for example `go: {disabled: true}`. |

An explicit profile replaces the entire built-in profile of the same name.
Explicit names sort alphabetically before remaining defaults, making overlapping matches deterministic.
Configuration rejects unknown fields, duplicate keys, unsafe paths, and oversized inputs before the service starts.

Install the server and its dependencies on the host or in a derived container image.
Declare only the runtime paths it needs, never your whole home directory or credential folders.
Some packaged runtimes, including Homebrew Node, require additional shared-library directories.
Servers receive a clean environment, read-only source, temporary writable storage, and no network access.
Repository configuration may influence the language server itself, so its executable and runtime paths must be trusted.
Fjord does not run repository installation hooks or fetch dependencies for it.

Symbol menus discover definition, implementation, references, and hover support from the server's actual `initialize` response.
Unsupported actions stay hidden; server startup failures offer a retry without blocking comments or AI.
The transport currently supports stdio and UTF-16 positions, not remote TCP/WebSocket servers or other position encodings.

## Containers

The Dockerfile includes `gopls`, `tofu-ls`, Go, GitHub CLI, and Bubblewrap.
Build the base image and optional Python variant:

```sh
docker build --build-arg APP_VERSION="$(git rev-parse HEAD)" -t fjord:local .
docker build -f examples/Dockerfile.python -t fjord:python .
```

Mount the example configuration read-only and pass `-config /etc/fjord/language-servers.yaml`.
Provide the server's GitHub CLI identity through runtime credentials, and set `-host` to the hostname browsers use.
The image runs as UID 10001 and listens on `0.0.0.0:4727`; allow writable temporary storage.

The host kernel and container runtime must permit nested unprivileged user namespaces and Bubblewrap's required namespace operations.
A Kubernetes cluster with user namespaces disabled cannot run this LSP isolation model unchanged.
Fjord fails closed; do not work around that with a privileged container or by removing the sandbox.
The container recipes have not yet been built or exercised in a container runtime; native macOS integration tests cover the language-server behavior.

## Navigation limits

The language server is selected from the file being inspected rather than initialized as Go for every repository.
Go navigation uses the nearest Go module with standard-library, local, or vendored dependencies.
Network access, CGO, automatic toolchain downloads, and VCS dependency fetching are disabled.
Missing dependencies are reported rather than presenting incomplete results as complete.

Terraform navigation uses `tofu-ls` to index configuration and local modules without running `tofu init`.
Supported Terraform files are `.tf`, `.tf.json`, `.tfvars`, `.tfvars.json`, and `.tofu`.
Provider downloads and execution are unavailable in the sandbox.
Remote modules and provider-specific schema information can therefore remain unresolved.

Archive extraction rejects symlinks, hardlinks, special files, traversal, and oversized archives.
The source pane cannot open files outside the extracted tree, including standard-library and dependency-cache files.
Workspaces are temporary, bounded, and removed at shutdown.

Dependency provisioning, hosted accounts, check-run views, issue comments, and thread resolution remain future work.
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

Fjord binds to loopback by default and supports explicitly configured trusted LAN access.
Exact Host and Origin checks, JSON-only API requests, and a restrictive Content Security Policy block requests from unrelated websites.
LAN mode has no authentication and is not a multi-user security boundary; use it only on a trusted network.
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
Theme palettes live in `web/src/theme.css`; components consume semantic CSS variables for surfaces, controls, diff states, file-tree states, and syntax tokens.
To add a named theme, define its palette, extend the theme choices in `web/src/theme.ts` and `web/src/ThemeToggle.tsx`, and include its contrast cases in `web/src/theme.test.ts`.
`make check` and CI require real `gopls`, `tofu-ls`, and a supported sandbox.
`make test` runs Go unit tests; use `FJORD_TEST_GOPLS=1 FJORD_TEST_TOFU_LS=1 make test` to include language-server integration tests.
Set `FJORD_TEST_PYRIGHT_CONFIG` to a working Python profile YAML to also exercise real sandboxed Pyright definitions, references, and hover.

Goblin currently provides a foundation and UI concepts, not an implemented PR engine.
Plannotator's edit-approval lifecycle differs from GitHub reviews.
Fjord reuses language servers, GitHub APIs, OpenTelemetry, and the shared browser telemetry package.
Architectural tradeoffs live in [adr/](adr/README.md).

Changes require human review before publication.
A passing local build does not imply a hosted deployment or production telemetry verification.

## License

MIT. See [LICENSE](LICENSE).
