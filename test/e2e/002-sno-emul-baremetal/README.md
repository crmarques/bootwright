# 002 SNO Emulated Bare Metal

Single-node OpenShift fixture using explicit bare-metal machine inventory and
emulated service hosts.

Physical facts such as boot MAC, interface MAC, BMC address, and root-device
hints live in `provider.yaml`. The selected cluster machine and static IP live
in `clusterinfra.yaml`.

The desired state uses `ClusterInfra.spec.platform.type: baremetal` with
`provisioningNetwork: disabled` to describe the machine-control path. Because
this is a single-node cluster, the rendered installer input uses
`platform.none`.

Cluster endpoints use `externalVip`; the operator-owned external load
balancer and DNS are outside this fixture.
