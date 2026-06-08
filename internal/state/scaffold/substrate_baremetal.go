package scaffold

var bareMetalSubstrate = Substrate{
	ProviderNameSuffix: "bare-metal",
	NetworkNameSuffix:  "vlan",
	EnvExtraSecrets: `    - bastion-host-ssh:
        file: ~/.ssh/bootwright-ssh-key
    - bmc-credentials
`,
	MachinesYAML: `apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: services-host
spec:
  capabilities:
    - container-runtime
    - artifact-server
  os:
    provided: true
  addresses:
    - name: ssh
      address: 192.168.130.1
    - name: bmc-lan
      address: 192.168.130.1
  access:
    ssh:
      addressRef:
        name: ssh
      keyRef:
        name: bastion-host-ssh
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
  hardware:
    nics:
      - name: primary
        macAddress: 52:54:00:21:11:10
    boot:
      nicRef:
        name: primary
    management:
      bmc:
        address: redfish-virtualmedia+https://bmc-rack1-srv1.example.test/redfish/v1/Systems/1
        credentialsRef:
          name: bmc-credentials
        disableCertificateVerification: true
  os:
    provided: false
    install:
      rootDeviceHints:
        deviceName: /dev/sda
  network:
    config:
      networkConfigRef:
        name: {{.NetworkID}}
      attachmentRef:
        name: {{.NetworkID}}
      interfaceAddresses:
        - interface: primary
          addressRef:
            name: ip
          prefixLength: 24
    interfaceBinding:
      - nicRef:
          name: primary
        interfaceName: primary
  addresses:
    - name: ip
      address: 192.168.130.20
`,
	EnvArtifactServer: `  infraComponents:
    artifactServers:
      - name: default
        type: managed
        componentRef:
          name: artifact-server

`,
	ProviderCapabilities: `apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata:
  name: {{.ProviderID}}
spec:
  type: baremetal
  bareMetal:
    boot:
      method: redfish-virtualmedia
  networkAttachments:
    - name: {{.NetworkID}}
      bareMetal:
        vlan: 0
`,
	ClusterArtifactAccess: `    artifactAccess:
      serverRef:
        name: default
      redfishVirtualMedia:
        endpointRef:
          name: bmc
`,
	InfraComponentYAML: `apiVersion: bootwright.io/v1alpha1
kind: InfraComponent
metadata:
  name: artifact-server
spec:
  artifactServer:
    machineRef:
      name: services-host
    endpoints:
      - name: bmc
        listener: https
        machineAddress: bmc-lan
`,
	NetworkDNSServers: `      dns-resolver:
        config:
          server:
            - 192.168.130.1
`,
	EndpointsYAML: `      api:
        address: 192.168.130.10
        source:
          type: external
      api-int:
        address: 192.168.130.10
        source:
          type: external
      ingress:
        address: 192.168.130.11
        source:
          type: external
`,
	PlatformYAML: `    platform:
      type: bareMetal
      baremetal:
        provisioningNetwork: disabled
`,
}
