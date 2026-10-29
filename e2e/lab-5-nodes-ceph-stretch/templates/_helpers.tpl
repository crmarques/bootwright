{{- define "address" -}}
{{- if eq .Values.host.address "auto" -}}
{{- required "host.address is auto and no default route was found; set host.address" .Values.host.detectedAddress -}}
{{- else -}}
{{- .Values.host.address -}}
{{- end -}}
{{- end -}}

{{/* site: the site of the Machine named by .name (dict root, name). */}}
{{- define "site" -}}
{{- $s := .root.Values.sites -}}
{{- if has .name $s.dc1 -}}dc1{{- else if has .name $s.dc2 -}}dc2{{- else -}}{{ $s.tiebreaker }}{{- end -}}
{{- end -}}

{{/* dataMachine: one Ceph data host of the ceph context (dict root, name). */}}
{{- define "dataMachine" -}}
{{- $v := .root.Values -}}
apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: {{ .name }}

spec:
  placement:
    site: {{ include "site" . }}

  substrate:
    providerRef: lab-libvirt
    libvirt:
      profileRef: data

  hardware:
    rootDevice:
      deviceName: /dev/vda

  os:
    provided: false
    installProfileRef: rhel

  network:
    configRef: lab-guests
    installAddressRef: ip

    addresses:
      - name: fqdn
        address: {{ .name }}.{{ $v.host.domain }}

      - name: ip
        address: {{ index $v.addresses .name }}/{{ $v.host.prefix }}
        interface: enp1s0
{{- end -}}
