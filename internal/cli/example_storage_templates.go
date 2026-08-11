package cli

const storageEnvironmentTmpl = `apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata:
  name: {{.Cluster}}
spec:
  domains:
    base: example.test              # change to a domain you own
`

const storageSecretsTmpl = `apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: {{.Cluster}}-cluster-ssh
spec:
  type: sshKeyPair

  source:
    generated:
      comment: bootwright-{{.Cluster}}-cluster
---
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: {{.Cluster}}-node-ssh
spec:
  type: sshKeyPair

  source:
    file:
      privateKey: ~/.ssh/bootwright-ssh-key   # change to the key that already logs in to the nodes
`

const storageClusterTmpl = `apiVersion: bootwright.io/v1alpha1
kind: StorageCluster
metadata:
  name: {{.Cluster}}
spec:
  type: ceph

  management: managed

  ceph:
    distribution: oss                 # oss | redhat | ibm - redhat and ibm need an Entitlement

    release: "{{.Release}}"

    community:
      mirror: https://download.ceph.com

    cephadm:
      addressRef: ssh
      clusterSSH:
        keyRef: {{.Cluster}}-cluster-ssh
      bootstrap:
        node: node-01

    networks:
      publicCIDRs:
        - 192.168.140.0/24            # change to your storage network
      clusterCIDRs:
        - 192.168.140.0/24

    topology:
      # every path under devices[] is wiped when its OSD is created
      nodes:
{{- range .Nodes}}
        - name: {{.Node}}
          machineRef: {{.Machine}}
          roles:
{{- range .Roles}}
            - {{.}}
{{- end}}
          devices:
{{- range .Devices}}
            - {{.}}
{{- end}}
{{- end}}
`

const storageMachineTmpl = `apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: {{.Node.Machine}}
spec:
  capabilities:
    - ceph-node

  os:
    provided: true

  addresses:
    - name: ssh
      address: {{.Node.Address}}        # change to this node's reachable address

  access:
    ssh:
      user: root
      addressRef: ssh
      auth:
        privateKeyRef: {{.Cluster}}-node-ssh
`
