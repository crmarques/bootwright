package scaffold

var kubeVirtSubstrate = Substrate{
	ProviderNameSuffix: "kubevirt",
	NetworkNameSuffix:  "nad",
	EnvExtraSecrets: `    - cnv-cluster-kubeconfig:
        file: ~/.kube/cnv-cluster.kubeconfig
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
	ProviderCapabilities: `apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata:
  name: {{.ProviderID}}
spec:
  type: kubevirt
  kubevirt:
    kubeconfigRef:
      name: cnv-cluster-kubeconfig
    namespace: bootwright-vms
    machineProfiles:
      - name: sno
        cpu: 8
        memoryMiB: 22528
        diskGiB: 120
  networkAttachments:
    - name: {{.NetworkID}}
      kubevirt:
        nadRef:
          name: ocp-install
          namespace: bootwright-vms
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
      type: none
`,
}
