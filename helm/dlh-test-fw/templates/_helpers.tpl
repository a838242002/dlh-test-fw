{{/* Common labels for our own resources. */}}
{{- define "dlh.labels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
{{- end }}

{{- define "dlh.namespace" -}}
{{ .Values.namespace | default "dlh-test-fw" }}
{{- end }}
