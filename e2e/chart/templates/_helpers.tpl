{{/*
Shared objects of every case. Each document opens with "# bw-path: <file>",
the file e2e/render.sh writes it to. Helpers that need parameters take a dict
whose "root" is the chart's top-level context.
*/}}

{{- define "e2e.address" -}}
{{- if eq .Values.host.address "auto" -}}
{{- required "host.address is auto and no default route was found; set host.address" .Values.host.detectedAddress -}}
{{- else -}}
{{- .Values.host.address -}}
{{- end -}}
{{- end -}}

{{/* e2e.guestIP: dict root, n. The guest address firstGuest + n, without prefix. */}}
{{- define "e2e.guestIP" -}}
{{- $net := .root.Values.host.network -}}
{{- printf "%s.%d" (regexReplaceAll "\\.[0-9]+$" $net.gateway "") (add $net.firstGuest .n) -}}
{{- end -}}

{{/* e2e.environment: dict root, sites (optional list of {name, description}). */}}
{{- define "e2e.environment" -}}
{{- $v := .root.Values -}}
---
# bw-path: environment.yaml
apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata:
  name: e2e-{{ $v.case }}

spec:
  domains:
    base: {{ $v.host.domain }}

  controller:
    machineRef: controller

  remoteMachinesAccessKey:
    keyRef: bootwright-machine-key
{{- with .sites }}

  sites:
{{- range . }}
    - name: {{ .name }}
{{- end }}
{{- end }}
{{- end -}}

{{- define "e2e.controller" -}}
---
# bw-path: infra/machines/controller.yaml
apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: controller

spec:
  os:
    provided: true

  proxy:
    direct: {}

  network:
    addresses:
      - name: ip
        address: {{ include "e2e.address" . }}

  access:
    local: true
{{- end -}}

{{/*
e2e.services: dict root, external (bool), httpsPort, httpPort. Managed DNS,
NTP and proxy run on the controller unless external, when they name the ones
another context on this host manages. The artifact server is always managed.
*/}}
{{- define "e2e.services" -}}
{{- $v := .root.Values -}}
{{- $address := include "e2e.address" .root -}}
---
# bw-path: infra/components/dns.yaml
apiVersion: bootwright.io/v1alpha1
kind: DNSServer
metadata:
  name: e2e-dns

spec:
{{- if .external }}
  management: external
  address: {{ $address }}
{{- else }}
  management: managed
  machineRef: controller
  implementation: dnsmasq
  port: 53

  endpoints:
    - name: ip
      addressRef: ip

  forwarders:
{{- range $v.host.upstreamDNS }}
    - {{ . }}
{{- end }}
{{- end }}
---
# bw-path: infra/components/ntp.yaml
apiVersion: bootwright.io/v1alpha1
kind: NTPServer
metadata:
  name: e2e-ntp

spec:
{{- if .external }}
  management: external
  address: {{ $address }}
{{- else }}
  management: managed
  machineRef: controller
  implementation: chrony
  port: 123

  endpoints:
    - name: ip
      addressRef: ip

  upstreamSources:
{{- range $v.host.upstreamNTP }}
    - {{ . }}
{{- end }}
{{- end }}
---
# bw-path: infra/components/proxy.yaml
apiVersion: bootwright.io/v1alpha1
kind: Proxy
metadata:
  name: e2e-proxy

spec:
{{- if .external }}
  management: external

  connection:
    httpProxy: http://{{ $address }}:3128
    httpsProxy: http://{{ $address }}:3128
{{- else }}
  management: managed
  machineRef: controller
  implementation: squid
  port: 3128

  endpoints:
    - name: ip
      addressRef: ip
{{- end }}
---
# bw-path: infra/components/artifact-server.yaml
apiVersion: bootwright.io/v1alpha1
kind: ArtifactServer
metadata:
  name: e2e-artifacts

spec:
  management: managed
  machineRef: controller
  retention: persistent

  tls:
    secretRef: artifact-server-tls
    minVersion: TLSv1.2

  listeners:
    - name: https
      protocol: https
      port: {{ .httpsPort | default 8443 }}

    - name: http
      protocol: http
      port: {{ .httpPort | default 8080 }}

  endpoints:
    - name: ip-https
      listenerRef: https
      addressRef: ip

    - name: ip-http
      listenerRef: http
      addressRef: ip
{{- end -}}

{{/*
e2e.provider: dict root, profiles (list of {name, cpu, memoryMiB, diskGiB,
dataDisks}), bmcPort (optional), bridge ("managed" default or "external").
*/}}
{{- define "e2e.provider" -}}
{{- $v := .root.Values -}}
{{- $net := $v.host.network -}}
---
# bw-path: infra/providers/e2e-libvirt.yaml
apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata:
  name: e2e-libvirt

spec:
  libvirt:
    capacityReserve:
      cpu: 1
      memoryMiB: 2048
      diskGiB: 10

    machineRef: controller

    bmcEmulation:
      bindAddress: 127.0.0.1
      port: {{ .bmcPort | default $v.host.bmcPort }}
      certificateRef: bmc-tls

      auth:
        credentialsRef: bmc-credentials

    machineProfiles:
{{- range .profiles }}
      - name: {{ .name }}
        cpu: {{ .cpu }}
        memoryMiB: {{ .memoryMiB }}
        diskGiB: {{ .diskGiB }}
        tpm: {}
{{- with .dataDisks }}

        dataDisks:
{{- toYaml . | nindent 10 }}
{{- end }}
{{- end }}

  networkAttachments:
    - name: e2e-guests

      libvirt:
        bridge: {{ $net.bridge }}
{{- if eq (.bridge | default "managed") "external" }}
        management: external
{{- else }}
        management: managed
        address: {{ $net.gateway }}/{{ $net.prefix }}
        forward: nat
{{- end }}
{{- end -}}

{{/* e2e.networkConfig: the chart root, or dict root, external (bool: the DNS server is external, so no endpoint is named). */}}
{{- define "e2e.networkConfig" -}}
{{- $root := . -}}
{{- $external := false -}}
{{- if hasKey . "root" -}}
{{- $root = .root -}}
{{- $external = .external | default false -}}
{{- end -}}
{{- $v := $root.Values -}}
{{- $net := $v.host.network -}}
---
# bw-path: infra/networkconfigs/e2e-guests.yaml
apiVersion: bootwright.io/v1alpha1
kind: NetworkConfig
metadata:
  name: e2e-guests

spec:
  machineNetwork:
    - cidr: {{ $net.cidr }}

  dns:
    - serverRef: e2e-dns
{{- if not $external }}
      endpointRef: ip
{{- end }}

  nmstate:
    interfaces:
      - name: enp1s0
        type: ethernet
        state: up
        mtu: 1500

        ipv4:
          enabled: true
          dhcp: false

        ipv6:
          enabled: false

    dns-resolver:
      config:
        search:
          - {{ $v.host.domain }}

    routes:
      config:
        - destination: 0.0.0.0/0
          next-hop-address: {{ $net.gateway }}
          next-hop-interface: enp1s0
          table-id: 254
          metric: 100
{{- end -}}

{{/*
e2e.rhelProfile: dict root, security (bool: honour security.*), registration
(bool: honour registration.*), packages (extra list), repositories (list of
repository entries, rendered verbatim), externalServices (bool).
*/}}
{{- define "e2e.rhelProfile" -}}
{{- $v := .root.Values -}}
{{- $net := $v.host.network -}}
{{- $fips := and .security $v.security.fips -}}
{{- $tpm2 := and .security $v.security.tpm2 -}}
{{- $register := and .registration $v.registration.enabled -}}
---
# bw-path: infra/os/rhel-boot.yaml
apiVersion: bootwright.io/v1alpha1
kind: MachineImage
metadata:
  name: rhel-boot

spec:
  bootMedia: local-media:{{ $v.rhel.bootMedia }}
---
# bw-path: infra/os/rhel.yaml
apiVersion: bootwright.io/v1alpha1
kind: MachineInstallProfile
metadata:
  name: rhel

spec:
  os:
    family: rhel
    version: {{ $v.rhel.version | quote }}
    architecture: x86_64

  updates:
    policy: {{ if and $register $v.registration.updateWithinRelease }}withinRelease{{ else }}none{{ end }}

  installer:
    anaconda:
      imageRef: rhel-boot

      redfishVirtualMedia:
        artifactServerEndpoint:
          serverRef: e2e-artifacts
          endpointRef: ip-https

      packageSource:
        hostedTree:
          fromMedia: local-media:{{ $v.rhel.dvdMedia }}

          artifactServerEndpoint:
            serverRef: e2e-artifacts
            endpointRef: ip-http

  ntp:
    - serverRef: e2e-ntp
{{- if not .externalServices }}
      endpointRef: ip
{{- end }}

  customizations:
    localization:
      language: en_US.UTF-8
      formats: en_US.UTF-8
      keyboard: us
      timezone: Etc/UTC

    ssh:
      passwordAuthentication: false

    packages:
      installWeakDeps: false
      excludeDocs: true

      install:
        - chrony
        - qemu-guest-agent
{{- range .packages }}
        - {{ . }}
{{- end }}

    services:
      enabled:
        - chronyd
        - qemu-guest-agent
        - sshd

    security:
      selinux:
        mode: enforcing
{{- if $fips }}

      fips:
        enabled: true
{{- end }}
{{- if $tpm2 }}

      diskEncryption:
        unlock:
          tpm2: {}
        recoveryPassphraseRef: luks-recovery
{{- end }}
{{- with .repositories }}

    repositories:
      configure:
{{- toYaml . | nindent 8 }}
{{- end }}
{{- if $register }}

  subscription:
    entitlementRef: rhel-rhsm
{{- end }}
{{- if $register }}
---
# bw-path: infra/os/rhel-rhsm.yaml
apiVersion: bootwright.io/v1alpha1
kind: Entitlement
metadata:
  name: rhel-rhsm

spec:
  type: redhatRhel

  rhsm:
    management: managed
    organizationRef: rhsm-org
    activationKeyRef: rhsm-key
    connectToInsights: false
---
# bw-path: secret-descriptors/rhsm-org.yaml
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: rhsm-org

spec:
  type: opaque

  source:
    contextStore: {}
---
# bw-path: secret-descriptors/rhsm-key.yaml
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: rhsm-key

spec:
  type: token

  source:
    contextStore: {}
{{- end }}
{{- if $tpm2 }}
---
# bw-path: secret-descriptors/luks-recovery.yaml
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: luks-recovery

spec:
  type: token

  source:
    contextStore: {}
{{- end }}
{{- end -}}

{{/*
e2e.rhelMachine: dict root, name, n, profile, site (optional), os ("install" or
"provided"), accessKeyRef (optional, provided only: the sshKeyPair Secret the
controller logs in with as user bootwright). A provided Machine declares
contacts only: the guest address without prefix, no network configuration.
*/}}
{{- define "e2e.rhelMachine" -}}
{{- $v := .root.Values -}}
{{- $provided := eq (.os | default "install") "provided" -}}
---
# bw-path: infra/machines/{{ .name }}.yaml
apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: {{ .name }}

spec:
{{- with .site }}
  placement:
    site: {{ . }}

{{- end }}
{{- if $provided }}
  os:
    provided: true
{{- with .accessKeyRef }}

  access:
    ssh:
      addressRef: ip
      user: bootwright

      auth:
        privateKeyRef: {{ . }}
{{- end }}

  network:
    addresses:
      - name: fqdn
        address: {{ .name }}.{{ $v.host.domain }}

      - name: ip
        address: {{ include "e2e.guestIP" . }}
{{- else }}
  substrate:
    providerRef: e2e-libvirt
    libvirt:
      profileRef: {{ .profile }}

  hardware:
    rootDevice:
      deviceName: /dev/vda

  os:
    provided: false
    installProfileRef: rhel

  network:
    configRef: e2e-guests
    installAddressRef: ip

    addresses:
      - name: fqdn
        address: {{ .name }}.{{ $v.host.domain }}

      - name: ip
        address: {{ include "e2e.guestIP" . }}/{{ $v.host.network.prefix }}
        interface: enp1s0
{{- end }}
{{- end -}}

{{/* e2e.secrets: dict root. The generated Secrets every case uses. */}}
{{- define "e2e.secrets" -}}
{{- $v := .root.Values -}}
---
# bw-path: secret-descriptors/artifact-server-tls.yaml
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: artifact-server-tls

spec:
  type: tlsCertificate

  source:
    generated:
      commonName: controller.{{ $v.host.domain }}

      dnsNames:
        - controller.{{ $v.host.domain }}

      ipAddresses:
        - {{ include "e2e.address" .root }}

      validityDays: 3650
---
# bw-path: secret-descriptors/bootwright-machine-key.yaml
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: bootwright-machine-key

spec:
  type: sshKeyPair

  source:
    generated:
      keyType: {{ if $v.security.fips }}ecdsaP256{{ else }}ed25519{{ end }}
      comment: bootwright-machine-key
---
# bw-path: secret-descriptors/bmc-credentials.yaml
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: bmc-credentials

spec:
  type: usernamePassword

  source:
    generated:
      username: admin
---
# bw-path: secret-descriptors/bmc-tls.yaml
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: bmc-tls

spec:
  type: tlsCertificate

  source:
    generated:
      commonName: localhost

      dnsNames:
        - localhost

      ipAddresses:
        - 127.0.0.1

      validityDays: 3650
{{- end -}}
