# Bundled language servers

This locked npm bundle supplies Fjord's YAML, JSON/JSONC, JavaScript/TypeScript, Python, Bash, Dockerfile, HTML, and CSS/SCSS servers.
Go and OpenTofu servers use `scripts/install-language-servers.sh`.

Run `sh scripts/install-node-language-servers.sh` from the checkout, then set `FJORD_LANGUAGE_BUNDLE` to the absolute `languages` directory before starting Fjord.
The container installs the same lockfile under `/opt/fjord/languages`.
Installation disables npm lifecycle scripts.
Node must be at least 22.22.2.
Profiles use an explicit interpreter, disable OpenSSL configuration loading, and cap the Node old-generation heap at 512 MiB.
This heap limit does not cap total process memory; container resource limits remain necessary.

TypeScript is pinned to 5.9.3 because the TypeScript 7 package does not contain the `tsserver.js` implementation required by `typescript-language-server`.
The profile selects this trusted bundled compiler explicitly, disables automatic typing acquisition, and supplies no plugin locations.
The real-server integration suite checks that a repository's TypeScript plugin cannot execute.
TypeScript's child processes have their own 512 MiB heap limit.
Pyright uses worker threads that inherit Node's execution flags.

To update the bundle, change exact versions, regenerate `package-lock.json` with lifecycle scripts disabled, run `npm audit --omit=dev`, and run the installed-server suite:

```sh
FJORD_LANGUAGE_BUNDLE="$PWD/languages" FJORD_TEST_NODE_LSP=1 GOWORK=off \
  go test ./internal/lspconfig ./internal/navigation
```

Native dynamically linked Node distributions can require precise extra `FJORD_NODE_RUNTIME_PATHS`, as described in the root README.
The suite tests actual sandboxed servers, offline schema diagnostics, and repository plugin isolation.

## Current upstream advisories

The locked Bash server depends on packages with two outstanding advisories:

- [GHSA-mrhx-c7xc-m3w5](https://github.com/advisories/GHSA-mrhx-c7xc-m3w5): Zod constrained-collection validation can exhaust memory. Bash uses Zod for operator-provided server configuration and ShellCheck output. Fjord bounds operator settings and disables ShellCheck, so repository content does not enter these affected validation paths with the default profile.
- [GHSA-g3hv-75wg-m249](https://github.com/advisories/GHSA-g3hv-75wg-m249): minimatch glob amplification. Bash reaches minimatch through editorconfig inside its shfmt formatter. Fjord disables shfmt and does not expose formatting requests, so this path is unavailable with the default profile.

Neither dependency has a compatible same-major fix in this bundle's upstream dependency constraints.
Major-version overrides would change upstream runtime contracts, so the bundle retains upstream versions and the disabled features.
Operators who replace the Bash profile and enable these tools must reassess these advisories.
