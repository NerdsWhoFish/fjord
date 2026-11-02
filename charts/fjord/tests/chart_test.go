package chart_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func helm(t *testing.T, args ...string) ([]byte, error) {
	t.Helper()
	if os.Getenv("FJORD_TEST_HELM") != "1" {
		t.Skip("set FJORD_TEST_HELM=1 to validate the Helm chart")
	}
	if _, err := exec.LookPath("helm"); err != nil {
		t.Fatal("FJORD_TEST_HELM requires helm on PATH")
	}
	return exec.Command("helm", args...).CombinedOutput()
}

func render(t *testing.T, args ...string) map[string]map[string]any {
	t.Helper()
	output, err := helm(t, append([]string{"template", "review", "..", "--namespace", "review-test"}, args...)...)
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, output)
	}
	documents := make(map[string]map[string]any)
	decoder := yaml.NewDecoder(bytes.NewReader(output))
	for {
		var document map[string]any
		if err := decoder.Decode(&document); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if kind, ok := document["kind"].(string); ok {
			if _, exists := documents[kind]; exists {
				t.Fatalf("unexpected duplicate kind %s", kind)
			}
			documents[kind] = document
		}
	}
	return documents
}

func field(t *testing.T, value any, path ...any) any {
	t.Helper()
	for _, segment := range path {
		switch key := segment.(type) {
		case string:
			object, ok := value.(map[string]any)
			if !ok {
				t.Fatalf("expected object at %v, got %T", path, value)
			}
			value = object[key]
		case int:
			array, ok := value.([]any)
			if !ok || key >= len(array) {
				t.Fatalf("expected array index %d at %v", key, path)
			}
			value = array[key]
		default:
			t.Fatalf("invalid path segment %v", segment)
		}
	}
	return value
}

func assertField(t *testing.T, object any, expected any, path ...any) {
	t.Helper()
	if actual := field(t, object, path...); !reflect.DeepEqual(actual, expected) {
		t.Errorf("%v = %#v, want %#v", path, actual, expected)
	}
}

func TestDefaultSandboxAndRouting(t *testing.T) {
	documents := render(t)
	if len(documents) != 2 {
		t.Fatalf("default chart should only create a Deployment and Service: %v", documents)
	}
	deployment, service := documents["Deployment"], documents["Service"]
	assertField(t, deployment, 1, "spec", "replicas")
	assertField(t, deployment, "Recreate", "spec", "strategy", "type")
	pod := field(t, deployment, "spec", "template", "spec")
	assertField(t, pod, false, "hostUsers")
	assertField(t, pod, false, "automountServiceAccountToken")
	assertField(t, pod, true, "securityContext", "runAsNonRoot")
	assertField(t, pod, 10001, "securityContext", "runAsUser")
	assertField(t, pod, "Localhost", "securityContext", "seccompProfile", "type")
	assertField(t, pod, "profiles/fjord.json", "securityContext", "seccompProfile", "localhostProfile")
	container := field(t, pod, "containers", 0)
	assertField(t, container, "ghcr.io/nerdswhofish/fjord:1.0.0", "image")
	assertField(t, container, false, "securityContext", "allowPrivilegeEscalation")
	assertField(t, container, true, "securityContext", "readOnlyRootFilesystem")
	assertField(t, container, "Unmasked", "securityContext", "procMount")
	assertField(t, container, []any{"ALL"}, "securityContext", "capabilities", "drop")
	assertField(t, container, "localhost:4727", "readinessProbe", "httpGet", "httpHeaders", 0, "value")
	assertField(t, container, "fjord-github", "env", 0, "valueFrom", "secretKeyRef", "name")
	assertField(t, service, field(t, deployment, "spec", "selector", "matchLabels"), "spec", "selector")
	assertField(t, service, "http", "spec", "ports", 0, "targetPort")
}

func TestIngressConfigAndReadOnlyCache(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	documents := render(t,
		"--set", "publicURL=https://review.example.com,ingress.enabled=true,ingress.className=nginx,ingress.tlsSecretName=review-tls",
		"--set", "image.digest="+digest+",image.tag=ignored",
		"--set", "languageServers.existingConfigMap=profiles,languageServers.configKey=custom.yaml,goModuleCache.existingClaim=modules",
		"--set", "telemetry.otlpEndpoint=http://collector:4318,telemetry.existingSecret=telemetry,telemetry.faroURL=https://faro.example/collect/app",
	)
	ingress := documents["Ingress"]
	assertField(t, ingress, "review.example.com", "spec", "rules", 0, "host")
	assertField(t, ingress, "review-tls", "spec", "tls", 0, "secretName")
	assertField(t, ingress, "review-fjord", "spec", "rules", 0, "http", "paths", 0, "backend", "service", "name")
	pod := field(t, documents["Deployment"], "spec", "template", "spec")
	container := field(t, pod, "containers", 0)
	assertField(t, container, "ghcr.io/nerdswhofish/fjord@"+digest, "image")
	assertField(t, container, "review.example.com", "livenessProbe", "httpGet", "httpHeaders", 0, "value")
	assertField(t, container, true, "volumeMounts", 2, "readOnly")
	assertField(t, container, true, "volumeMounts", 3, "readOnly")
	assertField(t, pod, "custom.yaml", "volumes", 2, "configMap", "items", 0, "key")
	assertField(t, pod, true, "volumes", 3, "persistentVolumeClaim", "readOnly")
	assertField(t, pod, "modules", "volumes", 3, "persistentVolumeClaim", "claimName")
	env := field(t, container, "env").([]any)
	foundHeaders := false
	for _, entry := range env {
		if field(t, entry, "name") == "OTEL_EXPORTER_OTLP_HEADERS" {
			foundHeaders = true
			assertField(t, entry, "telemetry", "valueFrom", "secretKeyRef", "name")
			assertField(t, entry, nil, "value")
		}
	}
	if !foundHeaders {
		t.Fatal("missing backend telemetry credentials reference")
	}
}

func TestRejectUnsafeOrUnusableValues(t *testing.T) {
	for name, values := range map[string]string{
		"empty credential reference": "github.existingSecret=",
		"empty seccomp profile":      "sandbox.seccompProfile=",
		"absolute seccomp path":      "sandbox.seccompProfile=/profiles/fjord.json",
		"traversal seccomp path":     "sandbox.seccompProfile=../fjord.json",
		"multiple replicas":          "replicaCount=2",
		"unconfined sandbox":         "sandbox.type=Unconfined",
		"origin with credentials":    "publicURL=https://user:secret@example.com",
		"origin with path":           "publicURL=https://example.com/path",
		"origin with query":          "publicURL=https://example.com?token=private",
		"origin with fragment":       "publicURL=https://example.com#fragment",
		"https ingress without TLS":  "publicURL=https://example.com,ingress.enabled=true",
		"ingress with port":          "publicURL=http://example.com:4727,ingress.enabled=true",
		"headers without endpoint":   "telemetry.existingSecret=telemetry",
		"invalid digest":             "image.digest=sha256:not-a-digest",
	} {
		t.Run(name, func(t *testing.T) {
			if output, err := helm(t, "template", "review", "..", "--set", values); err == nil {
				t.Fatalf("accepted invalid values %q\n%s", values, output)
			}
		})
	}
}

func TestPackagedChart(t *testing.T) {
	dir := t.TempDir()
	if output, err := helm(t, "lint", "..", "--strict"); err != nil {
		t.Fatalf("helm lint: %v\n%s", err, output)
	}
	if output, err := helm(t, "package", "..", "--destination", dir); err != nil {
		t.Fatalf("helm package: %v\n%s", err, output)
	}
	packages, err := filepath.Glob(filepath.Join(dir, "*.tgz"))
	if err != nil || len(packages) != 1 {
		t.Fatalf("expected one chart package, got %v: %v", packages, err)
	}
	file, err := os.Open(packages[0])
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(header.Name, "fjord/tests/") {
			t.Fatalf("test source included in published archive: %s", header.Name)
		}
	}
	if output, err := helm(t, "template", "review", packages[0]); err != nil {
		t.Fatalf("packaged chart cannot render: %v\n%s", err, output)
	}
}
