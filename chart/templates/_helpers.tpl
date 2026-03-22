{{/*
Expand the name of the chart.
*/}}
{{- define "k8s-spark-ui-assist.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited.
If release name contains chart name it will be used as-is.
*/}}
{{- define "k8s-spark-ui-assist.fullname" -}}
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
Create chart label.
*/}}
{{- define "k8s-spark-ui-assist.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels.
*/}}
{{- define "k8s-spark-ui-assist.labels" -}}
helm.sh/chart: {{ include "k8s-spark-ui-assist.chart" . }}
{{ include "k8s-spark-ui-assist.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels.
*/}}
{{- define "k8s-spark-ui-assist.selectorLabels" -}}
app.kubernetes.io/name: {{ include "k8s-spark-ui-assist.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
ServiceAccount name.
*/}}
{{- define "k8s-spark-ui-assist.serviceAccountName" -}}
{{- if .Values.serviceAccount.name }}
{{- .Values.serviceAccount.name }}
{{- else }}
{{- include "k8s-spark-ui-assist.fullname" . }}
{{- end }}
{{- end }}

{{/*
Container image reference.
*/}}
{{- define "k8s-spark-ui-assist.image" -}}
{{- $tag := .Values.image.tag | default .Chart.AppVersion }}
{{- printf "%s:%s" .Values.image.repository $tag }}
{{- end }}

{{/*
Validate that the required Gateway API values are set.
*/}}
{{- define "k8s-spark-ui-assist.validateGateway" -}}
{{- if not .Values.httpHostname }}
{{- fail "values.httpHostname is required" }}
{{- end }}
{{- if not .Values.httpGatewayName }}
{{- fail "values.httpGatewayName is required" }}
{{- end }}
{{- if not .Values.httpGatewayNamespace }}
{{- fail "values.httpGatewayNamespace is required" }}
{{- end }}
{{- end }}
