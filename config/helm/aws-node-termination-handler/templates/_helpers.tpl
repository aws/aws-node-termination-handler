{{/* vim: set filetype=mustache: */}}
{{/*
Expand the name of the chart.
*/}}
{{- define "aws-node-termination-handler.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "aws-node-termination-handler.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Equivalent to "aws-node-termination-handler.fullname" except that "-win" indicator is appended to the end.
Name will not exceed 63 characters.
*/}}
{{- define "aws-node-termination-handler.fullname.windows" -}}
{{- include "aws-node-termination-handler.fullname" . | trunc 59 | trimSuffix "-" | printf "%s-win" -}}
{{- end -}}

{{/*
Common labels
*/}}
{{- define "aws-node-termination-handler.labels" -}}
helm.sh/chart: {{ include "aws-node-termination-handler.chart" . }}
{{ include "aws-node-termination-handler.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
Selector labels
*/}}
{{- define "aws-node-termination-handler.selectorLabels" -}}
app.kubernetes.io/name: {{ include "aws-node-termination-handler.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "aws-node-termination-handler.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create the name of the service account to use
*/}}
{{- define "aws-node-termination-handler.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
    {{ default (include "aws-node-termination-handler.fullname" .) .Values.serviceAccount.name }}
{{- else -}}
    {{ default "default" .Values.serviceAccount.name }}
{{- end -}}
{{- end -}}

{{/*
Get the default node selector term prefix.
*/}}
{{- define "aws-node-termination-handler.defaultNodeSelectorTermsPrefix" -}}
kubernetes.io
{{- end -}}

{{/*
Get the default node selector OS term.
*/}}
{{- define "aws-node-termination-handler.defaultNodeSelectorTermsOs" -}}
    {{- list (include "aws-node-termination-handler.defaultNodeSelectorTermsPrefix" .) "os" | join "/" -}}
{{- end -}}

{{/*
Get the default node selector Arch term.
*/}}
{{- define "aws-node-termination-handler.defaultNodeSelectorTermsArch" -}}
    {{- list (include "aws-node-termination-handler.defaultNodeSelectorTermsPrefix" .) "arch" | join "/" -}}
{{- end -}}

{{/*
Get the node selector OS term.
*/}}
{{- define "aws-node-termination-handler.nodeSelectorTermsOs" -}}
    {{- or .Values.nodeSelectorTermsOs (include "aws-node-termination-handler.defaultNodeSelectorTermsOs" .) -}}
{{- end -}}

{{/*
Get the node selector Arch term.
*/}}
{{- define "aws-node-termination-handler.nodeSelectorTermsArch" -}}
    {{- or .Values.nodeSelectorTermsArch (include "aws-node-termination-handler.defaultNodeSelectorTermsArch" .) -}}
{{- end -}}
