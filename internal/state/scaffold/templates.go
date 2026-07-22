package scaffold

const environmentTmpl = `apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata:
  name: {{.Cluster}}
spec:
  baseDomain: example.test              # change to a domain you own

{{.Substrate.EnvArtifactServer}}
`

const secretsTmpl = `apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: openshift-pull-secret
spec:
  type: dockerConfigJson
---
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: {{.Cluster}}-cluster-admin-ssh-key
spec:
  type: sshKeyPair
  source:
    generated:
      comment: bootwright-{{.Cluster}}-cluster-admin
{{.EnvSecrets}}
`

const bastionMachinesTmpl = `{{.BastionMachinesYAML}}`

const clusterMachinesTmpl = `{{.MachinesYAML}}`

const networksTmpl = `apiVersion: bootwright.io/v1alpha1
kind: NetworkConfig
metadata:
  name: {{.NetworkID}}
spec:
  machineNetwork:
    - cidr: 192.168.130.0/24            # change to your cluster machine network

{{.Substrate.NetworkNameResolutionRefs}}
  template:
    networkConfig:
      interfaces:
        - name: primary
          type: ethernet
          state: up
          ipv4:
            enabled: true
            dhcp: false
          ipv6:
            enabled: false
{{.Substrate.NetworkDNSServers}}      routes:
        config:
          - destination: 0.0.0.0/0
            next-hop-address: 192.168.130.1
            next-hop-interface: primary
            table-id: 254
`

const providerTmpl = `{{.Substrate.ProviderCapabilities}}{{.Substrate.ProviderNetworkAttachments}}`

const infraComponentTmpl = `{{.Substrate.InfraComponentYAML}}`

const containerClusterTmpl = `apiVersion: bootwright.io/v1alpha1
kind: ContainerCluster
metadata:
  name: {{.Cluster}}
spec:
  distribution:
    release:
      version: 4.21.15

  install:
{{.Substrate.PlatformYAML}}
    endpoints:
{{.Substrate.EndpointsYAML}}
{{.Substrate.ClusterAgentArtifactServer}}
    nodeSSH:
      keyPairRef: {{.Cluster}}-cluster-admin-ssh-key

  networking:
    clusterNetwork:
      - cidr: 10.128.0.0/14
        hostPrefix: 23
    serviceNetwork:
      - 172.30.0.0/16

  nodes:
    - name: master-0
      role: master                      # master | worker
      machineRef: {{.Cluster}}-master-0
`
