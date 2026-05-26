# 001 SNO Libvirt

Single-node OpenShift fixture using a libvirt machine profile with Redfish BMC
emulation.

Key files:

| File | Kind |
| --- | --- |
| `environment.yaml` | `Environment` |
| `hosts.yaml` | `Host` |
| `provider.yaml` | `InfraProvider` |
| `infra-component.yaml` | `InfraComponent` |
| `networks.yaml` | `NetworkConfig` |
| `cluster-infra.yaml` | `ClusterInfra` |
| `container-cluster.yaml` | `ContainerCluster` |

The cluster node binds to `ClusterInfra.components.machines[master-0]`, which
uses the `sno-bridge` network template and a per-host static IP overlay.
Cluster endpoints use `providedBy` and resolve to the managed HAProxy
`InfraComponent/load-balancer` bind addresses.
