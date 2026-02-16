{{/*
Expand the name of the chart.
*/}}
{{- define "dnsmasq-controller.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "dnsmasq-controller.fullname" -}}
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
{{- define "dnsmasq-controller.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels (without role — each DaemonSet adds role inline).
*/}}
{{- define "dnsmasq-controller.labels" -}}
helm.sh/chart: {{ include "dnsmasq-controller.chart" . }}
{{ include "dnsmasq-controller.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels (shared between DNS and DHCP — no role here).
*/}}
{{- define "dnsmasq-controller.selectorLabels" -}}
app.kubernetes.io/name: {{ include "dnsmasq-controller.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use.
*/}}
{{- define "dnsmasq-controller.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "dnsmasq-controller.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Resolve the image tag — defaults to Chart.AppVersion.
*/}}
{{- define "dnsmasq-controller.imageTag" -}}
{{- default .Chart.AppVersion .Values.image.tag }}
{{- end }}
