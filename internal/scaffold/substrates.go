package scaffold

// Substrates is keyed by Provider. Adding a substrate is a single new
// entry here PLUS the matching API types / validator / dispatch /
// Ansible roles called out in specs/architecture.md. The scaffolder
// itself no longer requires per-substrate Go files.
var Substrates = map[Provider]Substrate{
	ProviderEmulatedBareMetal: {
		ProviderNameSuffix: "libvirt",
		NetworkNameSuffix:  "bridge",
		EnvExtraSecrets: `    provider-host-ssh:
      file: ~/.ssh/id_rsa
    bmc-credentials:
      generated:
        credentials:
          username: admin
    proxy-credentials:
      generated:
        credentials:
          username: proxy
`,
		EnvArtifactServer: `  infraComponents:
    nameResolution:
      - name: default
        default: true
        type: managed
        componentRef:
          name: name-resolution

`,
		HostsYAML: `apiVersion: bootwright.io/v1alpha1
kind: Host
metadata:
  name: lab-host
spec:
  addresses:
    - name: ssh
      address: 192.168.10.11             # change to the lab host's address
    - name: cluster-lan
      address: 192.168.130.1

  ssh:
    addressName: ssh
    keyRef:
      name: provider-host-ssh          # resolves under Environment.spec.secrets

  capabilities:                        # canonical: libvirt, container-runtime
    - libvirt
    - container-runtime
`,
		NetworkConnectivity: `  libvirt:
    bridge: vbr-{{.NetworkID}}          # libvirt bridge Bootwright will create
`,
		NetworkDNSServers: "",
		NetworkDNSRefs: `  dnsRefs:
    - default

`,
		ProviderCapabilities: `  machineProfiles:
    - name: sno
      cpu: 8
      memoryMiB: 22528
      diskGiB: 120
      libvirt:
        hostRef:
          name: lab-host
        uri: qemu:///system
        bmcEmulationDefaults:
          enabled: true
          protocol: redfish
          port: 8000
          vmediaPort: 8001
          auth:
            credentialRef:
              name: bmc-credentials

`,
		InfraComponentYAML: `apiVersion: bootwright.io/v1alpha1
kind: InfraComponent
metadata:
  name: load-balancer
spec:
  loadBalancer:
    type: haProxy
    hostRef:
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
  name: proxy
spec:
  proxy:
    type: squid
    hostRef:
      name: lab-host
    bindAddress: 0.0.0.0
    port: 3128
---
apiVersion: bootwright.io/v1alpha1
kind: InfraComponent
metadata:
  name: name-resolution
spec:
  nameResolution:
    type: dnsmasq
    hostRef:
      name: lab-host
    bindAddress: 192.168.130.1
    port: 53
`,
		ClusterMachineFrom: `        from:
          provider: {{.ProviderID}}
          profile: sno`,
		ClusterMachineExtras: "",
		ClusterServices:      "",
		EndpointsYAML: `    api:
      providedBy:
        address: control-plane
        componentRef:
          name: load-balancer
    apiInt:
      providedBy:
        address: control-plane
        componentRef:
          name: load-balancer
    ingress:
      providedBy:
        address: apps
        componentRef:
          name: load-balancer
`,
		PlatformYAML: `  platform:
    type: baremetal
    baremetal:
      provisioningNetwork: disabled
`,
		BootDevice: "/dev/vda",
	},
	ProviderBareMetal: {
		ProviderNameSuffix: "bare-metal",
		NetworkNameSuffix:  "vlan",
		EnvExtraSecrets: `    provider-host-ssh:
      file: ~/.ssh/id_rsa
    bmc-credentials:
      generated:
        credentials:
          username: admin
`,
		HostsYAML: `apiVersion: bootwright.io/v1alpha1
kind: Host
metadata:
  name: services-host
spec:
  addresses:
    - name: ssh
      address: 192.168.130.1
    - name: bmc-lan
      address: 192.168.130.1

  ssh:
    addressName: ssh
    keyRef:
      name: provider-host-ssh

  capabilities:
    - container-runtime
`,
		EnvArtifactServer: `  infraComponents:
    artifactServers:
      - name: default
        type: managed
        default: true
        componentRef:
          name: artifact-server
        routes:
          redfishVirtualMedia:
            endpoint: bmc

`,
		NetworkConnectivity: `  physical:
    vlan: 0                              # untagged; set a non-zero VLAN ID for tagged
`,
		ProviderCapabilities: `  machines:                           # explicit hardware inventory
    - name: rack1-srv1
      baremetal:
        bootMACAddress: 52:54:00:21:11:10
        interfaces:
          - name: primary
            macAddress: 52:54:00:21:11:10
        rootDeviceHints:
          deviceName: /dev/sda
        bmc:
          address: redfish-virtualmedia+https://bmc-rack1-srv1.example.test/redfish/v1/Systems/1
          protocol: redfish
          credentialsRef:
            name: bmc-credentials
          disableCertificateVerification: true
`,
		InfraComponentYAML: `apiVersion: bootwright.io/v1alpha1
kind: InfraComponent
metadata:
  name: artifact-server
spec:
  artifactServer:
    hostRef:
      name: services-host
    listeners:
      - name: https
        protocol: https
        port: 8443
    endpoints:
      - name: bmc
        listener: https
        addressName: bmc-lan
`,
		ClusterMachineFrom: `        from:
          provider: {{.ProviderID}}
          name: rack1-srv1`,
		ClusterMachineExtras: "",
		// No nameResolution component on this substrate, so the
		// renderer will not auto-inject a DNS service IP — keep an
		// explicit upstream resolver here.
		NetworkDNSServers: `      dns-resolver:
        config:
          server:
            - 192.168.130.1
`,
		ClusterServices: "",
		EndpointsYAML: `    api:
      externalVip: 192.168.130.10       # an operator-owned LB owns the VIP
    apiInt:
      externalVip: 192.168.130.10
    ingress:
      externalVip: 192.168.130.11
`,
		PlatformYAML: `  platform:
    type: baremetal
    baremetal:
      provisioningNetwork: disabled
`,
		BootDevice: "/dev/sda",
	},
	ProviderVSphere: {
		ProviderNameSuffix: "vsphere",
		NetworkNameSuffix:  "portgroup",
		EnvExtraSecrets: `    provider-host-ssh:
      file: ~/.ssh/id_rsa
    vcenter-credentials:
      file: ../secrets/vcenter-credentials
`,
		HostsYAML: `apiVersion: bootwright.io/v1alpha1
kind: Host
metadata:
  name: bastion
spec:
  addresses:
    - name: ssh
      address: bastion.example.test       # change to the bastion host's address

  ssh:
    addressName: ssh
    keyRef:
      name: provider-host-ssh

  capabilities:
    - container-runtime
`,
		NetworkConnectivity: `  vsphere:
    portgroup: ocp-install              # vSphere portgroup fronting this network
`,
		ProviderCapabilities: `  machineProfiles:
    - name: sno
      cpu: 8
      memoryMiB: 22528
      diskGiB: 120
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
        template: rhcos
`,
		ClusterMachineFrom: `        from:
          provider: {{.ProviderID}}
          profile: sno`,
		ClusterMachineExtras: "",
		// No nameResolution component on this substrate, so the
		// renderer will not auto-inject a DNS service IP — keep an
		// explicit upstream resolver here.
		NetworkDNSServers: `      dns-resolver:
        config:
          server:
            - 192.168.130.1
`,
		ClusterServices: "",
		EndpointsYAML: `    api:
      externalVip: 192.168.130.10
    apiInt:
      externalVip: 192.168.130.10
    ingress:
      externalVip: 192.168.130.11
`,
		PlatformYAML: `  platform:
    type: vsphere
`,
		BootDevice: "/dev/sda",
	},
	ProviderKubeVirt: {
		ProviderNameSuffix: "kubevirt",
		NetworkNameSuffix:  "nad",
		EnvExtraSecrets: `    provider-host-ssh:
      file: ~/.ssh/id_rsa
    cnv-cluster-kubeconfig:
      file: ~/.kube/cnv-cluster.kubeconfig
`,
		HostsYAML: `apiVersion: bootwright.io/v1alpha1
kind: Host
metadata:
  name: bastion
spec:
  addresses:
    - name: ssh
      address: bastion.example.test       # change to the bastion host's address

  ssh:
    addressName: ssh
    keyRef:
      name: provider-host-ssh

  capabilities:
    - container-runtime
`,
		NetworkConnectivity: `  kubevirt:
    nad: ocp-install                    # NetworkAttachmentDefinition on the host cluster
`,
		ProviderCapabilities: `  machineProfiles:
    - name: sno
      cpu: 8
      memoryMiB: 22528
      diskGiB: 120
      kubevirt:
        clusterRef:
          name: cnv-cluster-kubeconfig
        namespace: bootwright-vms
        # storageClassRef:
        #   name: <storage-class>       # optional override
`,
		ClusterMachineFrom: `        from:
          provider: {{.ProviderID}}
          profile: sno`,
		ClusterMachineExtras: "",
		// No nameResolution component on this substrate, so the
		// renderer will not auto-inject a DNS service IP — keep an
		// explicit upstream resolver here.
		NetworkDNSServers: `      dns-resolver:
        config:
          server:
            - 192.168.130.1
`,
		ClusterServices: "",
		EndpointsYAML: `    api:
      externalVip: 192.168.130.10
    apiInt:
      externalVip: 192.168.130.10
    ingress:
      externalVip: 192.168.130.11
`,
		PlatformYAML: `  platform:
    type: none
`,
		BootDevice: "/dev/vda",
	},
}
