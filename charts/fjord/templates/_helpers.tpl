{{- define "fjord.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "fjord.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name (include "fjord.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "fjord.selectorLabels" -}}
app.kubernetes.io/name: {{ include "fjord.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "fjord.labels" -}}
{{ include "fjord.selectorLabels" . }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "fjord.image" -}}
{{- if .Values.image.digest -}}
{{- printf "%s@%s" .Values.image.repository .Values.image.digest -}}
{{- else -}}
{{- printf "%s:%s" .Values.image.repository (default .Chart.AppVersion .Values.image.tag) -}}
{{- end -}}
{{- end -}}

{{- define "fjord.validate" -}}
{{- if and (not .Values.publicMode) (not .Values.github.existingSecret) -}}
{{- fail "github.existingSecret is required unless publicMode is enabled" -}}
{{- end -}}
{{- if and .Values.publicMode .Values.goModuleCache.existingClaim -}}
{{- fail "publicMode must not mount a shared Go module cache that could contain private dependencies" -}}
{{- end -}}
{{- if and .Values.publicMode .Values.languageServers.existingConfigMap -}}
{{- fail "publicMode uses bundled language servers and does not allow custom runtime mounts" -}}
{{- end -}}
{{- $url := urlParse .Values.publicURL -}}
{{- if or (not (has $url.scheme (list "http" "https"))) (not $url.host) $url.userinfo $url.query $url.fragment (and $url.path (ne $url.path "/")) -}}
{{- fail "publicURL must be an http(s) origin without credentials, query, fragment, or path" -}}
{{- end -}}
{{- if and .Values.ingress.enabled (contains ":" $url.host) -}}
{{- fail "ingress requires a DNS hostname in publicURL without an explicit port" -}}
{{- end -}}
{{- if and .Values.ingress.enabled (eq $url.scheme "https") (not .Values.ingress.tlsSecretName) -}}
{{- fail "ingress.tlsSecretName is required for an HTTPS ingress" -}}
{{- end -}}
{{- if and .Values.telemetry.existingSecret (not .Values.telemetry.otlpEndpoint) -}}
{{- fail "telemetry.otlpEndpoint is required when telemetry.existingSecret is set" -}}
{{- end -}}
{{- end -}}
