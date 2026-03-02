{{/*
Expand the name of the chart.
*/}}
{{- define "elydora.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "elydora.fullname" -}}
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
{{- define "elydora.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels.
*/}}
{{- define "elydora.labels" -}}
helm.sh/chart: {{ include "elydora.chart" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Selector labels.
*/}}
{{- define "elydora.selectorLabels" -}}
app.kubernetes.io/name: {{ include "elydora.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Namespace.
*/}}
{{- define "elydora.namespace" -}}
{{- .Values.namespace | default "elydora" }}
{{- end }}

{{/*
Database URL.
*/}}
{{- define "elydora.databaseUrl" -}}
postgresql://{{ .Values.postgres.user }}:{{ .Values.postgres.password }}@{{ .Values.postgres.host }}:{{ .Values.postgres.port }}/{{ .Values.postgres.database }}
{{- end }}

{{/*
Redis URL.
*/}}
{{- define "elydora.redisUrl" -}}
redis://{{ .Values.redis.host }}:{{ .Values.redis.port }}
{{- end }}

{{/*
MinIO endpoint.
*/}}
{{- define "elydora.minioEndpoint" -}}
http://{{ .Values.minio.host }}:{{ .Values.minio.port }}
{{- end }}
