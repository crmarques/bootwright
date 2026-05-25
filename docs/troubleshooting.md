---
title: Troubleshooting
description: Common validation and render failures.
---

# Troubleshooting

## Strict Decode Failures

Bootwright rejects unknown fields before normalization. If an abandoned field is
present, update the object to the current schema instead of expecting a
compatibility rewrite.

Current placement:

- provider physical facts belong in `InfraProvider.spec`
- machine network templates belong in `NetworkConfig`
- selected machines and host IP overlays belong in
  `ClusterInfra.spec.components.machines[]`
- cluster release belongs in `ContainerCluster.spec.distribution`, and install
  mode belongs in `ContainerCluster.spec.install.mode`
- node bindings belong in `ContainerCluster.spec.nodes[]`

## Reference Failures

Every `ContainerCluster.spec.nodes[]` entry must resolve to one selected
machine:

```yaml
machineRef:
  clusterInfra: prod-3node-infra
  name: master-0
```

The target must exist as
`ClusterInfra.spec.components.machines[].name`. In v1 all nodes in one cluster
must reference the same `ClusterInfra`.

## Address Failures

Endpoint VIPs and machine address overlays are checked against selected
`NetworkConfig.spec.machineNetwork[]` CIDRs. Select the correct machine network
through `ClusterInfra.spec.components.machines[].networkConfig.ref`, or fix
either the CIDR template or the host-specific IP.

A `machineNetwork[].cidr` may appear in only one `NetworkConfig`. If two
templates use the same CIDR, split the host-specific NMState into one owning
template or change one CIDR.

## Proxy And Registry Conflicts

An external environment proxy URL conflicts with a managed cluster proxy
component. The same single-source rule applies to external mirror URLs and
managed registry components.

Disconnected installs require mirror trust material and one mirror endpoint
source.

## OpenShift And OKD Release Failures

OpenShift requires a pull secret reference and either an exact version or a
release image. OKD may omit a Red Hat pull secret, but should use an explicit
OKD release image for reproducible installs.

## Active Apply Run

If an apply fails with an active-run message, inspect the current ledger:

```text
bootwright status --watch
```

Start a new apply only after the previous run reaches a terminal state. If the
previous Bootwright process exited without updating the ledger, `status` reports
a stale lease and the next `apply` or `destroy` marks that run cancelled before
continuing.

## SSH Or Artifact Fetch Failures

`check infra` and `apply infra` require SSH to provider/service hosts. Validate
the same key and address declared on the `Host` before retrying.

Real BMCs must also reach the generated artifact HTTPS route used for the agent
ISO. If Redfish virtual media insert fails after the controller can download
the ISO, verify reachability from the BMC network and prefer an IP-address
`redfishVirtualMedia.addressName`.

## Context Input Looks Stale

`context init` imports files into
`/var/lib/bootwright/contexts/<context>/input-files/`. If you edited the source
directory after import and only want to refresh inputs, rerun:

```text
bootwright context update <context-name> -f <input-dir>
```
