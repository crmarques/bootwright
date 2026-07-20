---
title: Troubleshooting
description: >-
  Common validation, render, apply-run, host-trust, secret, and orphan
  failures, and the authored field or command that resolves each.
---

# Troubleshooting

This page groups the failure classes Bootwright surfaces by symptom and points
each one at the owning YAML field or the command that resolves it. For the
deeper recovery workflows (rebuilds, `--converge-drifted` paths, destroy-protection)
see [Operations and Recovery](advanced/operations.md).

!!! tip "Where the logs live"
    Per-run, per-task, and per-cluster logs are written under the context
    state directory at `/var/lib/bootwright/contexts/<context>/runs/`. When a
    task fails, that is the first place to look — the terminal shows a summary,
    but the detailed tool output (Ansible, `openshift-install`, `cephadm`) is
    kept in those root-managed logs rather than streamed.

!!! warning "Edits need `context update`"
    Once you have run `context init`, the context holds a **copy** of your input
    tree. Editing the source has no effect until you refresh it with
    `bootwright context update --name <context> -f <dir>`, so run that after any
    fix before you re-run `preflight`, `apply`, or `diff`. `bootwright
    validate -f <dir>` reads the source directly and needs no refresh.

## Strict decode failures

Bootwright rejects unknown fields before normalization. If an abandoned field
is present, update the object to the current schema instead of expecting a
compatibility rewrite — `v1alpha1` carries no aliases or migrations.

Current placement:

- substrate capabilities, machine profiles, provider connection facts, and
  network attachments belong in `InfraProvider.spec`
- machine network templates belong in `NetworkConfig`
- per-machine install-network addresses and overrides belong in
  `Machine.spec.network.config`
- cluster release belongs in `ContainerCluster.spec.distribution`, and install
  mode belongs in `ContainerCluster.spec.install.mode`
- node bindings belong in `ContainerCluster.spec.hosts[]`

## Validation diagnostics

`bootwright validate` reports desired-state validation failures by owning
object and field. JSON output includes a `diagnostics[]` array for CI:

```json
{
  "object": "ContainerCluster/prod-3node",
  "field": "spec.networking.clusterNetwork[0].cidr",
  "value": "10.128.0.0",
  "message": "ContainerCluster/prod-3node spec.networking.clusterNetwork[0].cidr \"10.128.0.0\" is not a valid CIDR",
  "remediation": "Set a valid CIDR such as 10.128.0.0/14."
}
```

Each entry may also carry a `rule` identifier; `remediation` (when present) is
the most actionable field. Fix the named field in the authored YAML, then rerun
`bootwright validate`.

!!! note
    `validate` is offline — it never contacts hosts, BMCs, or clusters. Use
    `bootwright preflight <target>` for live read-only checks.

## Reference failures

Every `ContainerCluster.spec.hosts[]` entry must reference one selected
`Machine` through `machineRef`:

```yaml
  hosts:
  - hostname: master-0
    role: master
    machineRef: prod-3node-master-0
```

!!! warning "Use `hosts:`, not `nodes:`"
    The list key is `hosts:`. A `nodes:` paste fails strict decode — the schema
    field is `ContainerCluster.spec.hosts[]`.

The referenced `Machine` must be selected by the `Environment`, carry the
`openshift-node` capability, and set `spec.os.provided: false`.

References elsewhere are plain name strings on a `Ref`/`Refs`-suffixed field;
the `{name: ...}` object form is rejected.

## Address failures

Endpoint VIPs and machine address overrides are checked against selected
`NetworkConfig.spec.machineNetwork[]` CIDRs. Select the correct machine network
through `Machine.spec.network.config.networkConfigRef`, or fix either the CIDR
template or the node-specific IP.

A `machineNetwork[].cidr` may appear in only one `NetworkConfig`. If two
templates use the same CIDR, split the host-specific NMState into one owning
template or change one CIDR.

!!! note
    Rendering is a second enforcement line: a render entry point fails before
    writing anything when an endpoint load-balancer bind or a managed Ceph host
    address does not resolve, rather than emitting output with empty values.

## Proxy and registry conflicts

An external environment proxy URL conflicts with a managed cluster proxy
component. The same single-source rule applies to external mirror URLs and
managed registry components.

Disconnected installs require mirror trust material and one mirror endpoint
source.

## OpenShift and OKD release failures

OpenShift requires a pull secret reference and either an exact version or a
release image. OKD may omit a Red Hat pull secret, but should use an explicit
OKD release image for reproducible installs.

## Secret failures

Bootwright desired state references secrets by name only; the bytes live in the
context secret store or in operator files. Two common early-workflow blockers:

- **Missing declared secrets.** `bootwright secret generate` converges the
  declared `Secret` objects: it creates `source.generated` material and copies
  `source.file` sources into the encrypted context store (in `context` storage
  mode).
  `bootwright secret check` is the read-only gate: it reports any declared
  secret that is still missing and **exits non-zero while any declared secret
  remains absent**, so resolve the reported gaps before retrying a workflow that
  reads them.

- **Commands that would write credentials fail closed.** `bootwright render
  --output-dir <dir>` requires `--sensitive` because it writes secret-inlined
  installer files. Pass `--sensitive` only when you intend to materialize
  credentials, and keep the output local and unversioned.

!!! warning
    `secret set --generate` is a test-fixture path only. Generated material in
    real workflows comes from `secret generate`.

## Apply failed partway

When `apply` exits non-zero after the run has started, the work is resumable —
objects that already converged are recorded and skip on the next run. Do **not**
reach for `--converge-drifted` or `destroy`; those are for drift and rebuilds, not a
resumable interruption.

Run `bootwright status`: on a failed run it prints the exact scoped retry command
(for example `bootwright apply --clusters <name> --yes`) and the log path of each
failed task under `/var/lib/bootwright/contexts/<context>/runs/history/<run-id>/`.
Read that log, fix the cause, then re-run the printed command — completed objects
skip or re-run idempotently, and only the failed and pending work runs again.

See [Ownership and Safety](advanced/ownership-and-safety.md) for why the rerun is
safe, and [Operations and Recovery](advanced/operations.md) only for the drift
and `--converge-drifted` cases.

## Active apply run

If an apply fails with an active-run message, inspect the current ledger:

```text
bootwright status --watch
```

Start a new apply only after the previous run reaches a terminal state. The run
ledger and logs live under `/var/lib/bootwright/contexts/<context>/runs/`.

- **A still-live process** should be allowed to finish, or stopped deliberately,
  before you start another apply.
- **A stale lease** (the previous Bootwright process exited without updating the
  ledger) is reported by `status`; the next `apply` or `destroy` marks that run
  cancelled before continuing.

## SSH or artifact fetch failures

`preflight infra` and `apply --stage infra` require SSH to provider/service
hosts. Validate the same key and address declared on the `Machine` before
retrying. A machine used over SSH must declare `access.ssh.addressRef`,
`keyRef`, and a matching address.

If the host check fails with a missing SSH host-trust record (`fix: bootwright
machine trust`), either run `bootwright machine trust`, or rerun `preflight`/`apply`
interactively: they show each unknown host's key fingerprint and ask before
recording it (disable the prompt with `--trust-on-first-use=false`).
Non-interactive runs (`--yes`, `--output json`) never prompt and keep failing
closed until trust is pre-recorded; a `--dry-run` is read-only and never
records trust at all. A *changed* host key is never accepted at the prompt —
verify the new fingerprint and run `bootwright machine trust --replace <machine>`.

Real BMCs must also reach the generated artifact HTTPS endpoint used for the
agent ISO. If Redfish virtual media insert fails after the bastion can download
the ISO, verify reachability from the BMC network and prefer an IP-address
`InfraComponent.spec.artifactServer.endpoints[]` entry selected by
`ContainerCluster.spec.install.agent.redfishVirtualMedia.artifactServerEndpoint.endpointRef`.

If the BMC accepts the `InsertMedia` task but the task then ends in
`Exception`/`ConnectionFailed` ("Failed to connect to virtual media") — common
on BMCs such as Huawei iBMC — the BMC reached the listener but rejected the
HTTPS connection. The usual cause is the artifact server's **self-signed
certificate**: the BMC will not trust it, and the standard Redfish "skip
verification" toggles are unimplemented or ineffective on much firmware. Declare
the trust strategy with
`Machine.spec.hardware.management.bmc.virtualMedia.tls.trust` (or set it once on
the provider's `baremetal.defaults.bmc.virtualMedia.tls` to cover the whole
fleet):

- `trust: import-certificate` uploads the artifact server certificate into the
  BMC trust store before the fetch (and `removeCertificateAfterBoot: true`
  removes it once the ISO is mounted). Uses the Redfish VirtualMedia Certificates
  collection or the xFusion/Huawei iBMC `SecurityService.ImportRemoteHttpsServerRootCA`
  action. On xFusion/Huawei iBMC this needs the BMC account's **Security
  Configuration** right, otherwise HTTP 403 `InsufficientPrivilege`.
- `trust: disable-verification` (the default) asks the BMC to skip verifying the
  certificate for the fetch and restores verification afterwards unless
  `restoreVerificationAfterBoot: false` (best-effort; some firmware ignores it,
  and disabling an *enforcing* iBMC needs the same Security Configuration right).
- `trust: established` declares the BMC already trusts the artifact server
  (CA-signed certificate, pre-loaded root CA, or verification already off):
  bootwright performs **no** BMC security writes. This is the path for BMC
  accounts that lack the Security Configuration right.

Alternatively serve a BMC-trusted certificate (then declare
`trust: established`), or — if the failure is a TLS
*handshake* mismatch rather than trust — relax the listener with
`InfraComponent.spec.artifactServer.tls.minVersion`/`tls.ciphers` (see
[Artifact Server](concepts/infrastructure.md#artifact-server)).

## Resources no longer in desired state (orphans)

`apply` is additive: it creates and converges what desired state declares and
never deletes. If you remove an object (a `Machine`, an `InfraProvider`, a
cluster, …) from desired state *without* destroying it first, the live resource
keeps running. It is not lost — Bootwright still owns it through its ownership
records.

To find such orphans, run `bootwright diff`: resources owned by
Bootwright but no longer declared are listed under **"Owned but no longer
declared"** (and as `undeclared` in `--output json`). `bootwright destroy
--dry-run` shows the same set.

To resolve one, either re-declare the object and re-apply, or run a full
`bootwright destroy` to reclaim it — destroy is ownership-record driven, so it
can reach a provider host even after that host was removed from desired state.
See [Operations and Recovery](advanced/operations.md) for the full teardown and
reclamation workflow.

!!! note
    Orphan tracking is object-level. A Ceph sub-object (a `StoragePool`,
    `StorageFilesystem`, `StorageObjectGateway`, or `services[]` entry) deleted
    from a still-declared `StorageCluster` is not listed — the live pool or
    service simply keeps running. Remove it on the cluster with the
    `ceph`/`cephadm` CLI (see
    [Ceph topologies](advanced/ceph-topologies.md)).

## Recovering the Ceph dashboard password

`cephadm bootstrap` generates a one-time random `admin` password for the Ceph
dashboard, which Bootwright captures only during the install (see
[Ceph topologies](advanced/ceph-topologies.md#accessing-a-managed-cluster)). If the
stored `dashboard-password` file is lost, or the in-cluster password was changed
and no longer matches the stored copy, reset it directly on the cluster. The
`ceph` CLI is on the seed node's PATH after bootstrap, so no `cephadm shell` is
needed:

```bash
# SSH to the seed node (the SSH line from cluster info)
ssh root@192.168.134.20

# Set a new admin password. Modern Ceph requires the password to be supplied
# from a file via -i (a positional password argument is rejected), and enforces
# a policy: at least 8 characters and not a common word.
umask 077
printf 'NewStr0ngPassw0rd' > /tmp/dash-pass
sudo ceph dashboard ac-user-set-password admin -i /tmp/dash-pass
rm -f /tmp/dash-pass

# confirm the dashboard URL the active mgr is serving
sudo ceph mgr services
```

To keep `bootwright cluster info` accurate, write the same value back to the
stored file on the controller:

```bash
P=/var/lib/bootwright/contexts/<ctx>/clusters/ceph-libvirt/secrets/dashboard-password
printf 'NewStr0ngPassw0rd' | sudo tee "$P" >/dev/null
sudo chmod 0600 "$P"
```

A clean reinstall (`bootwright apply ... --converge-drifted`, which clears `/etc/ceph`
and re-bootstraps) re-captures a fresh dashboard password into the stored file
automatically.
