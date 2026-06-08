package scaffold

var vSphereSubstrate = Substrate{
	ProviderNameSuffix: "vsphere",
	NetworkNameSuffix:  "portgroup",
	EnvExtraSecrets: `    - vcenter-credentials:
        file: ../secrets/vcenter-credentials
`,
	MachinesYAML: `apiVersion: bootwright.io/v1alpha1
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
      interfaceAddresses:
        - interface: primary
          addressRef:
            name: ip
          prefixLength: 24
  addresses:
    - name: ip
      address: 192.168.130.20
`,
	ProviderCapabilities: `apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata:
  name: {{.ProviderID}}
spec:
  type: vsphere
  vsphere:
    vcenters:
      - server: vcenter.example.test
        port: 443
        datacenters:
          - dc1
        credentialsRef:
          name: vcenter-credentials
    failureDomains:
      - name: dc1-zone-a
        region: dc1
        zone: zone-a
        server: vcenter.example.test
        topology:
          datacenter: dc1
          computeCluster: /dc1/host/cluster1
          datastore: /dc1/datastore/datastore1
          folder: /dc1/vm/bootwright
          resourcePool: /dc1/host/cluster1/Resources/bootwright
          networks:
            - ocp-install
    machineProfiles:
      - name: sno
        cpu: 8
        memoryMiB: 22528
        diskGiB: 120
        template: rhcos
        failureDomainRef:
          name: dc1-zone-a
  networkAttachments:
    - name: {{.NetworkID}}
      vsphere:
        portgroup: ocp-install
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
      type: vsphere
`,
}
