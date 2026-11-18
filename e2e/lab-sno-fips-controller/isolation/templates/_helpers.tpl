{{- define "isolation.profile" -}}
{{- $profile := default "hardened" (default dict .Values.isolation).profile -}}
{{- if not (has $profile (list "hardened" "base")) -}}
{{- fail "isolation.profile must be hardened or base" -}}
{{- end -}}
{{- $profile -}}
{{- end -}}
