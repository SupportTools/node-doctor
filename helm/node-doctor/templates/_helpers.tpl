{{/*
Expand the name of the chart.
*/}}
{{- define "node-doctor.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "node-doctor.fullname" -}}
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
{{- define "node-doctor.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "node-doctor.labels" -}}
helm.sh/chart: {{ include "node-doctor.chart" . }}
{{ include "node-doctor.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/component: monitoring
tier: system
{{- end }}

{{/*
Selector labels
*/}}
{{- define "node-doctor.selectorLabels" -}}
app.kubernetes.io/name: {{ include "node-doctor.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app: {{ include "node-doctor.name" . }}
component: monitoring
tier: system
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "node-doctor.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "node-doctor.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Create the name of the config map
*/}}
{{- define "node-doctor.configMapName" -}}
{{- printf "%s-config" (include "node-doctor.fullname" .) }}
{{- end }}

{{/*
Create the name of the service
*/}}
{{- define "node-doctor.serviceName" -}}
{{- printf "%s-metrics" (include "node-doctor.fullname" .) }}
{{- end }}
