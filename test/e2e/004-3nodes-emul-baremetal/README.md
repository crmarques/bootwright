# 004 Three-Node Emulated Bare Metal

Three-node compact-control-plane OpenShift fixture using explicit bare-metal
machine inventory and emulated support services.

`provider.yaml` owns BMC endpoints, physical NIC MACs, boot MACs, and root
device hints for the three servers. `cluster-infra.yaml` selects those machines,
assigns per-node addresses, and declares the artifact service used for Redfish
virtual-media boot.

Cluster endpoints use `externalVip`; the operator-owned external load
balancer and DNS are outside this fixture.
