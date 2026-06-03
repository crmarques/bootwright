package scaffold

var kubeVirtSubstrate = Substrate{
	ProviderNameSuffix: "kubevirt",
	NetworkNameSuffix:  "nad",
	EnvExtraSecrets: `    - provider-host-ssh:
        file: ~/.ssh/bootwright-ssh-key
    - cnv-cluster-kubeconfig:
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
	ProviderNetworkAttachments: `
  networkAttachments:
    - name: {{.NetworkID}}
      kubevirt:
        nadRef:
          name: ocp-install             # NetworkAttachmentDefinition on the host cluster
          namespace: bootwright-vms

`,
	ClusterNetworkBindings: `
  networkBindings:
    - networkConfigRef:
        name: {{.NetworkID}}
      providerRef:
        name: {{.ProviderID}}
      attachmentRef:
        name: {{.NetworkID}}
`,
	ProviderCapabilities: `  machineProfiles:
    - name: sno
      cpu: 8
      memoryMiB: 22528
      diskGiB: 120
      kubevirt:
        kubeconfigRef:
          name: cnv-cluster-kubeconfig
        namespace: bootwright-vms
        # storageClassRef:
        #   name: <storage-class>       # optional override
`,
	ClusterMachineFrom: `        from:
          provider: {{.ProviderID}}
          profile: sno`,
	ClusterMachineExtras: "",
	NetworkDNSServers: `      dns-resolver:
        config:
          server:
            - 192.168.130.1
`,
	ClusterServices: "",
	EndpointsYAML: `    api:
      address: 192.168.130.10
      source:
        type: external
    api-int:
      address: 192.168.130.10
      source:
        type: external
    apps:
      address: 192.168.130.11
      source:
        type: external
`,
	PlatformYAML: `  platform:
    # Installer platform render mode; substrate inventory stays in InfraProvider.
    type: none
`,
	BootDevice: "/dev/vda",
}
