# 001 SNO Libvirt

Single-node OpenShift fixture using a libvirt machine profile with Redfish BMC
emulation.

Key files:

| File | Kind |
| --- | --- |
| `environment.yaml` | `Environment` |
| `service-machines.yaml` | `Machine` |
| `provider.yaml` | `InfraProvider` |
| `infra-component.yaml` | `InfraComponent` |
| `networks.yaml` | `NetworkConfig` |
| `cluster-machines.yaml` | `Machine` |
| `container-cluster.yaml` | `ContainerCluster` |

The cluster node binds to `Machine[master-0]`, which
uses the `sno-bridge` network template and a per-node static IP overlay.
Cluster endpoints use `source.type=infraComponent` and resolve to the managed
HAProxy `InfraComponent/load-balancer` bind addresses.
