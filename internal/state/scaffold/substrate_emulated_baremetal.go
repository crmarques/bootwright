package scaffold

var emulatedBareMetalSubstrate = Substrate{
	ProviderNameSuffix: "libvirt",
	NetworkNameSuffix:  "bridge",
	EnvExtraSecrets: `    - provider-host-ssh:
        file: ~/.ssh/bootwright-ssh-key
    - bmc-credentials
`,
	EnvArtifactServer: `  infraComponents:
    nameResolution:
      - name: default
        type: managed
        componentRef:
          name: name-resolution
    ntpSources:
      - name: default
        type: managed
        componentRef:
          name: ntp-server
        endpoint: cluster

`,
	MachinesYAML: `apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: lab-host
spec:
  capabilities:
    - libvirt
    - container-runtime
    - load-balancer
    - name-resolution
    - ntp
  os:
    provided: true
  addresses:
    - name: ssh
      address: 192.168.10.11
    - name: cluster-lan
      address: 192.168.130.1
  access:
    ssh:
      addressRef:
        name: ssh
      keyRef:
        name: provider-host-ssh
---
apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: {{.Cluster}}-master-0
spec:
  capabilities:
    - openshift-node
  substrate:
    providerRef:
      name: {{.ProviderID}}
    profileRef:
      name: sno
  os:
    provided: false
  network:
    config:
      networkConfigRef:
        name: {{.NetworkID}}
      attachmentRef:
        name: {{.NetworkID}}
      overrides:
        interfaces:
          - name: primary
            ipv4:
              address:
                - ip: 192.168.130.20
                  prefix-length: 24
  addresses:
    - name: ip
      address: 192.168.130.20
`,
	NetworkDNSServers: "",
	NetworkDNSRefs: `  dnsRefs:
    - default

`,
	ProviderCapabilities: `apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata:
  name: {{.Cluster}}-hosts
spec:
  type: baremetal
  bareMetal:
    boot:
      method: external
---
apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata:
  name: {{.ProviderID}}
spec:
  type: libvirt
  libvirt:
    machineRef:
      name: lab-host
    uri: qemu:///system
    bmcEmulationDefaults:
      auth:
        credentialRef:
          name: bmc-credentials
    machineProfiles:
      - name: sno
        cpu: 8
        memoryMiB: 22528
        diskGiB: 120
  networkAttachments:
    - name: {{.NetworkID}}
      libvirt:
        bridge: vbr-{{.NetworkID}}
`,
	InfraComponentYAML: `apiVersion: bootwright.io/v1alpha1
kind: InfraComponent
metadata:
  name: load-balancer
spec:
  loadBalancer:
    type: haProxy
    machineRef:
      name: lab-host
    bindAddresses:
      - name: control-plane
        ip: 192.168.130.10
      - name: apps
        ip: 192.168.130.11
---
apiVersion: bootwright.io/v1alpha1
kind: InfraComponent
metadata:
  name: name-resolution
spec:
  nameResolution:
    type: dnsmasq
    machineRef:
      name: lab-host
    bindAddress: 192.168.130.1
---
apiVersion: bootwright.io/v1alpha1
kind: InfraComponent
metadata:
  name: ntp-server
spec:
  ntp:
    type: chrony
    machineRef:
      name: lab-host
    bindAddress: 192.168.130.1
    endpoints:
      - name: cluster
        machineAddress: cluster-lan
`,
	EndpointsYAML: `      api:
        source:
          type: infraComponent
          componentRef:
            name: load-balancer
          bindAddress: control-plane
      api-int:
        source:
          type: infraComponent
          componentRef:
            name: load-balancer
          bindAddress: control-plane
      ingress:
        source:
          type: infraComponent
          componentRef:
            name: load-balancer
          bindAddress: apps
`,
	PlatformYAML: `    platform:
      type: bareMetal
      baremetal:
        provisioningNetwork: disabled
`,
}
