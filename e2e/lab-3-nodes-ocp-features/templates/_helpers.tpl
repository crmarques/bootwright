{{- define "address" -}}
{{- if .Values.ipv6 -}}
{{- required "ipv6 is true: set networks.ipv6.controllerAddress to an IPv6 address the controller holds and the guests reach" .Values.networks.ipv6.controllerAddress -}}
{{- else if eq .Values.host.address "auto" -}}
{{- required "host.address is auto and no default route was found; set host.address" .Values.host.detectedAddress -}}
{{- else -}}
{{- .Values.host.address -}}
{{- end -}}
{{- end -}}
