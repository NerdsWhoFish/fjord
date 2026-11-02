# Fjord

Review GitHub pull requests with semantic code navigation and MCP assistance from your preferred AI client.

Keep the diff, surrounding code, and your review together.
Follow symbols to their definitions or implementations, inspect references, and submit reviews to GitHub.
This is an early local application.
Go and Terraform navigation work out of the box; operator-defined language servers add other languages.

## Run

Published binaries, container images, and signed Chrome extension packages use the [Quill release workflow](docs/releases.md).
The extension also ships as a ZIP for Chrome's local **Load unpacked** installation.

Requirements: Go 1.27+, Node.js 22+, GitHub CLI, and `gopls` for Go navigation or `tofu-ls` for Terraform navigation on `PATH`.
Navigation requires macOS `sandbox-exec` or Linux Bubblewrap (`bwrap` with user namespaces enabled).
Fjord refuses to run an unconfined language server.

```sh
gh auth login --hostname github.com
sh scripts/install-language-servers.sh
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

Direct links open a review on page load:

```text
http://127.0.0.1:4727/spacelift-solutions/demo/229
http://127.0.0.1:4727/?owner=spacelift-solutions&repo=demo&number=229
http://127.0.0.1:4727/?pr=https://github.com/spacelift-solutions/demo/pull/229
```

Successful loads normalize to `/owner/repo/number`. Refresh and browser back/forward preserve the selected PR.
To serve behind a TLS reverse proxy, explicitly configure the public origin:

```sh
./bin/fjord -addr 0.0.0.0:4727 -public-url https://fjord.stout.zone
```

`FJORD_PUBLIC_URL` is the equivalent environment setting. Use either `-public-url` or `-host`.
The proxy must preserve the configured Host header. Fjord does not infer trust from forwarded headers.
This configures routing; it does not provision DNS, TLS, or a public deployment.

`make check` requires both language servers and runs frontend tests/build, Go vet, race tests, real sandboxed `gopls` and `tofu-ls` integration tests, and formatting checks.
`make install` checks and installs the embedded native application to `~/bin/fjord`.
Make targets disable inherited Go workspaces.

## Review workflow

- Load PR metadata and paginated file diffs using your GitHub CLI identity.
- Read code at exact revisions. Deleted lines use the merge base; renamed files use their previous path on the left side.
- Follow symbols using contextual definition, reference, and type-information actions in isolated revision workspaces. Go also supports implementation navigation.
- Draft single-line or multiline comments and explicitly submit a comment review, approval, or request for changes. Submission checks the head and merge-base SHAs and pins GitHub's `commit_id` to the reviewed head.
- Read regular PR conversation and submitted review decisions in the review dialog.
- Read line-level review threads directly beside their code and reply inline.
- Choose unified or split diffs in Settings; the preference persists across refreshes.
- Read GitHub-flavored Markdown and sanitized HTML in conversation, with Write/Preview tabs for review text.
- View unchanged renamed files and bounded reconstructed text diffs when GitHub omits a patch.
- Connect an MCP client to inspect the open review, follow code, and propose attributed comment or suggestion drafts.

Reconstructed views use the pinned revisions and remain read-only because GitHub did not provide comment anchors.
They support navigation and syntax highlighting; use the review summary for feedback.
The fallback is limited to 256 KiB and 2,000 lines per side, with separate messages for binary, oversized, and temporarily unavailable content.
Successful review submission returns to the GitHub PR; failures keep the draft in Fjord.

GitHub can receive a push after the freshness check.
Branch protection remains responsible for dismissing stale approvals.
GitHub remains the source of truth.

Hover or activate a symbol to open its code actions.
Choose **Go to** to look up definitions and references together. The same popup shows definitions first, followed by references.
Targets can be unchanged files anywhere in the extracted revision and open in the source pane without adding them to the changed-file list.
Use the line-number gutter to select a line or drag a range, then choose the comment action.
Shift-click extends a selection without dragging.
Multiline comments stay on one side within one diff hunk; ranges cannot span omitted code.
Choose **Add to review**, then select **Comment**, **Approve**, or **Request changes** below the PR title.
The review dialog contains the editable summary, decision, draft comments, and existing conversation.
Closing it preserves drafts; only its final submit button sends the review to GitHub.
The larger comment editor syntax-highlights the selected code and accepts Markdown.
Use **Insert suggestion** on head-side lines to insert a GitHub suggestion block prefilled with the selected code, then edit the replacement before adding it to your review.
The hamburger **Settings** menu contains the **Theme** selector, offering System, Light, Dark, and Dracula.
System follows your OS preference; an explicit choice is remembered and synchronized across browser tabs.

Changed files appear in an expandable folder tree with status icons, full-path tooltips, and viewed markers.
Search matches current and previous paths, and the filter menu supports change status and unviewed files.
The diff area scrolls through all changed files; selecting a tree entry jumps to that file.
Use the context controls to reveal omitted lines above, below, or between hunks in increments of 20.
Expanded context supports semantic navigation, while review comments remain limited to GitHub's original diff lines.
Navigation results replace the actions in the symbol popup; choose a destination there or return to its actions with Back.
The source pane remains available for further symbol navigation from each destination.
The sidebar follows the file at the top of the scrolling diff, and each file header has its own Viewed checkbox.
Context controls include directional arrows and check the source before offering more lines below the final hunk.
The PR title links to GitHub and includes the PR number; repository details appear once in the main heading.
The right sidebar automatically loads the active file's outline and follows opened source files at their exact revision.
Use **Search symbols** in the heading or Ctrl/Cmd+K to find declarations across the workspace.

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

Symbol menus discover navigation and inspection support from the server's actual `initialize` response.
Unsupported actions stay hidden; server startup failures offer a retry without blocking comments.
The transport currently supports stdio and UTF-16 positions, not remote TCP/WebSocket servers or other position encodings.

## Language-server review tools

The outline sidebar, heading search, inline diagnostics, and symbol tools expose the features the language server supports:

| Feature | Use during review |
| --- | --- |
| Diagnostics | See severity markers and squiggles inline; open messages and available fixes. |
| Incoming / outgoing calls | Follow callers or callees, including reported call sites. |
| Supertypes / subtypes | Explore the selected type's hierarchy. |
| File outline | Browse automatically loaded functions, types, and declarations in the right sidebar. |
| Workspace symbols | Search from the heading across the selected project root, including unchanged files. |
| Highlight occurrences | Mark reads and writes of the selected symbol inline. |
| Inlay hints | Show inferred types or parameter names without changing source offsets. |
| Code actions | Inspect a proposed fix and explicitly add review suggestion drafts. |
| Preview rename | Enter a new name and inspect the edits across files. |

Explicit symbol-navigation results stay in the initiating popup. Inline annotations are tied to the exact file and revision and can be cleared.
The Go default profile enables parameter-name, assigned-variable-type, and range-variable-type hints; custom servers may require their own hint settings.
Diagnostics use pull reports when available or wait briefly for push publications. A server that has not published is reported as pending, not as having no problems.
Reports load as files mount, with bounded caching and concurrency. File headers show counts, a visibility toggle, and retry states.
Head diagnostics never decorate deleted base lines; base source views and removed files request their own revision's report.

Code actions and rename never modify the workspace. Server commands and file creation, deletion, or renaming are not executed.
Edit previews validate every source range and reject overlapping or out-of-revision changes.
Code actions can become suggestions only when every edit fits existing head-side PR diff lines; edits on the same line are combined and multiline review boundaries are preserved.
Fixes that touch unchanged files remain previews because GitHub inline suggestions must attach to reviewable diff lines.
Available results still depend on installed dependencies and the server's indexing; bounded response and source limits prevent oversized results from blocking the review.

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
The base AMD64 image has been built and exercised on Talos Kubernetes with real gopls, tofu-ls, and sandbox isolation tests.
That deployment uses a pod user namespace (`hostUsers: false`), `procMount: Unmasked` for nested procfs, and a Localhost seccomp profile derived from containerd's default with Bubblewrap namespace setup syscalls allowed.
It remains nonroot, with all capabilities dropped and privilege escalation disabled.
The optional Python container recipe has not yet been exercised.

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

Dependency provisioning, hosted accounts, check-run views, posting top-level issue comments, and thread resolution remain future work.
The GitHub PR link provides actions not yet exposed here.

## MCP assistance

Fjord exposes a Streamable HTTP MCP server at `/mcp`. Open a real PR in the browser, then configure your preferred MCP client with the same service's endpoint, for example `http://127.0.0.1:4727/mcp`.
The Settings menu shows the endpoint and browser-session connection state.
No model provider, API key, or embedded chat is configured in Fjord.

An MCP client can discover open review sessions, read draft and published inline comments, PR conversation and review decisions, read code at the pinned revision, query the language server, and show source or navigation results in the browser.
It can propose inline comments and GitHub suggestion drafts. These remain visibly AI-attributed drafts for the reviewer to inspect, edit, or remove.
Tools do not submit reviews, reply on GitHub, execute language-server commands, or write source files.

Browser actions are queued with idempotency keys and acknowledged after the tab applies them.
Tool responses distinguish pending delivery from applied or failed delivery.
The browser remains the owner of its draft and mirrors it to the service; incoming drafts merge without replacing human edits.
Sessions are tied to the exact head and merge-base SHAs. A newly pushed revision requires opening a new review session.

Sessions and pending actions are bounded, expire, and live in one server process.
Restarting the service discards pending delivery; already applied browser drafts remain in sessionStorage.
Use a single service replica for this workflow. A connected browser tab is required to display results or accept drafts.
Review content and source returned through MCP are untrusted repository data, not instructions.
Your chosen AI client controls what it sends to its model provider.

## Chrome extension

The [Chrome extension](extension/README.md) redirects GitHub PR `/changes` pages to `https://fjord.stout.zone/owner/repo/number`.
It handles both full navigation and GitHub's in-page history transitions, without redirecting ordinary PR conversation pages.
Load the unpacked extension in a development browser using the instructions in its README.
The extension does not configure or deploy the Fjord service.

## Security and telemetry

Fjord binds to loopback by default and supports explicitly configured trusted LAN access.
Exact Host and Origin checks, JSON-only API requests, and a restrictive Content Security Policy block requests from unrelated websites.
LAN mode, including MCP, has no authentication and is not a multi-user security boundary; use it only on a trusted network.
Native MCP clients may omit Origin; supplied origins must match the configured public origin and cross-site browser requests are rejected.
GitHub credentials stay in the backend.
Browser draft storage contains review text, so use a trusted browser profile.

JSON logs contain trace/span IDs.
Set `OTEL_EXPORTER_OTLP_ENDPOINT` and `OTEL_EXPORTER_OTLP_HEADERS` through the runtime environment to export logs and traces using OTLP HTTP/protobuf.
Without an endpoint, telemetry stays local.
Set `FJORD_ENVIRONMENT=production` in hosted deployments so backend signals and the browser relay identify the deployment correctly; it defaults to `local`.
W3C TraceContext and Baggage are supported.
Never put ingestion credentials in Git, command arguments, logs, or browser code.

Optional RUM uses the shared `@nerdswhofish/browser-telemetry` Faro package and a same-origin relay.
Configure `FJORD_FARO_URL` on the server.
The relay rebuilds allowlisted error, event, and Web Vitals envelopes.
It discards user/session metadata, arbitrary text, source paths, and browser trace payloads.
Code, review text, credentials, and raw repository identities are never logged.

## Development

The Go packages separate GitHub, navigation, telemetry, and HTTP boundaries; MCP tools reuse these services.
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
