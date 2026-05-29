# Baremetal Redfish Fleet Postinstall Example

This example prepares two three-node OpenShift clusters from one bare-metal
Redfish inventory, then applies declarative post-install OpenShift
Virtualization and OpenShift GitOps extensions to both clusters.

The layout keeps fleet-wide selection in `environment.yaml`, shared services
and provider inventory in `shared/`, cluster-specific install intent in one
directory per cluster, and reusable extension intent in top-level
`ClusterExtension` resources:

| Path | Owns |
| --- | --- |
| `environment.yaml` | Fleet defaults, selected resources, secrets, cluster list, and extension resources |
| `shared/` | Service host, artifact server, machine network, and bare-metal inventory |
| `demo-ocp-a/` | `demo-ocp-a` cluster infrastructure and container cluster intent |
| `demo-ocp-b/` | `demo-ocp-b` cluster infrastructure and container cluster intent |
| `extensions/cluster-extension-*.yaml` | OpenShift Virtualization and OpenShift GitOps OLM extension intent |
| `extensions/cluster-extension-set.yaml` | Ordered platform bootstrap extension group |
| `cluster-extension-binding.yaml` | Binding that applies the platform bootstrap set to both clusters |

Both clusters share `fleet-machine-net`, `artifact-server`, and
`fleet-baremetal-provider`. Each cluster selects distinct machines, external
VIPs, and static node addresses.

The extension phase runs after each cluster install is complete. OpenShift
Virtualization uses the documented OpenShift 4.21-compatible `stable` channel,
which selects the supported 4.21 OpenShift Virtualization stream. OpenShift
GitOps uses the specific `gitops-1.20` channel.
