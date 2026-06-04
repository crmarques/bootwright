package scaffold

var bareMetalSubstrate = Substrate{
	ProviderNameSuffix: "bare-metal",
	NetworkNameSuffix:  "vlan",
	EnvExtraSecrets: `    - provider-host-ssh:
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
  substrate:
    providerRef:
      name: {{.ProviderID}}
    bareMetal:
      interfaces:
        - name: primary
          macAddress: 52:54:00:30:00:01
  os:
    mode: external
    addresses:
      - name: ssh
        address: 192.168.130.1
      - name: bmc-lan
        address: 192.168.130.1
    ssh:
      addressName: ssh
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
    bareMetal:
      bootMACAddress: 52:54:00:21:11:10
      interfaces:
        - name: primary
          macAddress: 52:54:00:21:11:10
      rootDeviceHints:
        deviceName: /dev/sda
      bmc:
        address: redfish-virtualmedia+https://bmc-rack1-srv1.example.test/redfish/v1/Systems/1
        credentialsRef:
          name: bmc-credentials
        disableCertificateVerification: true
  os:
    mode: raw
    install:
      network:
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
