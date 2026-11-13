# Configuration and operations

Start with the [quick start](../README.md). This reference covers hosting, language servers, MCP, and troubleshooting.

## Network access

Fjord binds to loopback by default. For another local port:

```sh
./bin/fjord -addr 127.0.0.1:4728
```

For a trusted LAN, replace the example address with the server's IP:

```sh
./bin/fjord -addr 0.0.0.0:4727 -host 192.168.1.10
```

Fjord has no login. Every connected user acts through the server's GitHub identity, including MCP clients. Keep access on a trusted network or put an authentication boundary in front of it.

Behind a TLS reverse proxy, configure the browser-facing origin:

```sh
./bin/fjord -addr 0.0.0.0:4727 -public-url https://review.example.com
```

`FJORD_PUBLIC_URL` is the environment equivalent. Use either `-public-url` or `-host`; the proxy must preserve the configured Host header. Fjord checks Host and Origin explicitly and does not trust forwarded headers. These flags do not provision DNS or TLS.

Reviews also accept query parameters, then normalize to `/owner/repo/number`:

```text
/?owner=OWNER&repo=REPO&number=123
/?pr=https://github.com/OWNER/REPO/pull/123
```

## Language servers

See the [pinned language bundle](../languages/README.md) for versions, installation, and dependency advisories, and [offline schemas](../internal/schemas/README.md) for associations and overrides. Syntax highlighting covers more languages than semantic navigation; navigation actions depend on the selected server.

Set `FJORD_CONFIG` or pass `-config /path/to/language-servers.yaml` to load operator-owned profiles at startup. Restart after changes. Fjord never reads its process configuration from the reviewed repository.

The [example profile](../examples/language-servers.yaml) changes bundled Pyright to strict checking. A named profile replaces that default entirely; other defaults remain enabled. Explicit profiles sort alphabetically before defaults, and the first matching profile wins.

| Field | Meaning |
| --- | --- |
| `command` | Executable and literal arguments. No shell expansion; scripts need an explicit interpreter. |
| `files` | Relative globs supporting `*`, `?`, and whole-directory `**`. |
| `languageId` | LSP document language identifier. |
| `rootMarkers` | Basenames identifying the nearest project root; otherwise use the revision root. |
| `rootRequired` | Fail when none of the root markers exist. |
| `initializationOptions` | Server-specific initialization values. |
| `settings` | Server configuration sent after initialization and returned for configuration requests. |
| `runtimePaths` | Absolute read-only paths for libraries, server code, and installed dependencies. |
| `environment` | Explicit non-sensitive values; isolation and loader variables are protected. |
| `adapter` | `generic` by default; `go` enables Go dependency preparation and package checks. |
| `disabled` | Disable a named profile, such as `go: {disabled: true}`. |

Install custom servers and dependencies on the host or in a derived image. Grant only their required runtime paths, never an entire home directory or credential folder. Servers receive read-only source, temporary writable storage, a clean environment, and no network access. Fjord does not run repository installation hooks or commands returned by code actions.

The transport supports stdio and UTF-16 positions. TCP/WebSocket servers and other position encodings are unsupported. Startup failures offer a retry without blocking comments.

### Native Node runtimes

`FJORD_LANGUAGE_BUNDLE` selects an absolute bundle directory. Runtime profiles default to `/opt/fjord/languages`; the native installer defaults to the checkout's `languages` directory and honors this variable when set.

Homebrew Node may need additional shared-library directories inside the sandbox:

```sh
otool -L "$(command -v node)"
```

Inspect the listed libraries' dependencies too. Set `FJORD_NODE_RUNTIME_PATHS` to the precise library directories, separated by the OS path-list separator (`:` on macOS/Linux). Include Node's library directory where required. Prefer stable paths such as `/opt/homebrew/opt/node/lib` over versioned Cellar paths, and recheck after upgrades. Never grant the entire Homebrew prefix.

Custom Node commands should include `--openssl-config=/dev/null`. Fjord also supplies a sanitized OpenSSL environment setting for child processes that replace their arguments.

## Containers

The [Dockerfile](../Dockerfile) includes the language servers, Go, Node, GitHub CLI, and Bubblewrap:

```sh
docker build --build-arg APP_VERSION="$(git rev-parse HEAD)" -t fjord:local .
```

The image runs as UID `10001`, listens on `0.0.0.0:4727`, and needs writable temporary storage. Provide GitHub credentials at runtime and configure the browser-facing host or public URL. Mount custom profiles read-only and pass `-config` to their mounted path. The [Python image example](../examples/Dockerfile.python) supplies a stricter profile; Pyright is already bundled.

The kernel and runtime must permit nested unprivileged user namespaces and Bubblewrap's namespace operations. The tested Kubernetes configuration uses:

- A pod user namespace: `hostUsers: false`.
- `procMount: Unmasked` for nested procfs.
- A scoped Localhost seccomp profile allowing Bubblewrap namespace setup.
- A nonroot user, all capabilities dropped, and privilege escalation disabled.

Build the seccomp profile from the runtime's current default, allowing the required `clone`, `unshare`, `mount`, `umount2`, `pivot_root`, and `sethostname` operations. Revalidate it after runtime upgrades. Do not use a privileged container or disable the sandbox to bypass failures.

Use one service replica: review/MCP sessions are process-local. See [release packaging](releases.md) for native archives, images, and extension signing.

## Navigation and review limits

### Go dependencies

Fjord selects the nearest Go module. Local replacements must remain inside the revision; vendored projects work offline.

For unvendored projects, Fjord downloads required versions pinned by committed `go.mod` and `go.sum` files. Only `proxy.golang.org` and its Google Storage artifact redirects are allowed. Fetches are bounded; metadata checksums are verified, and Go verifies and extracts archives inside the offline sandbox. gopls receives a read-only cache.

For private dependencies or fully offline operation:

1. Run `go mod download` for the reviewed project outside Fjord.
2. Mount that machine's module cache read-only, normally the directory from `go env GOMODCACHE`.
3. Set `FJORD_GO_MODULE_CACHE` to the mounted path.

Fjord verifies artifacts from `cache/download` against the PR's checksums. This mode never falls back to the public proxy, so the cache must contain public dependencies too.

Missing metadata, checksums, vendoring errors, and incompatible toolchains produce specific errors. Automatic toolchain downloads and CGO are disabled. Upgrade the Go installation when a reviewed module requires a newer version.

Usable repository packages remain navigable when other packages lack generated assets or have errors. A wholly unloadable workspace still fails. Legacy modules with a `go` directive below `1.17` may need vendoring, or an intentional directive upgrade followed by committed `go mod tidy` output; tidy alone preserves the older graph.

### Other boundaries

- Terraform indexes configuration and local modules without `tofu init`. Remote modules and provider schemas may remain unresolved.
- Navigation opens files within the extracted revision, including unchanged files. External dependency and standard-library source are not exposed.
- Code actions and rename are previews. Suggestions require edits that fit existing head-side diff lines; commands and filesystem mutations are not executed.
- Multiline comments must stay on one side within one original diff hunk. Expanded context and reconstructed patches have no GitHub comment anchors.
- Omitted-patch reconstruction is read-only and limited to 256 KiB and 2,000 lines per side.
- GitHub remains responsible for dismissing stale approvals through branch protection. Check-run views, top-level issue comments, and thread resolution are not exposed in Fjord.

Workspaces are temporary and bounded. Archive extraction rejects traversal, links, special files, and oversized input.

## MCP assistance

Open a real PR in a browser, then connect your MCP client to the same service's Streamable HTTP endpoint:

```text
http://127.0.0.1:4727/mcp
```

Settings shows the endpoint and browser connection state. Fjord does not need a model-provider API key.

Choose **Settings > Manage review sessions** to see sessions, their last sync, and their IDs. Remove old sessions after confirming; your current tab is protected. Removal disconnects AI assistance and discards pending actions, while local browser drafts and GitHub comments remain intact. An affected open tab can use **Reconnect** in Settings. Removal markers last 24 hours; ordinary inactive sessions expire after two hours.

Tools can read the review and pinned source, query language servers, show navigation results in the browser, and propose AI-attributed comment or suggestion drafts. They cannot submit reviews, reply on GitHub, run server commands, or edit source.

Browser actions require a connected tab and return pending, applied, or failed delivery status. Drafts merge without replacing human edits. Sessions are revision-bound: reopen the PR after a new push. Restarting the service discards pending actions; drafts already applied in the browser remain in its session storage.

Source and review content returned through MCP are untrusted data. Your AI client determines what reaches its model provider.

## Telemetry

JSON logs include trace/span IDs. Configure these runtime environment variables:

| Variable | Purpose |
| --- | --- |
| `FJORD_ENVIRONMENT` | Set `production` for hosted deployments; defaults to `local`. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTLP HTTP/protobuf destination for traces and logs. Without it, telemetry stays local. |
| `OTEL_EXPORTER_OTLP_HEADERS` | Backend-only ingestion credentials, if required. |
| `FJORD_FARO_URL` | Optional Faro browser RUM collector, reached through Fjord's same-origin relay. |

W3C TraceContext and Baggage are supported. The browser relay allowlists error, event, and Web Vitals data and strips user/session metadata, arbitrary text, source paths, and browser trace payloads. Keep credentials out of Git, shell arguments, logs, and browser code.

The Faro relay sends the configured application origin upstream for origin-restricted collector admission. It never forwards browser credentials, session identifiers or incoming Origin values. Browsers continue sending to the same-origin `/api/telemetry` endpoint, so switching the server's collector URL requires no new browser CSP destination. Deploy the compatible relay before switching to a collector that requires Origin.

## Development

`web/` contains React/TypeScript; Go packages separate GitHub, navigation, telemetry, and HTTP services. MCP reuses those services.

| Command | Runs |
| --- | --- |
| `make build` | Frontend build and embedded Go binary. |
| `make check` | Frontend/signing tests, vet, Go race tests, real language-server tests, and formatting checks. |
| `make test` | Go package tests; opt-in installed-server suites require the flags below. |
| `make install` | Checks, builds, and installs to `~/bin/fjord`. |

Install the pinned Go and Node servers before `make check`, with a supported OS sandbox. Make targets disable inherited Go workspaces.

For targeted integration tests, use `FJORD_TEST_GOPLS=1`, `FJORD_TEST_TOFU_LS=1`, and `FJORD_TEST_NODE_LSP=1`. Node tests also need `FJORD_LANGUAGE_BUNDLE` and any native runtime paths. `FJORD_TEST_PYRIGHT_CONFIG` enables the separate custom-Pyright profile test.

Settings offers Light, Dark, Dracula, and True Black themes, or follows your system preference. Code colors can follow the theme or use GitHub, Atom One, or Monochrome independently. Both choices are saved in your browser.

Theme palettes live in `web/src/theme.css`, and code color overrides live in `web/src/code-styles.css`. Register choices in `theme.ts` and cover every theme/code color combination in `theme.test.ts`. Architectural decisions live in [adr/](../adr/README.md).

Syntax colors use the published [GitHub VS Code theme](https://github.com/primer/github-vscode-theme), [Atom One Light](https://github.com/atom/one-light-syntax) / [One Dark](https://github.com/atom/one-dark-syntax), and [Dracula Prism](https://github.com/dracula/prism) palettes. GitHub and Atom One switch between their light and dark variants with the page theme. Muted syntax comments retain the upstream palette colors.
