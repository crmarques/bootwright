# 005 Three-Node Bare Metal

Three-node compact-control-plane OpenShift fixture for real bare-metal hosts.

`provider.yaml` owns BMC endpoints, two physical NIC MACs per server, boot
MACs, and root device hints. `networks.yaml` bonds `eno1` and `eno2`, creates a
VLAN interface on the bond, and leaves per-node machine-network IPs to
`cluster-machines.yaml` overlays. `infra-component.yaml` declares a
bastion-hosted artifact server so Redfish virtual media can fetch the
generated agent ISO from the bastion.

The case uses external proxy and DNS services. It does not declare managed
proxy, DNS, or load-balancer components. API and ingress endpoints omit
`source.type`, so `ContainerCluster.spec.install.endpointRefs` treats them as
OpenShift-managed and the installer uses the default keepalived and HAProxy
behavior.
