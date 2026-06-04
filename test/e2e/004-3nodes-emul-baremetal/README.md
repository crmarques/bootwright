# 004 Three-Node Emulated Bare Metal

Three-node compact-control-plane OpenShift fixture using explicit bare-metal
machine inventory and emulated support services.

`provider.yaml` owns BMC endpoints, physical NIC MACs, boot MACs, and root
device hints for the three servers. `cluster-machines.yaml` selects those machines
and assigns per-node addresses. `infra-component.yaml` declares the artifact
service used for Redfish virtual-media boot.

Cluster endpoints use `source.type=external`; the operator-owned external
load balancer and DNS are outside this fixture.
