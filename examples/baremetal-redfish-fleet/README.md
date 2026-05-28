# Baremetal Redfish Fleet Example

This example prepares two three-node OpenShift clusters from one bare-metal
Redfish inventory.

The layout keeps fleet-wide selection in `environment.yaml`, shared services
and provider inventory in `shared/`, and cluster-specific install intent in
one directory per cluster:

| Path | Owns |
| --- | --- |
| `environment.yaml` | Fleet defaults, selected resources, secrets, and cluster list |
| `shared/` | Service host, artifact server, machine network, and bare-metal inventory |
| `demo-ocp-a/` | `demo-ocp-a` cluster infrastructure and container cluster intent |
| `demo-ocp-b/` | `demo-ocp-b` cluster infrastructure and container cluster intent |

Both clusters share `fleet-machine-net`, `artifact-server`, and
`fleet-baremetal-provider`. Each cluster selects distinct machines, external
VIPs, and static node addresses.
