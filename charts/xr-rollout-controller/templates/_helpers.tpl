{{/*
Expand the name of the chart.
*/}}
{{- define "xr-rollout-controller.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "xr-rollout-controller.fullname" -}}
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
Chart name and version as used by the chart label.
*/}}
{{- define "xr-rollout-controller.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Image tag: values.image.tag, otherwise the chart appVersion.
*/}}
{{- define "xr-rollout-controller.imageTag" -}}
{{- .Values.image.tag | default .Chart.AppVersion }}
{{- end }}

{{/*
Fully qualified image reference.
*/}}
{{- define "xr-rollout-controller.image" -}}
{{- printf "%s:%s" .Values.image.repository (include "xr-rollout-controller.imageTag" .) }}
{{- end }}

{{/*
Pull policy. An empty value uses Always for the latest tag and IfNotPresent otherwise.
*/}}
{{- define "xr-rollout-controller.imagePullPolicy" -}}
{{- if .Values.image.pullPolicy }}
{{- .Values.image.pullPolicy }}
{{- else if eq (include "xr-rollout-controller.imageTag" .) "latest" }}
{{- "Always" }}
{{- else }}
{{- "IfNotPresent" }}
{{- end }}
{{- end }}

{{/*
Common labels.
*/}}
{{- define "xr-rollout-controller.labels" -}}
helm.sh/chart: {{ include "xr-rollout-controller.chart" . }}
{{ include "xr-rollout-controller.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | replace "+" "_" | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/component: controller
{{- end }}

{{/*
Selector labels.
*/}}
{{- define "xr-rollout-controller.selectorLabels" -}}
app.kubernetes.io/name: {{ include "xr-rollout-controller.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
control-plane: controller-manager
{{- end }}

{{/*
ServiceAccount name.
*/}}
{{- define "xr-rollout-controller.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "xr-rollout-controller.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- required "serviceAccount.name is required when serviceAccount.create is false" .Values.serviceAccount.name }}
{{- end }}
{{- end }}
