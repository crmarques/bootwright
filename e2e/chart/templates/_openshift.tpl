{{/*
Shared objects of the OpenShift cases (sno, compact). The services, provider,
network configuration and generic Secrets come from _helpers.tpl.
*/}}

{{/* e2e.ocpMachine: dict root, name, n, profile. An installer-provisioned guest. */}}
{{- define "e2e.ocpMachine" -}}
{{- $v := .root.Values -}}
---
# bw-path: infra/machines/{{ .name }}.yaml
apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: {{ .name }}

spec:
  substrate:
    providerRef: e2e-libvirt
    libvirt:
      profileRef: {{ .profile }}

  hardware:
    rootDevice:
      deviceName: /dev/vda

  os:
    provided: false

  network:
    configRef: e2e-guests
    attachmentRef: e2e-guests
    installAddressRef: ip

    addresses:
      - name: fqdn
        address: {{ .name }}.{{ $v.host.domain }}

      - name: ip
        address: {{ include "e2e.guestIP" . }}/{{ $v.host.network.prefix }}
        interface: enp1s0
{{- end -}}

{{/* e2e.ocpSecrets: dict root, cluster. The pull secret and the cluster admin key. */}}
{{- define "e2e.ocpSecrets" -}}
{{- $v := .root.Values -}}
---
# bw-path: secret-descriptors/openshift-pull-secret.yaml
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: openshift-pull-secret

spec:
  type: dockerConfigJson

  source:
    contextStore: {}
---
# bw-path: secret-descriptors/{{ .cluster }}-cluster-admin-ssh-key.yaml
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: {{ .cluster }}-cluster-admin-ssh-key

spec:
  type: sshKeyPair

  source:
    generated:
      keyType: {{ if $v.security.fips }}ecdsaP256{{ else }}ed25519{{ end }}
      comment: {{ .cluster }}-cluster-admin
{{- end -}}

{{/* e2e.ocpSecurity: dict root. The cluster's spec.security block, when a toggle asks for one. */}}
{{- define "e2e.ocpSecurity" -}}
{{- $sec := .root.Values.security -}}
{{- if or $sec.fips $sec.tpm2 }}
  security:
{{- if $sec.fips }}
    fips:
      enabled: true
{{- end }}
{{- if $sec.tpm2 }}
    diskEncryption:
      unlock:
        tpm2: {}
{{- end }}
{{- end }}
{{- end -}}

{{/*
e2e.ocpAddons: dict root, cluster, packages (list of "nmstate" and "metallb").
One ClusterAddonBinding. The channel and starting CSV are operator choices that
no default can supply, so rendering an enabled package without them fails.
*/}}
{{- define "e2e.ocpAddons" -}}
{{- $v := .root.Values -}}
---
# bw-path: add-ons/binding.yaml
apiVersion: bootwright.io/v1alpha1
kind: ClusterAddonBinding
metadata:
  name: {{ .cluster }}-addons

spec:
  clusterRef: {{ .cluster }}

  packages:
{{- range $i, $name := .packages }}
{{- $set := get $v.addons.subscriptions $name }}
{{- if $i }}
{{ end }}
    {{ $name }}:
      subscription:
        channel: {{ required (printf "set addons.subscriptions.%s.channel" $name) $set.channel }}
        startingCSV: {{ required (printf "set addons.subscriptions.%s.startingCSV" $name) $set.startingCSV }}
{{- if eq $name "metallb" }}

      pools:
        - name: services

          addresses:
{{- range $v.addons.metallbPool }}
            - {{ . }}
{{- end }}
{{- end }}
{{- end }}
{{- end -}}

{{/* e2e.ocpCluster: dict root, name, nodes (list of machine names), vip (bool), packages. */}}
{{- define "e2e.ocpCluster" -}}
{{- $v := .root.Values -}}
---
# bw-path: clusters/{{ .name }}/cluster.yaml
apiVersion: bootwright.io/v1alpha1
kind: OpenShiftCluster
metadata:
  name: {{ .name }}

spec:
  distribution:
    type: openshift

    release:
      version: {{ $v.openshift.version }}

  install:
    mode: connected

    endpoints:
      api:
{{- if .vip }}
        address: {{ include "e2e.guestIP" (dict "root" .root "n" .apiGuest) }}

        source:
          type: installer
{{- else }}
        source:
          type: node
{{- end }}

      ingress:
{{- if .vip }}
        address: {{ include "e2e.guestIP" (dict "root" .root "n" .ingressGuest) }}

        source:
          type: installer
{{- else }}
        source:
          type: node
{{- end }}

    agent:
      redfishVirtualMedia:
        artifactServerEndpoint:
          serverRef: e2e-artifacts
          endpointRef: ip-https

    ntp:
      - serverRef: e2e-ntp
        endpointRef: ip

  nodes:
{{- range $i, $m := .nodes }}
    - name: master-{{ $i }}
      role: controlPlane
      machineRef: {{ $m }}
{{ end }}
{{ include "e2e.ocpSecurity" (dict "root" .root) }}
{{- end -}}
