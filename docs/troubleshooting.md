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
- per-machine install-network addresses and overrides belong in
  `Machine.spec.network.config`
- cluster release belongs in `ContainerCluster.spec.distribution`, and install
  mode belongs in `ContainerCluster.spec.install.mode`
- node bindings belong in `ContainerCluster.spec.nodes[]`

## Validation Diagnostics

`bootwright validate` reports desired-state validation failures by owning
object and field. JSON output includes a `diagnostics[]` array for CI:

```json
{
  "object": "ContainerCluster/prod-3node",
  "field": "spec.networking.clusterNetwork[0].cidr",
  "value": "10.128.0.0",
  "message": "ContainerCluster/prod-3node spec.networking.clusterNetwork[0].cidr \"10.128.0.0\" is not a valid CIDR"
}
```

Fix the named field in the authored YAML, then rerun `bootwright validate`.

## Reference Failures

Every `ContainerCluster.spec.nodes[]` entry must reference one selected
`Machine` through `machineRef`:

```yaml
nodes:
  - hostname: master-0
    role: master
    machineRef:
      name: prod-3node-master-0
```

The referenced `Machine` must be selected by the `Environment`, carry the
`openshift-node` capability, and set `spec.os.provided: false`.

## Address Failures

Endpoint VIPs and machine address overrides are checked against selected
`NetworkConfig.spec.machineNetwork[]` CIDRs. Select the correct machine network
through `Machine.spec.network.config.networkConfigRef`, or
fix either the CIDR template or the node-specific IP.

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

`preflight infra` and `apply --stage infra` require SSH to provider/service hosts. Validate
the same key and address declared on the `Machine` before retrying.

Real BMCs must also reach the generated artifact HTTPS endpoint used for the
agent ISO. If Redfish virtual media insert fails after the bastion can download
the ISO, verify reachability from the BMC network and prefer an IP-address
`InfraComponent.spec.artifactServer.endpoints[]` entry selected by
`ContainerCluster.spec.install.artifactAccess.redfishVirtualMedia.endpointRef.name`.

## Context Input Looks Stale

`context init` imports files into
`/var/lib/bootwright/contexts/<context>/input/`. If you edited the source
directory after import and only want to refresh inputs, rerun:

```text
bootwright context update <context-name> -f <input-dir> --yes
```

Omit `--yes` to review and confirm the input replacement interactively.

## Resources No Longer In Desired State (Orphans)

`apply` never deletes — it only creates and converges what desired state declares. If
you remove an object (a `Machine`, an `InfraProvider`, a cluster, …) from desired state
*without* destroying it first, the live resource keeps running. It is not lost: Bootwright
still owns it through its ownership records, so a full `bootwright destroy` reclaims it
(destroy is ownership-record driven and rebuilds its inventory from those records, so it
can reach a provider host even after that host was removed from desired state).

To find such orphans, run `bootwright state-check`: resources owned by Bootwright but no
longer declared are listed under **"Owned but no longer declared"** (and as `undeclared`
in `--output json`). `bootwright destroy --dry-run` shows the same. To resolve one, either
re-declare the object and re-apply, or run a full `bootwright destroy` to reclaim it.
Bootwright deliberately does not prune on `apply`: a stray desired-state edit must never
silently tear down running infrastructure.
