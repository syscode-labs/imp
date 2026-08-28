{{- define "impsandbox.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "impsandbox.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s" (include "impsandbox.name" .) | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}

{{- define "impsandbox.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{ include "impsandbox.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/part-of: imp
{{- end }}

{{- define "impsandbox.selectorLabels" -}}
app.kubernetes.io/name: {{ include "impsandbox.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "impsandbox.serviceAccountName" -}}
{{- if .Values.rbac.create }}
{{- include "impsandbox.fullname" . }}-controller
{{- else }}
{{- required "rbac.create=false requires an explicit serviceAccountName" .Values.serviceAccountName }}
{{- end }}
{{- end }}

{{- define "impsandbox.image" -}}
{{- $tag := default .Chart.AppVersion .Values.sandbox.image.tag -}}
{{- printf "%s:%s" .Values.sandbox.image.repository $tag -}}
{{- end }}

{{- define "impsandbox.gateway.image" -}}
{{- $tag := default .Chart.AppVersion .Values.gateway.image.tag -}}
{{- printf "%s:%s" .Values.gateway.image.repository $tag -}}
{{- end }}
