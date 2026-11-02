# Kubernetes

The [Fjord Helm chart](../charts/fjord) deploys the public container image, its bundled language servers, and a ClusterIP service. Ingress is optional. Fjord has no login: use a trusted network or an authenticating proxy.

## Prepare the nodes

Use Kubernetes 1.33+ with pod user namespaces and `procMount: Unmasked` support enabled in the kubelet and container runtime. The host kernel must permit nested unprivileged user namespaces (`user.max_user_namespaces` must be nonzero). A version number alone does not guarantee runtime support.

Install a scoped seccomp profile at `/var/lib/kubelet/seccomp/profiles/fjord.json` on every eligible node, or adjust `sandbox.seccompProfile` relative to the kubelet seccomp directory. Start with your container runtime's current default profile and allow Bubblewrap's `clone`, `unshare`, `mount`, `umount2`, `pivot_root`, and `sethostname` operations. Revalidate after runtime upgrades. On Talos, use `machine.seccompProfiles`.

The chart runs as UID/GID `10001`, drops all capabilities, prohibits privilege escalation, uses a read-only root filesystem, and sets `hostUsers: false`. Missing node prerequisites should fail deployment or navigation. Do not work around them with privileged containers, unconfined seccomp, or disabled language-server isolation.

Your admission policy must allow the scoped Localhost profile, pod user namespaces, and unmasked procfs. Restrict eligible nodes with `nodeSelector` if only some nodes have the profile. The chart does not alter node configuration or weaken namespace admission policies.

## Install

Create namespace `fjord` and a Secret named `fjord-github` with a `token` key using your secret manager. The GitHub identity needs access to repositories being reviewed and permission to submit reviews. Keep token values out of Helm values, command arguments, and Git.

```sh
helm repo add nerdswhofish https://charts.nerdswhofish.com
helm repo update
helm upgrade --install fjord nerdswhofish/fjord \
  --namespace fjord --create-namespace -f values.yaml
```

For local access, an empty values file uses `http://localhost:4727`:

```sh
kubectl -n fjord port-forward svc/fjord-fjord 4727:4727
```

For a reverse proxy:

```yaml
publicURL: https://review.example.com
github:
  existingSecret: fjord-github
ingress:
  enabled: true
  className: nginx
  tlsSecretName: fjord-tls
telemetry:
  otlpEndpoint: http://otel-collector.observability.svc.cluster.local:4318
  faroURL: https://your-faro-collector.example/collect/your-app
```

Provision DNS, TLS, and access restrictions separately. The proxy must preserve the configured Host header. Probes send that same header automatically.

## Configuration and upgrades

- For authenticated OTLP, use `telemetry.existingSecret` with a `headers` key in OpenTelemetry's `Header-Name=value` format. Only the backend receives the headers. Verify traces, correlated logs, browser RUM, and exporter errors after deployment.
- Mount operator profiles using `languageServers.existingConfigMap`; its default key is `language-servers.yaml`. For additional executables, build a derived image and grant only the required sandbox runtime paths.
- For private or offline Go dependencies, set `goModuleCache.existingClaim` to a prepopulated PVC containing the `cache/download` tree. Fjord mounts it read-only and will not fall back to public downloads. The PVC must be readable by UID/GID `10001`.
- Restart the Deployment after changing credentials or profile ConfigMaps. Fjord loads profiles at startup.
- One replica owns review/MCP sessions. `Recreate` upgrades briefly interrupt service and discard server-side sessions. Browser drafts remain in the browser; a PVC does not persist sessions.

Use `image.digest` to pin an immutable image. Otherwise, the chart selects its `appVersion` tag. Inspect [chart values](../charts/fjord/values.yaml) for resource limits and scheduling.

Validate a local chart with:

```sh
helm lint charts/fjord --strict
FJORD_TEST_HELM=1 GOWORK=off go test ./charts/fjord/tests
```
