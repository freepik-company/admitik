{{/*
Expand the name of the chart.
*/}}
{{- define "admitik.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "admitik.fullname" -}}
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
{{- define "admitik.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "admitik.labels" -}}
helm.sh/chart: {{ include "admitik.chart" . }}
{{ include "admitik.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}

# Following labels are included to avoid chicken-egg scenarios
{{ include "admitik.sensitiveLabels" . }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "admitik.selectorLabels" -}}
app.kubernetes.io/name: {{ include "admitik.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Sensitive labels
*/}}
{{- define "admitik.sensitiveLabels" -}}
admitik.dev/ignore-admission: "true"
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "admitik.serviceAccountName" -}}
{{- if .Values.controller.serviceAccount.create }}
{{- default (include "admitik.fullname" .) .Values.controller.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.controller.serviceAccount.name }}
{{- end }}
{{- end }}
