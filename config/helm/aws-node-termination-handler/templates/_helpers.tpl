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
{{- define "aws-node-termination-handler.fullnameWindows" -}}
{{- include "aws-node-termination-handler.fullname" . | trunc 59 | trimSuffix "-" | printf "%s-win" -}}
{{- end -}}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "aws-node-termination-handler.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Common labels
*/}}
{{- define "aws-node-termination-handler.labels" -}}
{{ include "aws-node-termination-handler.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/part-of: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ include "aws-node-termination-handler.chart" . }}
{{- with .Values.customLabels }}
{{ toYaml . }}
{{- end }}
{{- end -}}

{{/*
Deployment labels
*/}}
{{- define "aws-node-termination-handler.labelsDeployment" -}}
{{ include "aws-node-termination-handler.labels" . }}
app.kubernetes.io/component: deployment
{{- end -}}

{{/*
Daemonset labels
*/}}
{{- define "aws-node-termination-handler.labelsDaemonset" -}}
{{ include "aws-node-termination-handler.labels" . }}
app.kubernetes.io/component: daemonset
{{- end -}}

{{/*
Selector labels
*/}}
{{- define "aws-node-termination-handler.selectorLabels" -}}
app.kubernetes.io/name: {{ include "aws-node-termination-handler.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
Selector labels for the deployment
*/}}
{{- define "aws-node-termination-handler.selectorLabelsDeployment" -}}
{{ include "aws-node-termination-handler.selectorLabels" . }}
app.kubernetes.io/component: deployment
{{- end -}}

{{/*
Selector labels for the daemonset
*/}}
{{- define "aws-node-termination-handler.selectorLabelsDaemonset" -}}
{{ include "aws-node-termination-handler.selectorLabels" . }}
app.kubernetes.io/component: daemonset
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
The image to use
*/}}
{{- define "aws-node-termination-handler.image" -}}
{{- printf "%s:%s" .Values.image.repository (default (printf "v%s" .Chart.AppVersion) .Values.image.tag) }}
{{- end }}

{{/* Get PodDisruptionBudget API Version */}}
{{- define "aws-node-termination-handler.pdb.apiVersion" -}}
  {{- if and (.Capabilities.APIVersions.Has "policy/v1") (semverCompare ">= 1.21-0" .Capabilities.KubeVersion.Version) -}}
      {{- print "policy/v1" -}}
  {{- else -}}
    {{- print "policy/v1beta1" -}}
  {{- end -}}
{{- end -}}

{{/*
Whether the IMDS event controller is used: IMDS mode with imdsEventController.enabled
*/}}
{{- define "aws-node-termination-handler.imdsEventController" -}}
{{- if and (not .Values.enableSqsTerminationDraining) .Values.imdsEventController.enabled -}}
true
{{- end -}}
{{- end -}}

{{/*
The name of the service account of the DaemonSet when the IMDS event controller is used
*/}}
{{- define "aws-node-termination-handler.nodeServiceAccountName" -}}
{{- if .Values.imdsEventController.nodeServiceAccount.name -}}
{{ .Values.imdsEventController.nodeServiceAccount.name }}
{{- else if .Values.serviceAccount.create -}}
{{ include "aws-node-termination-handler.fullname" . | trunc 58 | trimSuffix "-" }}-node
{{- else -}}
{{ fail "imdsEventController.nodeServiceAccount.name is required when serviceAccount.create is false" }}
{{- end -}}
{{- end -}}

{{/*
The service account of the DaemonSet
*/}}
{{- define "aws-node-termination-handler.daemonsetServiceAccountName" -}}
{{- if include "aws-node-termination-handler.imdsEventController" . -}}
{{ include "aws-node-termination-handler.nodeServiceAccountName" . }}
{{- else -}}
{{ include "aws-node-termination-handler.serviceAccountName" . }}
{{- end -}}
{{- end -}}

{{/*
Validate the IMDS event controller values
*/}}
{{- define "aws-node-termination-handler.validateIMDSEventController" -}}
{{- if include "aws-node-termination-handler.imdsEventController" . -}}
{{- if semverCompare "<1.30-0" .Capabilities.KubeVersion.Version -}}
{{- fail "imdsEventController.enabled=true requires Kubernetes 1.30 or later (ValidatingAdmissionPolicy and service account tokens bound to a node). Setting imdsEventController.enabled=false lets the DaemonSet cordon and drain its node itself, with a service account that can cordon and drain every node in the cluster." -}}
{{- end -}}
{{- if not .Values.rbac.create -}}
{{- fail "imdsEventController.enabled=true requires rbac.create=true: the ValidatingAdmissionPolicy that limits the DaemonSet to its own node is created with the RBAC resources" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Selector labels for the IMDS event controller
*/}}
{{- define "aws-node-termination-handler.selectorLabelsIMDSEventController" -}}
{{ include "aws-node-termination-handler.selectorLabels" . }}
app.kubernetes.io/component: imds-event-controller
{{- end -}}

{{/*
Environment variables that configure how nodes are cordoned and drained, shared by the DaemonSet and the IMDS event
controller
*/}}
{{- define "aws-node-termination-handler.drainEnv" -}}
- name: LOG_LEVEL
  value: {{ .Values.logLevel | quote }}
- name: JSON_LOGGING
  value: {{ .Values.jsonLogging | quote }}
- name: LOG_FORMAT_VERSION
  value: {{ .Values.logFormatVersion | quote }}
- name: DRY_RUN
  value: {{ .Values.dryRun | quote }}
- name: CORDON_ONLY
  value: {{ .Values.cordonOnly | quote }}
- name: TAINT_NODE
  value: {{ .Values.taintNode | quote }}
- name: ENABLE_OUT_OF_SERVICE_TAINT
  value: {{ .Values.enableOutOfServiceTaint | quote }}
- name: EXCLUDE_FROM_LOAD_BALANCERS
  value: {{ .Values.excludeFromLoadBalancers | quote }}
- name: DELETE_LOCAL_DATA
  value: {{ .Values.deleteLocalData | quote }}
- name: IGNORE_DAEMON_SETS
  value: {{ .Values.ignoreDaemonSets | quote }}
- name: POD_TERMINATION_GRACE_PERIOD
  value: {{ .Values.podTerminationGracePeriod | quote }}
- name: NODE_TERMINATION_GRACE_PERIOD
  value: {{ .Values.nodeTerminationGracePeriod | quote }}
- name: EMIT_KUBERNETES_EVENTS
  value: {{ .Values.emitKubernetesEvents | quote }}
{{- with .Values.kubernetesEventsExtraAnnotations }}
- name: KUBERNETES_EVENTS_EXTRA_ANNOTATIONS
  value: {{ . | quote }}
{{- end }}
{{- if or .Values.webhookURL .Values.webhookURLSecretName }}
- name: WEBHOOK_URL
{{- if .Values.webhookURLSecretName }}
  valueFrom:
    secretKeyRef:
      name: {{ .Values.webhookURLSecretName }}
      key: webhookurl
{{- else }}
  value: {{ .Values.webhookURL | quote }}
{{- end }}
{{- end }}
{{- with .Values.webhookHeaders }}
- name: WEBHOOK_HEADERS
  value: {{ . | quote }}
{{- end }}
{{- with .Values.webhookProxy }}
- name: WEBHOOK_PROXY
  value: {{ . | quote }}
{{- end }}
{{- if and .Values.webhookTemplateConfigMapName .Values.webhookTemplateConfigMapKey }}
- name: WEBHOOK_TEMPLATE_FILE
  value: {{ print "/config/" .Values.webhookTemplateConfigMapKey | quote }}
{{- else if .Values.webhookTemplate }}
- name: WEBHOOK_TEMPLATE
  value: {{ .Values.webhookTemplate | quote }}
{{- end }}
- name: ENABLE_REBALANCE_DRAINING
  value: {{ .Values.enableRebalanceDraining | quote }}
{{- end -}}
