# SNO Libvirt Redfish Example

This is the smallest canonical seven-kind example for one OpenShift
single-node cluster on a libvirt provider with emulated Redfish BMC access.

Authored files:

```text
environment.yaml       fleet defaults, selected resources, and secret names
hosts.yaml             provider host reachability
provider.yaml          libvirt and Redfish emulator capabilities
infra-component.yaml   shared infra services
networks.yaml          machine network and NMState template
cluster-infra.yaml     selected machine and endpoints
container-cluster.yaml OpenShift install intent and node binding
```

Before importing this example into a real lab, edit these fields:

| File | Required first edits |
| --- | --- |
| `environment.yaml` | `spec.baseDomain`, declared secret names and local source files, and any proxy, mirror, artifact, DNS, or NTP selections |
| `hosts.yaml` | Provider/service host `spec.addresses[]`, SSH `addressName`, user, and key reference |
| `provider.yaml` | Libvirt host/profile values, BMC emulator reachability, and any machine sizing changes |
| `infra-component.yaml` | Load balancer and artifact service bind addresses, listeners, and endpoint names reachable by BMCs or cluster nodes |
| `networks.yaml` | `spec.machineNetwork[]`, NMState routes, DNS refs, and bridge/interface names |
| `cluster-infra.yaml` | Endpoint VIP ownership, selected machine profile, per-machine IP overlays, and root device hints |
| `container-cluster.yaml` | Distribution release, install mode, cluster/service networks, and node-to-machine bindings |

Generated context state, installer files, runtime output, and external
`render --output-dir` exports are not edit points. Change desired state here,
then rerun `bootwright context init <name> -f examples/sno-libvirt-redfish --yes`.
