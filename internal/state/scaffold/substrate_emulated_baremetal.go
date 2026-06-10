package scaffold

var emulatedBareMetalSubstrate = Substrate{
	ProviderNameSuffix: "libvirt",
	NetworkNameSuffix:  "bridge",
	EnvExtraSecrets: `    - bastion-host-ssh:
        file: ~/.ssh/bootwright-ssh-key
    - bmc-credentials
`,
	EnvArtifactServer: `  infraComponents:
    nameResolution:
      - name: default
        type: managed
        componentRef: name-resolution
    ntpSources:
      - name: default
        type: managed
        componentRef: ntp-server
        endpointRef: cluster

`,
	MachinesYAML: `apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: bastion
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
      addressRef: ssh
      keyRef: bastion-host-ssh
---
apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: {{.Cluster}}-master-0
spec:
  capabilities:
    - openshift-node
  substrate:
    providerRef: {{.ProviderID}}
    profileRef: sno
  os:
    provided: false
  network:
    config:
      networkConfigRef: {{.NetworkID}}
      attachmentRef: {{.NetworkID}}
      interfaceAddresses:
        - interface: primary
          addressRef: ip
          prefixLength: 24
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
  baremetal:
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
    machineRef: bastion
    uri: qemu:///system
    bmcEmulationDefaults:
      auth:
        credentialsRef: bmc-credentials
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
  type: loadBalancer
  loadBalancer:
    implementation: haproxy
    machineRef: bastion
    bindAddresses:
      - name: control-plane
        address: 192.168.130.10
      - name: apps
        address: 192.168.130.11
---
apiVersion: bootwright.io/v1alpha1
kind: InfraComponent
metadata:
  name: name-resolution
spec:
  type: nameResolution
  nameResolution:
    implementation: dnsmasq
    machineRef: bastion
    bindAddress: 192.168.130.1
---
apiVersion: bootwright.io/v1alpha1
kind: InfraComponent
metadata:
  name: ntp-server
spec:
  type: ntp
  ntp:
    implementation: chrony
    machineRef: bastion
    bindAddress: 192.168.130.1
    endpoints:
      - name: cluster
        addressRef: cluster-lan
`,
	EndpointsYAML: `      api:
        source:
          type: infraComponent
          componentRef: load-balancer
          bindAddress: control-plane
      api-int:
        source:
          type: infraComponent
          componentRef: load-balancer
          bindAddress: control-plane
      ingress:
        source:
          type: infraComponent
          componentRef: load-balancer
          bindAddress: apps
`,
	PlatformYAML: `    platform:
      type: baremetal
      baremetal:
        provisioningNetwork: disabled
`,
}
