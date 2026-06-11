# 007 SNO vSphere

Single-node OpenShift fixture using a vCenter-managed vSphere machine
profile. The VM is created blank through the vCenter API, the generated
agent ISO is uploaded to the staging datastore folder, and the machine
boots it from a SATA CD-ROM with disk-first EFI boot order.

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

The cluster node binds to `Machine[master-0]`, which selects the `sno`
machine profile on `InfraProvider/lab-vsphere-provider` and places it on
failure domain `dc1-zone-a`. Cluster endpoints use
`source.type=infraComponent` and resolve to the managed HAProxy
`InfraComponent/load-balancer` bind addresses. `vcenter-credentials` is a
`user:password` secret for the declared vCenter; the lab vCenter uses a
self-signed certificate, so the provider opts out of verification per
vCenter with `disableCertificateVerification: true`.

Running against a live vCenter requires the declared datacenter, compute
cluster, datastore, folder, resource pool, and portgroup to exist; the
machine's static IP must be reachable from the bastion for SSH readiness
probing.
