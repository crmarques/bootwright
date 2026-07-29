---
title: Troubleshooting
description: >-
  Common validation, render, apply-run, host-trust, secret, and orphan
  failures, and the authored field or command that resolves each.
---

# Troubleshooting

This page groups the failure classes Bootwright surfaces by symptom and points
each one at the owning YAML field or the command that resolves it. For the
deeper recovery workflows (rebuilds, `--mode rebuild` paths, destroy-protection)
see [Operations and Recovery](advanced/operations.md).

!!! tip "Where the logs live"
    Per-run, per-task, and per-cluster logs are written under the context
    state directory at `/var/lib/bootwright/contexts/<context>/runs/`. When a
    task fails, that is the first place to look — the terminal shows a summary,
    but the detailed tool output (Ansible, `openshift-install`, `cephadm`) is
    kept in those root-managed logs rather than streamed. Credential-handling
    tasks appear as `censored due to no_log` in both the terminal and the log —
    see
    [Surfacing redacted output with `--verbose`](advanced/operations.md#surfacing-redacted-output-with-verbose)
    before reaching for `-v`. `destroy --purge-history`
    deletes a torn-down component's share of these logs (see
    [Operations and Recovery](advanced/operations.md#leaving-no-trace-of-a-destroyed-component)),
    so skip it while a failure is still under investigation.

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
- node bindings belong in `ContainerCluster.spec.nodes[]`

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

Every `ContainerCluster.spec.nodes[]` entry must reference one selected
`Machine` through `machineRef`:

```yaml
  nodes:
  - name: master-0
    role: master
    machineRef: prod-3node-master-0
```

!!! warning "Use `nodes:`, not `hosts:`"
    The list key is `nodes:`. A `hosts:` paste fails strict decode — the schema
    field is `ContainerCluster.spec.nodes[]`.

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
reach for `--mode rebuild` or `destroy`; those are for drift and rebuilds, not a
resumable interruption.

Run `bootwright status`: on a failed run it prints the exact scoped retry command
(for example `bootwright apply --clusters <name> --yes`) and the log path of each
failed task under `/var/lib/bootwright/contexts/<context>/runs/history/<run-id>/`.
Read that log, fix the cause, then re-run the printed command — completed objects
skip or re-run idempotently, and only the failed and pending work runs again.

See [Ownership and Safety](advanced/ownership-and-safety.md) for why the rerun is
safe, and [Operations and Recovery](advanced/operations.md) only for the drift
and `--mode rebuild` cases.

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

## A run takes far longer than expected

A run that succeeds but takes too long is a timing question, not a failure one.
`bootwright status --timings` (add `--run <runID>` for a finished run, `--output
json` for CI) reports each task's duration, how long it waited for a free slot,
what it was blocked on, and the run's **critical path** — the longest dependency
chain, which is the number to compare between runs. Set
`BOOTWRIGHT_ANSIBLE_PROFILE=1` on the next run to get per-Ansible-task durations
inside the slow task. See
[Timing a run and reading its critical path](advanced/operations.md#timing-a-run-and-reading-its-critical-path)
for the report and
[Tuning apply and destroy concurrency](advanced/operations.md#tuning-apply-and-destroy-concurrency)
for the caps — notably the per-host cap of 4, which throttles concurrent virtual
machine creation on a single hypervisor.

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

## A Ceph node refuses passwordless sudo

Two different refusals on a Ceph topology node read alike. Both leave the node
otherwise untouched and both are resolved by fixing the node and re-applying.

**Before the account is created**, apply reports that the login it connected
with "cannot run a privileged command", and no account was created, no sudoers
file written, root SSH untouched. Bootwright proves it can escalate with the
login it is borrowing — `bootwright` on a node the cluster installs, the
machine's own `access.ssh` identity on an `os.provided: true` node — before it
creates anything. A `sudo: sorry, you must have a tty to run sudo` policy on
that login is **not** the cause and needs no change: Bootwright retries with a
terminal and continues. The refusal means neither attempt worked.

- **`rc=255` with a failed PTY allocation** — `sshd` sets `PermitTTY no` for
  that account. Set `PermitTTY yes` in an `/etc/ssh/sshd_config.d/` drop-in and
  reload `sshd`.
- **Any other failure** — the login has no passwordless sudo. Grant it out of
  band; Bootwright writes sudo policy only for the account it owns.

**After the account is created**, apply reports that the node "did not accept
`<user>@<address>` with passwordless sudo after the account was provisioned",
with root SSH not yet revoked and still reachable. That proof runs
`sudo -n true` on a terminal-less channel deliberately — it is how cephadm's
manager runs — so a policy that reaches the account only from an interactive
session fails here by design. If `/etc/sudoers.d/60-bootwright-<user>` is
present, 0440 `root:root`, and correct, the node is not reading it — a
later-sorting file in `/etc/sudoers.d`, a `Defaults requiretty` placed after
`@includedir` in `/etc/sudoers`, or an LDAP/SSSD `cn=defaults` carrying
`ignore_local_sudoers` that makes sudo skip local files entirely. The normative
statement of all three is
[specs/security.md § Node Login Identity and Privilege](https://github.com/crmarques/bootwright/blob/main/specs/security.md).

Two commands tell them apart: the `sudoers:` line in `/etc/nsswitch.conf` shows
whether policy comes from a directory at all, and `sudo -ll -U <user>` shows
which policy actually wins.

```bash
grep '^sudoers:' /etc/nsswitch.conf
sudo -ll -U cephadm
```

See [Storage → The Ceph node login](concepts/storage.md#the-ceph-node-login) and
[What apply does, in order](concepts/storage.md#what-apply-does-in-order).

## Install never completes

An agent-based OpenShift/OKD install (or a Ceph bootstrap) that boots the nodes
but never reaches a ready cluster is almost always an environment-reachability
gap. Two classes are fixable in the authored YAML:

- **Endpoint DNS or VIP not reachable.** The agent bootstrap converges only once
  the API, api-int, and ingress endpoints resolve and their virtual IPs answer.
  Confirm the cluster endpoints (`ContainerCluster.spec.install.endpoints`, in
  the labs sourced from the load-balancer component), the published VIPs
  (`InfraComponent.spec.loadBalancer.bindAddresses[]`), and the name-resolution
  records all agree, and that external DNS carries A records for the API/apps
  VIPs, an A record for each machine's `fqdn` name, and a CNAME from each
  node FQDN to its machine's `fqdn` (see
  [Networking](advanced/networking.md#name-resolution)). The **Name
  resolution** preflight group checks the machine and node records before
  apply and names the exact record to create; a missing or wrong endpoint
  record leaves the install waiting indefinitely.
- **Disconnected trust or mirror material missing.** A disconnected or proxied
  install stalls when the nodes cannot pull release content: the mirror endpoint
  is unreachable, its CA/trust bundle is absent, or the proxy selection is wrong.
  Provide one mirror endpoint source and its trust material, and confirm the
  proxy and `no_proxy`/CIDR bypass selection reach the mirror and the release
  upstreams — see
  [Disconnected & proxied installs](advanced/disconnected-proxy.md).

Watch progress with `bootwright status --watch`; the per-task logs under
`/var/lib/bootwright/contexts/<context>/runs/` show which host or endpoint the
install is waiting on.

## Add-on (ClusterAddon) apply failures

A `ClusterAddon` apply (an OLM operator install, its steps, or a manifest-only
attachment) can fail at several distinct gates; `bootwright status` names which
one and the exact detail last observed, and the full command/tool output for
that task is in its log under
`/var/lib/bootwright/contexts/<context>/runs/history/<run-id>/tasks/`.

- **`CatalogSource/... did not reach connectionState READY`** — the add-on
  ships its own `spec.olm.catalogSource` (for example `fusion-data-foundation`)
  and the registry pod never reported `READY`. Usually the catalog image
  cannot be pulled: confirm the image reference and tag exist, and that the
  cluster's pull secret (or the add-on's `globalPullSecretMerge` input, e.g.
  `ibm-entitlement`) actually carries credentials for that registry. A
  `TRANSIENT_FAILURE` with a pod-security admission error means the catalog
  also needs the matching
  `catalogSource.grpcPodConfig.securityContextConfig`; the bundled IBM Fusion
  Data Foundation add-on declares `restricted`.
- **`Subscription/... operator CSV did not reach Succeeded`** — the catalog
  resolved but the operator's ClusterServiceVersion never installed. Check the
  Subscription's `installPlanApproval` (a `Manual` plan needs approving) and
  the operator namespace's events/pod status for image pull or RBAC errors.
- **`step "<name>" (<lifecycle>) failed`** — a `spec.steps` playbook or
  manifest apply failed. The task log contains the full Ansible or `oc apply`
  output; a step whose `target.fromInput` resolves to a Ceph `StorageExport`
  needs the referenced `StorageCluster` reachable over SSH (or, for external
  Ceph, needs `externalDetails.fromSecretRef` rather than a playbook — `bootwright
  validate` rejects the mismatched combination up front). A step target machine
  with no `spec.access.ssh` is rejected the same way, before any apply runs.
- **`step %s target machine %s has no resolvable SSH access`** — the step's
  resolved target machine has no reachable SSH address; point the step at a
  machine that configures `spec.access.ssh`, or use a `boundCluster`/
  `static.clusters` target instead of a bare machine.

The per-add-on apply record (status, phase, and the same last-observed detail)
also lives on disk at
`/var/lib/bootwright/contexts/<context>/clusters/<cluster>/runtime/addons/<addon>.json`,
independent of the run history, if you need to inspect it outside of `status`.

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
    [Ceph topologies](advanced/ceph-topologies.md)). Removing a `ClusterAddon`
    from a `ClusterAddonBinding` (or deleting the binding) is the same kind of
    gap: the add-on's installed operator, custom resources, and step-applied
    manifests are not tracked as orphans and are not torn down by `apply` or
    `diff` — remove them on the cluster directly (`oc delete`), or destroy the
    whole `ContainerCluster` to reclaim everything at once.

## Ceph disk-space alerts flap after install

Shortly after a managed Ceph install the dashboard starts emitting paired
`CephNodeDiskspaceWarning` notifications — the same nodes go *active* and then
*resolved* within a scrape or two:

```
CephNodeDiskspaceWarning (active)
  Mountpoint / on node-02... will be full in less than 5 days
CephNodeDiskspaceWarning (resolved)
  Mountpoint / on node-02... will be full in less than 5 days
```

Two things combine to produce that pattern. Ceph ships the rule as

```
predict_linear(node_filesystem_free_bytes{device=~"/.*"}[2d], 3600 * 24 * 5) < 0
```

with **no `for:` clause** — unlike the neighbouring `CephNodeRootFilesystemFull`,
which holds `for: 5m`. A single evaluation where the projection dips below zero
fires the alert, and the next evaluation resolves it, so every wobble in the
trailing fill rate becomes an active/resolved pair. And the projection is a
straight-line extrapolation of the last two days: right after an install the
root filesystem is genuinely ramping (container image pulls, the mon RocksDB
store growing through initial peering, the Prometheus TSDB filling toward its
retention window, the journal growing to its cap), so the rule extrapolates a
*bounded* ramp as if it ran forever.

The alert only fires at all when the projected five-day consumption exceeds the
free space — roughly `free < 5 × daily fill rate`. So the flapping is a real
signal about headroom, not pure noise. Check the actual margin on every node —
repeat these per storage node, or script them over `bootwright machine list`:

```bash
# free space and the biggest consumers on the Ceph state filesystem
bootwright cluster exec --name <storage-cluster> --node <node> -- \
  sudo df -h / /var/lib/ceph
bootwright cluster exec --name <storage-cluster> --node <node> -- \
  sudo du -xsh /var/lib/ceph /var/lib/containers

# journal size against its cap
bootwright cluster exec --name <storage-cluster> --node <node> -- \
  sudo journalctl --disk-usage
```

If the nodes are comfortably empty (tens of GiB free and falling by megabytes a
day), the alerts are the post-install ramp and stop on their own once the
cluster settles and the two-day window fills with steady-state samples.

If free space is in the single-digit-to-low-double-digit GiB range, the
filesystem is genuinely undersized for the roles the node carries — see the
[node root-filesystem budget](concepts/storage.md#node-root-filesystem-budget).
Remedies, in order of preference:

- Give the node a larger root disk. `Machine.spec.os.install.rootDeviceHints`
  picks the install disk and the profile's
  `customizations.storage.rootDevice.source: machineRootDeviceHints` makes
  Anaconda use it — see [Managed OS installs](advanced/managed-os.md).
  Reinstalling against a larger disk is the durable fix.
- Lower `spec.ceph.monitoring.prometheus.retentionSize` (Bootwright defaults it
  to `10GB`) and re-apply.
- Move the `prometheus`, `grafana`, `alertmanager` or `loki` placements onto
  nodes with headroom, or off the `mon` nodes.
- Cap the journal, which defaults to 10% of the filesystem (max 4 GiB):
  `journalctl --vacuum-size=1G` now, and
  `SystemMaxUse=1G` in `/etc/systemd/journald.conf.d/` to hold it there.

`bootwright preflight` fails below 20 GiB free and warns below the
per-node budget, so a re-run reports which nodes are short.

## Recovering the Ceph dashboard password

`cephadm bootstrap` generates a one-time random `admin` password for the Ceph
dashboard, which Bootwright captures only during the install (see
[Ceph topologies](advanced/ceph-topologies.md#accessing-a-managed-cluster)). If the
stored `dashboard-password` file is lost, or the in-cluster password was changed
and no longer matches the stored copy, reset it directly on the cluster through
`cephadm shell` — the containerized Ceph client that ships with cephadm, so no
host `ceph` package is required:

```bash
# SSH to the seed node (the SSH line from cluster info)
ssh root@192.168.134.20

# Set a new admin password. Modern Ceph requires the password to be supplied
# from a file via -i (a positional password argument is rejected), and enforces
# a policy: at least 8 characters and not a common word. Feed it on stdin so no
# plaintext file is left on disk:
printf 'NewStr0ngPassw0rd' | \
  sudo cephadm shell -- ceph dashboard ac-user-set-password admin -i -

# Or mount a host file into the shell instead of using stdin:
#   umask 077
#   printf 'NewStr0ngPassw0rd' > /tmp/dash-pass
#   sudo cephadm shell -m /tmp/dash-pass:/tmp/dash-pass -- \
#     ceph dashboard ac-user-set-password admin -i /tmp/dash-pass
#   rm -f /tmp/dash-pass

# confirm the dashboard URL the active mgr is serving
sudo cephadm shell -- ceph mgr services
```

To keep `bootwright cluster info` accurate, write the same value back to the
stored file on the controller:

```bash
P=/var/lib/bootwright/contexts/<ctx>/clusters/ceph-libvirt/secrets/dashboard-password
printf 'NewStr0ngPassw0rd' | sudo tee "$P" >/dev/null
sudo chmod 0600 "$P"
```

A clean reinstall (`bootwright apply ... --mode rebuild`, which clears `/etc/ceph`
and re-bootstraps) re-captures a fresh dashboard password into the stored file
automatically.
