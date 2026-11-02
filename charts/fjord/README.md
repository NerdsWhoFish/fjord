# Fjord Helm chart

Run Fjord on Kubernetes with its bundled, isolated language servers.

Read the [node prerequisites and installation guide](https://github.com/NerdsWhoFish/fjord/blob/main/docs/kubernetes.md) first. Kubernetes 1.33+ and a runtime supporting pod user namespaces are required. The chart intentionally does not disable sandbox protections or install host profiles.

```sh
helm repo add nerdswhofish https://charts.nerdswhofish.com
helm repo update
helm upgrade --install fjord nerdswhofish/fjord \
  --namespace fjord --create-namespace -f values.yaml
```

Create a GitHub token Secret separately; never put tokens in Helm values. Default access is through `kubectl -n fjord port-forward svc/fjord-fjord 4727:4727` at `http://localhost:4727`.

| Value | Default | Use |
| --- | --- | --- |
| `publicURL` | `http://localhost:4727` | Exact browser-facing origin |
| `github.existingSecret` / `tokenKey` | `fjord-github` / `token` | GitHub token Secret |
| `image.tag` / `digest` | Chart app version / empty | Pin an image; digest takes precedence |
| `sandbox.seccompProfile` | `profiles/fjord.json` | Profile installed on each eligible node |
| `ingress.enabled` / `className` | `false` / empty | Standard Kubernetes Ingress |
| `ingress.tlsSecretName` | empty | TLS Secret required for HTTPS ingress |
| `languageServers.existingConfigMap` | empty | Operator language-server configuration |
| `goModuleCache.existingClaim` | empty | Prepopulated, read-only Go module cache PVC |
| `telemetry.otlpEndpoint` | empty | Backend OTLP HTTP/protobuf collector |
| `telemetry.existingSecret` / `headersKey` | empty / `headers` | Backend OTLP credentials |
| `telemetry.faroURL` | empty | Browser RUM collector through Fjord's relay |
| `temporaryStorage.sizeLimit` | `6Gi` | Ephemeral workspace storage limit |

See [values.yaml](values.yaml) for scheduling, resource, and image settings. Review sessions stay in memory: the chart fixes the deployment at one replica with `Recreate` upgrades. A PVC does not preserve sessions.
