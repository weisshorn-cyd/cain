{{/* vim: set filetype=mustache: */}}
{{/*
Expand the name of the chart.
*/}}
{{- define "ca-injector.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create the name of the webhook service account to use
*/}}
{{- define "ca-injector.serviceAccountName" -}}
{{- printf "%s-%s" (include "ca-injector.name" .) "sa" }}
{{- end -}}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "ca-injector.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create CA injector app version
*/}}
{{- define "ca-injector.defaultTag" -}}
{{- default .Chart.AppVersion .Values.tagOverride }}
{{- end -}}

{{/*
Return valid version label
*/}}
{{- define "ca-injector.versionLabelValue" -}}
{{ regexReplaceAll "[^-A-Za-z0-9_.]" (include "ca-injector.defaultTag" .) "-" | trunc 63 | trimAll "-" | trimAll "_" | trimAll "." | quote }}
{{- end -}}

{{/*
Common labels
*/}}
{{- define "ca-injector.labels" -}}
helm.sh/chart: {{ include "ca-injector.chart" .context }}
{{ include "ca-injector.selectorLabels" (dict "context" .context "component" .component) }}
app.kubernetes.io/managed-by: {{ .context.Release.Service }}
app.kubernetes.io/version: {{ include "ca-injector.versionLabelValue" .context }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "ca-injector.selectorLabels" -}}
app.kubernetes.io/instance: {{ .context.Release.Name }}
{{- if .component }}
app.kubernetes.io/component: {{ .component }}
{{- end }}
{{- end }}
