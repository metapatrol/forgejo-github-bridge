{{/*
Expand the name of the chart.
*/}}
{{- define "bridge.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "bridge.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{- define "bridge.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "bridge.labels" -}}
helm.sh/chart: {{ include "bridge.chart" . }}
{{ include "bridge.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "bridge.selectorLabels" -}}
app.kubernetes.io/name: {{ include "bridge.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "bridge.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "bridge.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{- /*
  The image tag, in three steps.

  An explicit .Values.image.tag always wins. Otherwise the chart version decides:

    0.3.0                 a released chart -> appVersion, the released image
    0.3.0+ab12cd34ef56    a Git source     -> that commit's image

  Flux appends the commit to the chart version when a HelmRelease reads a chart
  from a GitRepository with `reconcileStrategy: Revision`. CI pushes an image
  under that same twelve-character tag so this resolves.
*/ -}}
{{- define "bridge.image" -}}
{{- $tag := .Values.image.tag -}}
{{- if not $tag -}}
  {{- $parts := splitList "+" .Chart.Version -}}
  {{- if gt (len $parts) 1 -}}
    {{- $tag = last $parts -}}
  {{- else -}}
    {{- $tag = .Chart.AppVersion -}}
  {{- end -}}
{{- end -}}
{{- printf "%s:%s" .Values.image.repository $tag }}
{{- end }}

{{- /*
  The GitHub App ID as a string of digits. Helm carries YAML numbers as
  float64, and a large one quoted as is renders as 1.2345678e+07, which the
  bridge rightly refuses.
*/ -}}
{{- define "bridge.appID" -}}
{{- $id := required "github.appID is required" .Values.github.appID -}}
{{- if kindIs "string" $id -}}
{{- $id -}}
{{- else -}}
{{- $id | int64 | toString -}}
{{- end -}}
{{- end }}

{{- /*
  The configuration file. Rendered from values rather than taken verbatim, so
  a typo in a key fails here, in the values' schema of one place, instead of
  in the bridge at startup.
*/ -}}
{{- define "bridge.configFile" -}}
repositories:
{{- range $i, $r := .Values.repositories }}
  - github: {{ required (printf "repositories[%d].github is required" $i) $r.github | quote }}
    forgejo: {{ required (printf "repositories[%d].forgejo is required" $i) $r.forgejo | quote }}
    {{- with $r.publishLabel }}
    publishLabel: {{ . | quote }}
    {{- end }}
    {{- with $r.importLabel }}
    importLabel: {{ . | quote }}
    {{- end }}
    {{- if hasKey $r "internalPrefix" }}
    internalPrefix: {{ $r.internalPrefix | quote }}
    {{- end }}
    {{- with $r.importSince }}
    importSince: {{ . | quote }}
    {{- end }}
    {{- with $r.contrib }}
    contrib: {{ . | quote }}
    {{- end }}
    {{- with $r.targetBranches }}
    targetBranches:
      {{- range . }}
      - {{ . | quote }}
      {{- end }}
    {{- end }}
    {{- if $r.rebaseRequests }}
    rebaseRequests: true
    {{- end }}
    {{- with $r.ciPaths }}
    ciPaths:
      {{- range . }}
      - {{ . | quote }}
      {{- end }}
    {{- end }}
    {{- with $r.messages }}
    messages:
      {{- toYaml . | nindent 6 }}
    {{- end }}
{{- end }}
{{- with .Values.messages }}
messages:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- with .Values.userMapping }}
userMapping:
  {{- range $fj, $gh := . }}
  {{ $fj | quote }}: {{ $gh | quote }}
  {{- end }}
{{- end }}
{{- end }}

{{- /*
  What the chart refuses to render. A bridge missing any of these starts and
  then fails, or does nothing, and says so only in a log nobody is reading.
*/ -}}
{{- define "bridge.validate" -}}
{{- if not .Values.database.existingSecret -}}
{{- fail "database.existingSecret is required: the Secret holding the PostgreSQL DSN, e.g. CloudNativePG's <cluster>-app" -}}
{{- end -}}
{{- if not .Values.database.existingSecretKey -}}
{{- fail "database.existingSecretKey must not be empty" -}}
{{- end -}}
{{- if lt (int .Values.database.maxConnections) 2 -}}
{{- fail "database.maxConnections must be at least 2: the leader lock holds one" -}}
{{- end -}}
{{- if not .Values.github.existingSecret -}}
{{- fail "github.existingSecret is required: the Secret holding the GitHub App's private key" -}}
{{- end -}}
{{- if not .Values.forgejo.url -}}
{{- fail "forgejo.url is required" -}}
{{- end -}}
{{- if not .Values.forgejo.existingSecret -}}
{{- fail "forgejo.existingSecret is required: the Secret holding the github-bridge account's token" -}}
{{- end -}}
{{- if not .Values.repositories -}}
{{- fail "repositories must name at least one bridged repository" -}}
{{- end -}}
{{- range .Values.repositories -}}
{{- if and .targetBranches (not $.Values.forgejo.contribExistingSecret) -}}
{{- fail (printf "forgejo.contribExistingSecret is required: %s imports pull requests" .github) -}}
{{- end -}}
{{- if and .contrib (not .targetBranches) -}}
{{- fail (printf "repositories: %s sets contrib but no targetBranches" .github) -}}
{{- end -}}
{{- end -}}
{{- $_ := include "bridge.appID" . -}}
{{- end }}
