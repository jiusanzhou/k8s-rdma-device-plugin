{{/*
Common labels
*/}}
{{- define "rdma-device-plugin.labels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "rdma-device-plugin.selectorLabels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
{{- end }}
