# Offline language-server schemas

Fjord embeds these schemas in its binary so native and container installations validate common configuration without fetching schemas at review time.
`Materialize` writes a private copy under the session runtime directory.
`Expand` replaces `fjord-schema://<name>` in settings and initialization options with a local `file://` URI.
The session exposes the bundle read-only to its language server and removes it with the session.

## Sources

Definitions and SchemaStore files are unmodified upstream artifacts.
`kubernetes.json` is generated from the definitions' `x-kubernetes-group-version-kind` metadata with explicit `apiVersion` and `kind` constraints.
The upstream `all.json` includes helper objects and causes ambiguous validation for valid resources, so it is unsuitable for manifest validation.
Their SHA-256 hashes are verified by the bundle tests.

| Asset | Version/source | SHA-256 |
| --- | --- | --- |
| `kubernetes.json` | Generated Kubernetes v1.37.0 resource union | `3a2b7a214f7f5be3cd47d2ac7dd33664d8e3c96ad896672358196100fe3e069a` |
| `_definitions.json` | Kubernetes v1.37.0, `v1.37.0-standalone-strict/_definitions.json` | `4755c5231489317fe4be800f1d0b2427c06b24da8328dfc4b0c565946683700e` |
| `github-workflow.json` | SchemaStore `src/schemas/json/github-workflow.json` | `d10c9f4656e1bd5bc6727e9b35080e017dc167154726fca93da33c7a6bd1c4f3` |
| `tsconfig.json` | SchemaStore `src/schemas/json/tsconfig.json` | `9e9599c2e6021574483e3a802eb075c872be1f5b4185ad45da03d33edce3c2b1` |

Kubernetes source: [yannh/kubernetes-json-schema at e0c72a1bc5f409d1685c0e1323460c8a98cd7459](https://github.com/yannh/kubernetes-json-schema/tree/e0c72a1bc5f409d1685c0e1323460c8a98cd7459/v1.37.0-standalone-strict).
SchemaStore source: [SchemaStore/schemastore at 1f9ebc8379f842319663f776f9767fdf6ca5e4a5](https://github.com/SchemaStore/schemastore/tree/1f9ebc8379f842319663f776f9767fdf6ca5e4a5/src/schemas/json).
Both sources use Apache-2.0; their upstream license files are bundled alongside the assets.
`LICENSE-schemastore` contains the full Apache-2.0 license text.

## Scope and limitations

Default YAML associations apply GitHub Actions validation only to `.github/workflows/*.yml` and `.github/workflows/*.yaml`.
Kubernetes validation applies to YAML under directories named `k8s` or `kubernetes`, avoiding blanket Kubernetes validation of application settings and CI files.
Put rendered manifests there; Helm templates are not valid YAML before rendering.
CRDs require operator-supplied schemas for their custom resource instances.
Kubernetes schema versions are pinned, so clusters using different API versions may require an operator override.

The JSON profile associates the TypeScript configuration schema with `tsconfig.json`, `tsconfig.*.json`, and `jsconfig.json`.
Arbitrary `$schema` URLs and YAML modelines cannot enable network access in the isolated server.

Operators can replace the named default profile in their language-server configuration to change associations or point at a mounted local schema.
Include that schema's directory in the profile's `runtimePaths`.
Built-in URIs remain available as `fjord-schema://kubernetes.json`, `fjord-schema://github-workflow.json`, and `fjord-schema://tsconfig.json`.

## Updating

Choose an immutable upstream commit, download the same source paths, preserve the license files, run `go generate ./internal/schemas`, and update the hashes and version recorded here and in `bundle_test.go`.
Run `go test ./internal/schemas`.
The reference-resolution test rejects missing external dependencies and remote references; a new dependency must be bundled before an update can pass.
