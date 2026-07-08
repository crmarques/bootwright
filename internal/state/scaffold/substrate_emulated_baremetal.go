package scaffold

var emulatedBareMetalSubstrate = Substrate{
	ProviderNameSuffix: "libvirt",
	NetworkNameSuffix:  "bridge",
	EnvExtraSecrets: `---
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: bastion-host-ssh
spec:
  type: sshKeyPair
  source:
    file:
      privateKey: ~/.ssh/bootwright-ssh-key
---
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: bmc-credentials
spec:
  type: usernamePassword
  source:
    generated:
      username: bmc-admin
`,
	EnvArtifactServer: `  infraComponents:
    nameResolution:
      - name: default
        management: managed
        componentRef: name-resolution
    ntp:
      - name: default
        management: managed
        componentRef: ntp-server
        endpointRef: cluster

`,
	BastionMachinesYAML: `apiVersion: bootwright.io/v1alpha1
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
`,
	MachinesYAML: `apiVersion: bootwright.io/v1alpha1
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
	NetworkNameResolutionRefs: `  nameResolutionRefs:
    - default

`,
	ProviderCapabilities: `apiVersion: bootwright.io/v1alpha1
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
          bindAddressRef: control-plane
      api-int:
        source:
          type: infraComponent
          componentRef: load-balancer
          bindAddressRef: control-plane
      ingress:
        source:
          type: infraComponent
          componentRef: load-balancer
          bindAddressRef: apps
`,
	PlatformYAML: `    platform:
      type: baremetal
      baremetal:
        provisioningNetwork: disabled
`,
}
