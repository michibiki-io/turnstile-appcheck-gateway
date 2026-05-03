{{/*
Expand the name of the chart.
*/}}
{{- define "turnstile-appcheck-gateway.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "turnstile-appcheck-gateway.fullname" -}}
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
Create chart name and version as used by the chart label.
*/}}
{{- define "turnstile-appcheck-gateway.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Common labels.
*/}}
{{- define "turnstile-appcheck-gateway.labels" -}}
helm.sh/chart: {{ include "turnstile-appcheck-gateway.chart" . }}
app.kubernetes.io/name: {{ include "turnstile-appcheck-gateway.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
Selector labels.
*/}}
{{- define "turnstile-appcheck-gateway.selectorLabels" -}}
app.kubernetes.io/name: {{ include "turnstile-appcheck-gateway.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
Create the name of the service account to use.
*/}}
{{- define "turnstile-appcheck-gateway.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "turnstile-appcheck-gateway.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/*
Create the ConfigMap name.
*/}}
{{- define "turnstile-appcheck-gateway.configMapName" -}}
{{- printf "%s-config" (include "turnstile-appcheck-gateway.fullname" .) -}}
{{- end -}}

{{/*
Create the Secret name.
*/}}
{{- define "turnstile-appcheck-gateway.secretName" -}}
{{- default (printf "%s-secret" (include "turnstile-appcheck-gateway.fullname" .)) .Values.existingSecret -}}
{{- end -}}

{{/*
Create the PVC name.
*/}}
{{- define "turnstile-appcheck-gateway.pvcName" -}}
{{- default (include "turnstile-appcheck-gateway.fullname" .) .Values.persistence.existingClaim -}}
{{- end -}}

{{/*
Normalize the App Check subpath.
*/}}
{{- define "turnstile-appcheck-gateway.appCheckSubpath" -}}
{{- $subpath := trim (default "/appcheck" (index .Values.config "APPCHECK_SUBPATH")) -}}
{{- if eq $subpath "" -}}
/appcheck
{{- else if hasPrefix "/" $subpath -}}
{{- trimSuffix "/" $subpath | default "/" -}}
{{- else -}}
/{{ trimSuffix "/" $subpath }}
{{- end -}}
{{- end -}}

{{/*
Normalize the admin base path.
*/}}
{{- define "turnstile-appcheck-gateway.adminBasePath" -}}
{{- $base := trim (default "/admin" (index .Values.config "ADMIN_BASE_PATH")) -}}
{{- if eq $base "" -}}
/admin
{{- else if hasPrefix "/" $base -}}
{{- trimSuffix "/" $base | default "/admin" -}}
{{- else -}}
/{{ trimSuffix "/" $base }}
{{- end -}}
{{- end -}}

{{/*
Return the configured health path.
*/}}
{{- define "turnstile-appcheck-gateway.healthPath" -}}
{{- default "/healthz" (index .Values.config "HEALTH_PATH") -}}
{{- end -}}

{{/*
Return the configured readiness path.
*/}}
{{- define "turnstile-appcheck-gateway.readyPath" -}}
{{- default "/readyz" (index .Values.config "READY_PATH") -}}
{{- end -}}

{{/*
Return the writable data mount path.
*/}}
{{- define "turnstile-appcheck-gateway.dataMountPath" -}}
{{- default "/var/lib/turnstile-appcheck-gateway" .Values.persistence.mountPath -}}
{{- end -}}

{{/*
Return the admin dashboard path under the public subpath.
*/}}
{{- define "turnstile-appcheck-gateway.adminDashboardPath" -}}
{{- printf "%s%s/" (include "turnstile-appcheck-gateway.appCheckSubpath" .) (include "turnstile-appcheck-gateway.adminBasePath" .) -}}
{{- end -}}

{{/*
Return the admin API base path under the public subpath.
*/}}
{{- define "turnstile-appcheck-gateway.adminAPIPath" -}}
{{- printf "%s/_admin/api/v1" (include "turnstile-appcheck-gateway.appCheckSubpath" .) -}}
{{- end -}}
