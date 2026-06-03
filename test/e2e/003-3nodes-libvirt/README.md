# 003 Three-Node Libvirt

Three-node compact-control-plane OpenShift fixture using one libvirt machine
profile and three selected cluster machines.

Each `ContainerCluster.spec.nodes[]` entry binds a hostname and role to a
`ClusterInfra.components.nodes[]` entry. The nodes reuse the
`cluster-3n-bridge` `NetworkConfig` template and override only their static
addresses.

Cluster endpoints use `source.type=infraComponent` and resolve to the managed
HAProxy `InfraComponent/load-balancer` bind addresses.
