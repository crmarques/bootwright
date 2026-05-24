package scaffold

// Substrates is keyed by Provider. Adding a substrate is a single new
// entry here PLUS the matching API types / validator / dispatch /
// Ansible roles called out in specs/architecture.md. The scaffolder
// itself no longer requires per-substrate Go files.
var Substrates = map[Provider]Substrate{
	ProviderEmulatedBareMetal: {
		ProviderNameSuffix: "libvirt",
		NetworkNameSuffix:  "bridge",
		BastionHostRef:     "lab-host",
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
		// dnsServers omitted on purpose: this project declares
		// components.nameResolution below, so the renderer auto-injects
		// the bootwright dnsmasq IP (the libvirt bridge gateway) into
		// every node's resolver list. Empty here = "use DHCP for the
		// rest"; add upstream entries explicitly if you want them.
		NetworkDNSServers: "",
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

  loadBalancers:
    - name: default
      haProxy:
        hostRef:
          name: lab-host

  artifactPublishers:
    - name: default
      http:
        hostRef:
          name: lab-host
        routes:
          clusterInstall:
            addressName: cluster-lan

  proxies:
    - name: default
      squid:
        hostRef:
          name: lab-host

  dns:
    - name: default
      dnsmasq:
        hostRef:
          name: lab-host
`,
		ClusterMachineFrom: `        from:
          provider: {{.ProviderID}}
          profile: sno`,
		ClusterMachineExtras: "",
		ClusterServices: `    loadBalancers:
      - name: default
        from:
          provider: {{.ProviderID}}
          name: default
        bindAddresses:
          - name: control-plane
            ip: 192.168.130.10
          - name: apps
            ip: 192.168.130.11
    proxy:
      from:
        provider: {{.ProviderID}}
        name: default
      port: 3128
      bindAddress: 0.0.0.0
    nameResolution:
      from:
        provider: {{.ProviderID}}
        name: default
      port: 53
      bindAddress: 192.168.130.1
`,
		EndpointsYAML: `    api:
      providedBy:
        loadBalancer: default
        address: control-plane
    apiInt:
      providedBy:
        loadBalancer: default
        address: control-plane
    ingress:
      providedBy:
        loadBalancer: default
        address: apps
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
		BastionHostRef:     "services-host",
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

  artifactPublishers:
    - name: default
      http:
        hostRef:
          name: services-host
        routes:
          redfishVirtualMedia:
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
		BastionHostRef:     "bastion",
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
		BastionHostRef:     "bastion",
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
