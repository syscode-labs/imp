{{/*
Expand the name of the chart.
*/}}
{{- define "imp.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "imp.fullname" -}}
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
Chart label.
*/}}
{{- define "imp.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels.
*/}}
{{- define "imp.labels" -}}
helm.sh/chart: {{ include "imp.chart" . }}
{{ include "imp.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels.
*/}}
{{- define "imp.selectorLabels" -}}
app.kubernetes.io/name: {{ include "imp.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Operator ServiceAccount name.
*/}}
{{- define "imp.operator.serviceAccountName" -}}
{{- printf "%s-operator" (include "imp.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Agent ServiceAccount name.
*/}}
{{- define "imp.agent.serviceAccountName" -}}
{{- printf "%s-agent" (include "imp.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Image tag — falls back to .Chart.AppVersion when tag is empty.
*/}}
{{- define "imp.operator.image" -}}
{{- $tag := .Values.operator.image.tag | default .Chart.AppVersion }}
{{- printf "%s:%s" .Values.operator.image.repository $tag }}
{{- end }}

{{- define "imp.agent.image" -}}
{{- $tag := .Values.agent.image.tag | default .Chart.AppVersion }}
{{- printf "%s:%s" .Values.agent.image.repository $tag }}
{{- end }}
