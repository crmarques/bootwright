{{- define "address" -}}
{{- if eq .Values.host.address "auto" -}}
{{- required "host.address is auto and no default route was found; set host.address" .Values.host.detectedAddress -}}
{{- else -}}
{{- .Values.host.address -}}
{{- end -}}
{{- end -}}
