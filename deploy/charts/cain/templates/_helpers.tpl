{{/*
Expand the name of the chart.
*/}}
{{- define "cain.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "cain.fullname" -}}
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

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "cain.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "cain.labels" -}}
helm.sh/chart: {{ include "cain.chart" . }}
{{ include "cain.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "cain.selectorLabels" -}}
app.kubernetes.io/name: {{ include "cain.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "cain.serviceAccountName" -}}
{{- default (include "cain.fullname" .) .Values.serviceAccount.name }}
{{- end }}

{{/*
Certificate helpers
*/}}
{{- define "cain.selfSignedIssuer" -}}
{{ printf "%s-selfsign" (include "cain.fullname" .) }}
{{- end -}}

{{- define "cain.rootCAIssuer" -}}
{{ printf "%s-ca" (include "cain.fullname" .) }}
{{- end -}}

{{- define "cain.rootCACertificate" -}}
{{ printf "%s-ca" (include "cain.fullname" .) }}
{{- end -}}

{{- define "cain.servingCertificate" -}}
{{ printf "%s-tls" (include "cain.fullname" .) }}
{{- end -}}
