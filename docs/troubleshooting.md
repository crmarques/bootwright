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

### Strict decode also gates `destroy`

`destroy` plans a teardown from the context's stored input plus the ownership
records, so it loads that input through the same strict decoder and the same
validator `apply` uses — before it reads a single ownership record. A context
whose stored input no longer matches the running binary's schema therefore
cannot be torn down until the input is schema-current, and the refusal covers
every object in the context, including ones the run would not have touched.
This is the routine consequence of rebuilding Bootwright while a lab is still
standing.

Two remedies, in order of preference:

1. Re-render or edit the input for the current schema, validate the new tree
   offline until it is clean, then adopt it and destroy:

    ```bash
    bootwright validate -f ./lab-input
    bootwright context update --name lab -f ./lab-input --yes
    bootwright destroy --authorize data-loss --yes
    ```

    The
   re-authored input must keep every applied identity — context, `Environment`,
   `InfraProvider`, `Machine`, `ContainerCluster`, `StorageCluster`, and node
   names — because teardown matches ownership records by those names. Dropping a
   declaration instead of migrating it turns its records into orphans, and only
   some kinds are reclaimable without one (see
   [Resources no longer in desired state](#resources-no-longer-in-desired-state-orphans)).
2. `destroy --authorize stale-input`, which skips the documents that cannot be
   decoded or validated and reports both them and every ownership record left
   without a declaration. `--dry-run` needs the token too, so the discovery path
   is `destroy --dry-run` (refuses, naming the token) → `destroy --dry-run
   --authorize stale-input` (preview the blast radius) → the real run. It relaxes
   no other gate — see
   [Ownership, idempotency & safety](advanced/ownership-and-safety.md#tearing-down-a-context-whose-input-no-longer-decodes).

`context delete --purge --abandon-resources` is not a teardown: it loads no
input, so it still runs, but it abandons whatever is still standing and deletes
the kubeconfigs and kubeadmin passwords you would need to clean up by hand.

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

Run `bootwright status`: on a failed run it prints the shell-quoted exact
invocation recorded when that run started, including `--context`, selection,
mode, effect flags, authorizations, SSH overrides, and whether you originally
passed `--yes`, plus the log path of each failed task under
`/var/lib/bootwright/contexts/<context>/runs/history/<run-id>/`. It never adds
`--yes` for you. Read that log, fix the cause, then re-run the printed command —
completed objects skip or re-run idempotently, and only the failed and pending
work runs again. A ledger written by an older build without exact argv produces
command-free guidance; recover the invocation from your shell or automation
history rather than widening it from the ledger's display label.

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

Preflight does not require SSH to a machine whose OS the same run installs. Such
a host is reported as deferred in the preflight log — Bootwright installs it in
the machines phase, which runs before the phase that logs in to it — and its
host checks run on the next preflight. Every other unreachable host fails
preflight by name, with the address and user it tried: nothing the run does
makes it reachable. A machine with `os.provided: true` and no `spec.access`
defaults to the controller identity as `root`, so its public key has to be in
that account's `authorized_keys` before the first run.

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
`/var/lib/bootwright/contexts/<context>/runs/history/<run-id>/clusters/<cluster>/`
(add-on steps under `addons/<addon>/`, everything else under `steps/<task-id>/`).

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

How far that reaches depends on the record's kind, because reclaiming a resource
without a declaration needs a control plane the teardown can still find on the
provider or infra host it is already talking to:

| Record kind | Reclaimed by a full `destroy` without its declaration? |
| --- | --- |
| `libvirt-domain`, `libvirt-network`, `managed-os-install` | Yes — the host-local record sweep removes them |
| `bmc-emulator`, `infra-component` | Yes — swept with the provider and infra-component services |
| `kubevirt-machine`, `vsphere-machine`, `vsphere-vmedia`, `controller-name-resolver`, `storage-cluster` | No — their control plane is reached only through the declaration |

For a kind in the last row, re-declare the object under its **original** names
and destroy it while it is declared. `destroy --dry-run` lists the record names
to match. Deleting the record file alone would strand the live resource.

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

## A Ceph command reaches its safety timeout

A bounded Ceph task now fails with a diagnostic like:

```text
Ceph state-changing command "Apply Ceph OSD service spec" on storage-0 exceeded
its 600-second safety timeout and was terminated (rc 124). The outcome is
unknown; Bootwright did not treat the timeout as evidence that the cluster is
absent, safe to change, or successfully changed. ... retry the exact resolved
invocation: `bootwright apply ...`.
```

Read the task log named beside the failure before retrying. A registry pull,
unreachable monitor, lost quorum, slow removal, or unhealthy host can all keep
`cephadm shell` alive; rc 124 means the finite bound expired, while rc 137 means
the child also needed the KILL escalation. A task carrying credentials or key
material deliberately relays only its name, timeout, exit code, and whether it
changes state, so its log may contain no Ceph stderr. Neither code is an
ownership or authorization refusal, so adding `--authorize` flags is not a
remedy.

For a state-changing task, fix the condition the log names and copy the exact
`bootwright ...` invocation printed by the failure. It preserves this run's
context, selection, stage/range, mode, credentials, effects, and accepted
authorizations. Bootwright treats the prior outcome as unknown and re-enters
the idempotency and safety gates; do not substitute a broader command. A
read-only probe timeout deliberately prints no state-changing retry command:
repair reachability or cluster health and repeat the original read-only
operation.

## A Ceph apply ends with zero OSDs

The apply bootstraps the cluster, brings up every monitor, manager and
monitoring daemon, and then fails closed at the OSD readiness check:

```text
Ceph cluster ceph-01 did not create the declared OSDs: expected at least 6
OSD(s) 'in', observed 0 (up 0) after 90 of 90 attempts. The poll also ends at a
1800s wall-clock deadline: an attempt count short of that budget means the
deadline is what stopped it, so cephadm was answering slowly rather than not at
all. ...
cephadm accepted the OSD spec, inventoried every declared OSD host, and refused
none of their declared devices — so this is NOT a device condition either. ...
Declared OSD services (running counts): osd.ceph-01 running=0 — a service
sitting at running=0 was accepted but never deployed, ...
Declared OSD device availability: cephadm inventoried all 3 declared OSD
host(s); of the 6 declared device(s) it did inventory, 0 are unavailable to
ceph-volume and 6 are available. ...
Orchestrator cache age: cephadm last refreshed between 2026-08-07T09:12:04 and
2026-08-07T09:14:41 UTC, which is 63s (1.1 min) before this verdict, against a
900s wait. ...
```

`ceph orch apply` registers the OSD drivegroups and returns immediately; cephadm
creates the OSDs asynchronously through ceph-volume, which refuses any device
that is not empty. Two unrelated failures end at this same check, and the failure
text already tells them apart. **Read the `Declared OSD device availability:`
line before touching a disk** — it counts how many of the declared devices
cephadm actually refused, and that count picks the branch below. The
`after N of M attempts` count says which limit ended the poll: `90 of 90` is the
attempt budget running out, while a first number short of the second means the
wall-clock deadline stopped it — cephadm was answering, only slowly.

### Zero unavailable devices — the disks are not the problem

The sample above is that case: cephadm inventoried every declared OSD host and
refused none of their declared devices. **Zero OSDs with zero unavailable devices
is not a disk-hygiene problem**, and no reclaim can do anything about it — both
`--reclaim-devices` and the automatic `dataDevices.all: true` reclaim select only
devices cephadm reports as `available=false`, so on this cluster their candidate
set is empty and running either one would wipe nothing and change nothing. The
failure says as much itself: *"NOT a device remedy … Do NOT wipe disks on this
evidence."*

**Date the evidence before reading it.** `ceph orch ps`, `ceph orch ls` and
`ceph orch device ls` are all served from the cephadm mgr module's cache, never
read live, so every host, daemon and device list the failure prints is a snapshot
of one moment. The `Orchestrator cache age:` line says which moment. When it
reports that the cache did not advance across the whole wait, those lists prove
nothing: a cephadm mgr module that restarts mid-bootstrap keeps serving the
seed-only view it held before the restart, which reads exactly like a dead
cluster with one host and no devices.

**Then work the orchestrator, not the disks.** On the seed, see which hosts
cephadm has reached and what it has been doing:

```bash
cephadm shell -- ceph orch host ls --detail
cephadm shell -- ceph log last 200 cephadm
```

Hosts that are online but carry few or no daemons mean cephadm cannot run on
them — usually a container-image pull or registry/mirror/proxy failure, which
`ceph log last 200 cephadm` names as a `cephadm pull` or deploy error. Hosts that
are online with only the inventory or the daemon list stalled mean the
orchestrator itself is stuck; restart it, wait for `ceph orch device ls` to list
every declared OSD host, then re-apply:

```bash
cephadm shell -- ceph mgr fail
```

An OSD service sitting at `running=0` in the failure's *Declared OSD services
(running counts)* breakdown is the same condition seen from the service side: the
spec was accepted and never deployed. So is a host the failure reports as never
inventoried — an uninventoried device is not a rejected device, and nothing in
the inventory says anything about its contents.

### Some declared device is unavailable — read the reject reasons

When the availability line does count devices as unavailable to ceph-volume, the
declared disks still carry a signature. Read the
**REJECT REASONS** column of the device inventory the failure prints:

| Reject reasons | What is on the disk |
| --- | --- |
| `Has a FileSystem`, `LVM detected`, `Insufficient space (<10 extents) on vgs` | The whole disk is an LVM physical volume in a fully-allocated volume group — the classic fingerprint of a **previous** `ceph-volume` install (one `ceph-<uuid>` VG per disk, one LV at 100%). |
| `Has a FileSystem` alone | A plain filesystem signature, no LVM. |
| `Has BlueStore device label` | A raw-mode bluestore OSD, not an LVM one. |
| `locked` | Something on the host holds the device open. |

!!! warning "Confirm what the LVM is before wiping anything"
    A live OSD's disk and a stale one look identical in that table. Read one
    node directly first — `lsblk`, `pvs`, `vgs -o vg_name,vg_free_count`,
    `lvs -o lv_name,vg_name,lv_tags` and `findmnt -no SOURCE /`. Volume groups
    named `ceph-<uuid>` whose LV tags carry `ceph.osd_id=` / `ceph.cluster_fsid=`
    are prior-install residue; a volume group holding the mounted root
    filesystem is the OS disk and must never be reclaimed.

This is the expected outcome of reinstalling the operating system on nodes whose
data disks already held Ceph. The managed-OS kickstart deliberately confines
itself to `rootDeviceHints.deviceName` (`ignoredisk --only-use=` plus
`clearpart --drives=`), so a reinstall preserves the data disks — and with them
the previous cluster's LVM.

**How to clear it depends on how the OSD devices are declared.**

=== "`dataDevices.all: true`"

    Bootwright reclaims the disks for you:

    ```bash
    bootwright apply --clusters ceph-01 --mode rebuild --authorize data-loss
    ```

    Before the OSD apply this zaps every disk on those hosts that is unavailable
    to ceph-volume, carries no mounted filesystem, and does not already back an
    OSD of this cluster. Mounted and system disks, live OSDs, and any device
    that cannot be probed are skipped. It is irreversible and **not** limited to
    disks that once held Ceph, so never use it on a host that also carries data
    to keep or runs a second Ceph cluster — an unmounted disk belonging to a
    co-resident cluster is indistinguishable and would be wiped.

    Two interactions to expect: `--mode rebuild` additionally re-bootstraps the
    cluster if its identity (`seedHost`/`monIP`/network) drifted, and it fails
    closed on a context whose `safety.protectedKinds` lists `StorageCluster` —
    there, run the `destroy` the refusal names first.

=== "`dataDevices.paths` / `pathSpecs`"

    Name the devices explicitly:

    ```bash
    bootwright apply --clusters ceph-01 --reclaim-devices /dev/disk/by-id/wwn-0x...
    ```

    or pass the single word `all` to select every declared OSD device of the
    selected owned cluster(s):

    ```bash
    bootwright apply --clusters ceph-01 --reclaim-devices all
    ```

    Either way only a declared device whose host marker does not already record
    it is wiped, so the healthy nodes stay untouched.

    If the reclaim itself refuses the device — *"it carries LVM or dm-crypt
    holders"* — the disk still belongs to something. The refusal says which
    something: it reports whether the node carries a Ceph daemon tree at
    `/var/lib/ceph`. With one, drain the OSD first. Without one, the holders are
    an orphan no `ceph orch osd rm` can reach, and
    `--authorize data-loss,unowned-devices` is the pair that wipes it — see
    [Wiping an orphan the marker never recorded](advanced/operations.md#managed-os-reinstall-and-owned-ceph-rebuild).

    See [Reclaiming OSD disks](advanced/operations.md#managed-os-reinstall-and-owned-ceph-rebuild).

=== "A narrowing filter (`model`/`size`/`rotational`/`vendor`/`limit`)"

    There is no automatic reclaim. Bootwright only knows the boolean `all` flag,
    not the predicate, so auto-zapping every unavailable disk could wipe disks
    the filter never targets. Confirm each disk is disposable and clean it on
    its host (`wipefs --all`, `sgdisk --zap-all`), or exclude it from the
    selection, then re-apply.

!!! note "A count short of the declaration means a device that is not there"
    If readiness reports fewer OSDs than declared and the shortfall is the same
    on every node, the declared device list names a disk the hardware does not
    have. A reclaim run says so directly — it skips the device and reports that
    the declaration does not match the hardware — and NVMe namespace numbering is
    the usual cause: whether the root disk occupies `nvme0n1` decides whether the
    data disks run `nvme0n1`–`nvme3n1` or `nvme1n1`–`nvme4n1`. Confirm with
    `lsblk -dno NAME,SIZE,TYPE` and `findmnt -no SOURCE /` on one node before
    editing `spec.ceph.topology.nodes[].devices`.

If the inventory instead reports **zero devices**, nothing was refused and none
of this applies: that is the orchestrator condition of the previous section, and
the disks are not the problem.

!!! note "The half-built cluster is already owned"
    Ownership is recorded before bootstrap, so a plain re-`apply` reconciles the
    existing cluster in place rather than re-bootstrapping it — and fails at the
    same check until the disks are cleared. `apply --mode create` refuses.

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

Which nodes flap carries little signal on its own — it mostly reflects scrape
timing. The bootstrap seed's biggest writes (the Ceph and monitoring image
pulls) land before Prometheus exists to record them, so its window shows a
level, not a slope, and it stays quiet even though it holds the most data. A
node added after scraping began carries its whole install ramp inside the
two-day window, so it flaps hardest. A mon-only arbiter writes too little to
project full.

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

## A Ceph device wipe reports no gdisk

The declared-device wipe runs `wipefs --all --force` and then `sgdisk --zap-all`
on each device. `sgdisk` comes from `gdisk`, which Bootwright installs on demand
rather than as a Ceph prerequisite, so a host that never needed a device zap does
not carry it. At teardown the repositories that served the apply are often no
longer reachable — an unregistered host, a retired mirror, or a content view that
never carried `gdisk` — and the install fails with *"No package gdisk
available."*

That does not stop the teardown. Bootwright reports the failed install, skips
`sgdisk --zap-all` for that host, and completes the wipe:

```text
Could not install gdisk on ceph-prd-01/srv4204: ...
sgdisk is not available on ceph-prd-01/srv4204, so `sgdisk --zap-all` is
skipped for its devices.
```

`wipefs --all --force` still ran, and it erases the primary and backup GPT
headers along with every filesystem, RAID and LVM signature, so the disks are
released and `ceph-volume` sees them as available on the next apply. Only the
belt-and-braces rewrite of the GPT structures is missing. The wipe itself still
fails closed: a `wipefs` error, a device that became mounted, and an unprobeable
device all abort the teardown as before.

To get the zap back, put `gdisk` on the nodes while a repository that carries it
is still reachable. On a Bootwright-installed OS the durable place is the install
profile, which draws from the installer tree rather than the node's runtime
repositories:

```yaml
# MachineInstallProfile
spec:
  customizations:
    packages:
      install:
        - podman
        - lvm2
        - chrony
        - firewalld
        - gdisk
```

On nodes Bootwright did not install, `dnf install gdisk` once per host is enough
— the wipe only ever needs it to be present. If the install fails there too,
`dnf provides */sgdisk` says which repository carries it and whether that
repository is enabled on the host.

## A destroy reported complete but a node still carries the cluster

`destroy` ends with `[OK] destroy: complete` and a partial-destroy warning, and
one node still has everything:

```text
[OK] destroy: complete
[WARN] partial destroy: storage cluster(s) ceph-prd-01 left partially destroyed:
unreachable node(s) were skipped, ... Skipped node(s): node-01.
```

```text
$ bootwright cluster exec --name ceph-prd-01 --node node-01 -- sudo pvs -o pv_name,vg_name,lv_name
  /dev/nvme0n1 ceph-96d04ef1-... osd-block-d50ffa4e-...
```

The `[OK]` is the *run* status; the `[WARN]` is the outcome. A skipped node is
never wiped, its Ceph daemons are never stopped, and it keeps serving the cluster
the run reported destroyed. Read the warning: it names the skipped nodes.

Wiping those disks by hand fails while the daemons hold them:

```text
Logical volume ceph-96d04ef1-.../osd-block-d50ffa4e-... in use.
wipefs: error: /dev/nvme0n1: probing initialization failed: Device or resource busy
```

That is an open logical volume, which on a storage node is a live OSD. `vgremove`
and `wipefs` cannot proceed until the daemons that hold it are gone.

**Re-run `destroy` for the same scope.** It releases the leftover itself: it
reads the cluster identity from the `ceph.cluster_fsid` tag of the bluestore
volume groups on the devices it is authorized to wipe, runs the fsid-scoped
`cephadm rm-cluster --force --fsid <fsid>` on that node, then takes the LVM stack
down (`vgchange --activate n` → `vgremove` → `pvremove`) before wiping. It does
this even when the seed no longer names the cluster, which is the normal state
after the other nodes were cleaned.

Clear it by hand only when the re-run refuses — for example when the node holds
no Bootwright OSD ownership marker for those devices, so nothing vouches for the
cluster identity:

```bash
systemctl list-units 'ceph-*@*.service'
sudo cephadm rm-cluster --force --fsid <fsid>
```

That is fsid-scoped: it stops and removes only that cluster's daemons, units and
`/var/lib/ceph` state on that node, and zaps no disk. Re-run `destroy` afterwards
to wipe the devices and remove the rest of the local state.

**Why the node was skipped.** `--authorize unreachable-nodes` skips a node only
when the teardown *proves* it could not be contacted — no route, an unreachable
network, a host that is down, a connection that timed out or was refused. Any
other refusal now fails the teardown closed and prints what the probes reported:
a rejected identity (see
[A Ceph node refuses passwordless sudo](#a-ceph-node-refuses-passwordless-sudo)),
an address that does not resolve, or a diagnostic it cannot read. A run predating
that behaviour skipped on any refusal at all and left the node in exactly the
state above, so read the skip line in the warning: it now names each skipped node
with the diagnostic the skip was based on.

## A destroy reported complete and the next apply refuses one node's disks

`destroy` ends `[OK] destroy: complete` with no partial-destroy warning, and the
next `apply` refuses the declared OSD devices of a single node — in practice the
seed, and only the seed:

```text
Declared OSD device /dev/nvme0n1 on node-01 already carries data
(nvme0n1 0x218  LVM2_member jxfRiI-...) and is not recorded as a
Bootwright-owned OSD device.
```

The disks were wiped; something signed them again after the wipe. `destroy`
removes the cluster one host at a time, and until Bootwright disabled it first, a
still-enabled cephadm manager module reconciled each purged host straight back:
it redeployed the daemons the purge removed and, seeing the OSD devices it had
just freed, ran ceph-volume over them again. The seed is purged first, so it is
the node the surviving managers had the longest to reprovision — and the fresh
volume groups it ends up with carry no Bootwright OSD ownership marker, because
the same teardown removed it.

A second `destroy` clears the node: by then no manager is left to reprovision it.

Current runs close both halves. Before any host removes the cluster, the seed
runs `ceph mgr module disable cephadm` and the teardown fails closed if it
cannot; after the wipe, every device is re-read with `wipefs --no-act` and a
surviving signature fails the run *before* the OSD ownership marker and the
cluster ownership record are removed, so a re-run still recognizes the devices as
Bootwright's instead of refusing them as foreign data.

If a teardown predating that behaviour left a node in this state, re-run
`destroy` for the same scope; if its ownership evidence is already gone, wipe the
named devices with:

```bash
bootwright apply --reclaim-devices /dev/nvme0n1,/dev/nvme1n1 \
  --authorize data-loss,unowned-devices
```

A cluster left with its orchestrator disabled by a failed teardown re-enables
with `cephadm shell -- ceph mgr module enable cephadm` on the seed.

## The stretch tiebreaker is on a host that is down or being decommissioned

Do not edit `spec.ceph.topology.stretch.tiebreaker.node` and re-run `apply` —
the re-authored tiebreaker is structural drift, so `apply` fails closed, and
the refusal's `--mode rebuild` suggestion is the owned-Ceph wipe-and-rebuild,
not an arbiter move. The verb that moves a live stretch tiebreaker is:

```bash
bootwright storage-cluster replace-arbiter --name <cluster> --new-arbiter-machine <machine>
```

It adds the replacement mon before removing the old one, so a failure part-way
leaves the original still holding the tiebreaker. Three refusals name the token
that proceeds:

- `--authorize unreachable-nodes` — when the old arbiter host is provably gone
  (down, no route), so it must be retired offline;
- `--authorize same-site-arbiter` — when the replacement shares a site with the
  data-site mons (the emergency fallback while the third site is gone);
- `--authorize degraded-quorum` — when declared mons are already out of quorum.

The replacement's site comes from the candidate machine's
`spec.placement.site`, so moving the arbiter to a *different* third site is just
naming a machine that stands there — there is no site flag. A candidate with no
`placement.site` is refused before anything runs: without it the promoted mon
would inherit the retiring arbiter's location and report a datacenter it is not
in.

See [Replacing the arbiter](advanced/ceph-topologies.md#replacing-the-arbiter)
for the full procedure and its preconditions.

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

## A credentials prompt pops up on the Ceph management gateway

While using the dashboard behind a
[management gateway](advanced/ceph-topologies.md#the-management-gateway-and-ha-dashboard),
the browser repeatedly raises an HTTP Basic-auth dialog naming only the gateway
origin, without saying which page asked.

The moment a `mgmt-gateway` is deployed, cephadm treats the monitoring stack as
secured: Prometheus and Alertmanager start demanding Basic authentication even
though Bootwright never enables `mgr/cephadm/secure_monitoring_stack`. The
gateway's `/prometheus` and `/alertmanager` routes proxy the browser's request
through without injecting credentials, so the daemons' `401` challenge reaches
the browser attributed to the whole origin. The dashboard itself never triggers
it — its pages authenticate with a token, and its server-side monitoring calls
carry the generated credentials — so a popup means some tab, bookmark, or link
touched `/prometheus` or `/alertmanager` on the gateway directly. Alert
"Source" links that operators rewrite to the gateway (next section) are the
usual trigger.

The credentials it wants are cephadm's generated monitoring credentials:

```bash
bootwright cluster exec --name <storage-cluster> --node <seed> -- \
  sudo cephadm shell -- ceph orch prometheus get-credentials
bootwright cluster exec --name <storage-cluster> --node <seed> -- \
  sudo cephadm shell -- ceph orch alertmanager get-credentials
```

They default to `admin`/`admin`; rotate them with the matching
`set-credentials` verbs. With the credentials in hand, the Prometheus and
Alertmanager UIs are fully usable through the gateway at `/prometheus/` and
`/alertmanager/` — that is the supported way in, not the daemons' own ports.

## Ceph alert links point at a machine hostname on port 9095

Clicking an alert's *Source* link opens
`http://<machine-fqdn>:9095/prometheus/graph?...` — the Prometheus node's own
machine name and daemon port, which operator networks typically cannot reach —
instead of going through the management gateway.

cephadm renders the Prometheus daemon with
`--web.external-url=http://<machine-fqdn>:9095/prometheus`: the scheme is
hardcoded, the host is the Prometheus node's own resolver FQDN, and the gateway
VIP is never consulted. Every alert stamps that value as its `generatorURL`,
Alertmanager passes it through, and the dashboard renders it verbatim. There is
no supported override — `PrometheusSpec` carries no URL field,
`extra_entrypoint_args` would emit the flag twice (Prometheus refuses to start
on a repeated flag), and `ceph dashboard set-prometheus-api-host` only moves
the mgr's server-side proxy, not the links.

The links still work with one substitution: keep the path and query, and swap
the scheme, host, and port for the gateway origin (the `DashboardURL` from
`bootwright cluster info`):

```
http://<machine-fqdn>:9095/prometheus/graph?g0.expr=...
        └── becomes ──┘
http://<gateway-fqdn>:8888/prometheus/graph?g0.expr=...
```

The gateway path prompts for the monitoring credentials above.

## The dashboard behind the gateway intermittently answers 502

Two nginx-side mechanisms produce sporadic `502` responses on `/api/...`
endpoints through a gateway origin that otherwise works:

- The gateway's dashboard upstream lists **every** mgr host; standbys
  deliberately answer `503` and nginx walks the list to find the active one.
  One failed exchange with the active mgr — a dashboard module restart, a slow
  or erroring `/api` handler — marks it down for 10 seconds, and with the
  standbys already failing, nginx answers `502` for everything until the window
  passes. Continuously polled endpoints surface it; a retry moments later
  works.
- A gateway daemon keeps the configuration it was deployed with until its
  dependency list changes — a corrected spec alone never rewrites a running
  daemon. Gateways deployed while a vendor build was stuck in the
  reconfigure loop can keep serving loop-era configuration (TLS answers on a
  plain-HTTP gateway, or a backend map naming the dashboard's old HTTPS port,
  which the gateway itself now occupies), and the keepalived VIP floats across
  the per-host gateways, so the same origin alternates between healthy and
  stale daemons.

The second mechanism is not limited to a build that looped. cephadm computes a
gateway daemon's dependency list from the manager daemon *names* alone — never
the dashboard's port or scheme — while the apply's management phase flips
`mgr/dashboard/ssl` off and moves the dashboard to its own HTTP port. Gateways
that predate that flip therefore keep proxying an endpoint that no longer
exists, and answer `502` for **every** request rather than intermittently.

Apply repairs this on `exposure: http` clusters: after service readiness it
GETs each gateway's own port, and one `ceph orch reconfig mgmt-gateway` runs if
any of them answers an upstream fault. An `https` gateway serves a
cephadm-signed certificate that the probe has no trust anchor for, so it is not
covered — run the commands below by hand there.

Check the stored spec, then rewrite every gateway daemon from it:

```bash
# settled store with the ssl switch present ("needs_configuration": false,
# inner spec carrying "ssl": false on an exposure: http gateway)
bootwright cluster exec --name <storage-cluster> --node <seed> -- \
  sudo cephadm shell -- ceph config-key get mgr/cephadm/spec.mgmt-gateway

# a Reconfiguring storm here means the store relapsed — re-run apply first
bootwright cluster exec --name <storage-cluster> --node <seed> -- \
  sudo cephadm shell -- ceph log last 100 info cephadm

# re-render all gateway daemons from the settled spec, then watch them return
bootwright cluster exec --name <storage-cluster> --node <seed> -- \
  sudo cephadm shell -- ceph orch reconfig mgmt-gateway
bootwright cluster exec --name <storage-cluster> --node <seed> -- \
  sudo cephadm shell -- ceph orch ps --daemon-type mgmt-gateway
```

Never save-and-reapply `ceph orch ls --export` output for the gateway on a
subscription-backed build: the export prints the class-default `ssl: true` for
a gateway that provably runs ssl-off, and re-applying it restarts the
reconfigure loop.

## Grafana and Alertmanager reject the dashboard's credentials

They are separate accounts from the dashboard's, and they fail for different
reasons.

**Grafana has no administrator unless you declare one.** The gateway's
`/grafana` location sets `proxy_set_header Authorization "";`, so no Basic
credentials ever reach it and what you see is Grafana's own login form. That
form refuses everything when the cluster never declared
`spec.ceph.monitoring.grafana.initialAdminPasswordRef`, because cephadm's
`grafana.ini` template writes `disable_initial_admin_creation = true` whenever
the Grafana spec carries no `initial_admin_password` — Grafana then creates no
admin account for any password to match. `bootwright plan` advises on this.
Declare the reference and re-apply; the apply seeds the account and recreates
the Grafana daemons, because the password only reaches `grafana.ini` when the
daemon is rebuilt. The dashboard's embedded Grafana panels are unaffected
either way: cephadm grants them anonymous viewer access.

**Alertmanager and Prometheus use real Basic auth** — armed by the mere
presence of a management gateway, not by anything you declared — and the
gateway does forward the browser's `Authorization` header to them. The
credentials are simply not the dashboard's:

```bash
bootwright cluster exec --name <storage-cluster> --node <seed> -- \
  sudo cephadm shell -- ceph orch alertmanager get-credentials
bootwright cluster exec --name <storage-cluster> --node <seed> -- \
  sudo cephadm shell -- ceph orch prometheus get-credentials
```

Both start at `admin`/`admin`; the matching `set-credentials` commands change
them.

## The dashboard's Upgrade page cannot list versions on a vendor build

`Administration > Upgrade` reports `not retrieving upgrades` /
`Failed to fetch registry information`, often alongside a `502` that clears on
its own.

The page calls cephadm's `upgrade_ls`, which resolves the image from
`mgr/cephadm/container_image_base` and then queries the registry
**anonymously** — `reg = Registry(reg_name); ls = reg.get_tags(bare_image)`
passes no credentials, and the registry-login store cephadm uses to *pull*
images is never consulted. On IBM Storage Ceph that base is `cp.icr.io`, which
is entitled, so the tag listing cannot succeed no matter how the cluster is
configured. `requests.get` there also carries no timeout, so an unreachable or
silently-dropping registry blocks the dashboard's API handler until nginx's
read timeout fires — and because the gateway's `upstream dashboard_servers`
block sets no `max_fails`, nginx's default of `max_fails=1 fail_timeout=10s`
applies and standbys answer `503`, so that one stall blacks the whole origin
out with `502` for ten seconds. That is the 502 you see, and it is a reason to
keep off that page rather than a cosmetic annoyance.

Nothing in Bootwright can supply credentials to that query. Upgrades on these
builds are driven out of band, which is where the product documents them
anyway:

```bash
bootwright cluster exec --name <storage-cluster> --node <seed> -- \
  sudo cephadm shell -- ceph orch upgrade start --image <registry>/<repo>:<tag>
```

### The page cannot be hidden, and the 502 cannot be tuned

Three mitigations get proposed and none of them works; they are recorded here so
the question stays closed.

**A dashboard feature toggle.** `ceph dashboard feature disable` accepts only
`rbd`, `mirroring`, `iscsi`, `cephfs`, `rgw`, `nfs`, and `dashboard` — the
`Features` enum has no upgrade member, so there is nothing to switch off.

**Hiding it with RBAC.** The nav item is gated on `permissions.configOpt.read`,
so a role without `config-opt` read does hide it — along with **Configuration**,
**Ceph users**, **Manager modules**, and **Multi-cluster**, which are gated on
the same scope. The scope is far too coarse to trade for one page, so this only
makes sense for a deliberately restricted operator role, never for the estate's
administrators.

**Tuning the upstream so one stall cannot black out the origin.** The
`max_fails`/`fail_timeout` behaviour comes from nginx's defaults, and cephadm's
`mgmt-gateway/nginx.conf.j2` exposes no parameter for either. cephadm rewrites
that file on every reconcile, so overriding it does not survive.

The only change that makes the page work is pointing
`mgr/cephadm/container_image_base` at a registry that serves tag listings
anonymously — a local mirror. That same value drives real image pulls, so it is
worth doing only where a full mirror already exists, and never as a way to
quiet a cosmetic error.

## The Data Foundation exporter fails to verify the manager endpoint

The external-cluster attach step dies with the exporter's stdout withheld and a
stderr naming the manager's Prometheus endpoint:

```text
unable to connect to endpoint: 10.7.7.1:9283, failed error:
HTTPSConnectionPool(host='10.7.7.1', port=9283): Max retries exceeded with url: /
(Caused by SSLError(SSLCertVerificationError(1, '[SSL: CERTIFICATE_VERIFY_FAILED]
certificate verify failed: unable to get local issuer certificate')))
```

Declaring a `mgmtGateway` is the cause. cephadm computes `security_enabled` as
`secure_monitoring_stack OR mgmt_gw_enabled`, so a gateway arms monitoring
security on its own, and the ceph-mgr prometheus module then serves `:9283`
over TLS with a certificate signed by the per-cluster cephadm root CA. Rook's
exporter re-dials the endpoint the managers publish and verifies it against the
system trust store, which has never heard of that CA.

Confirm the scheme the managers publish:

```bash
bootwright cluster exec --name <storage-cluster> --node <seed> -- \
  sudo cephadm shell -- ceph mgr services
```

An `https://…:9283/` value there means the exporter has to *verify* that
endpoint, so the attach step first retrieves the cluster's own cephadm root CA
and runs the exporter against a trust bundle that carries it — the node trust
store plus that CA, mounted into the `cephadm shell` container and named by
`REQUESTS_CA_BUNDLE`. Omitting the endpoint instead is not an option:
ocs-operator refuses to reconcile a `StorageCluster` whose external resources
carry no `monitoring-endpoint`, failing with
`Unable to retrieve "monitoring-endpoint" external resource` and parking the
cluster in `Phase: Error`.

If the step fails with the refusal naming three retrieval paths, run them on the
seed to see which one the build offers:

```bash
bootwright cluster exec --name <storage-cluster> --node <seed> -- \
  sudo cephadm shell -- ceph orch certmgr cert get cephadm_root_ca_cert
bootwright cluster exec --name <storage-cluster> --node <seed> -- \
  sudo cephadm shell -- ceph orch sd dump cert
```

Note what the emitted endpoint does and does not buy you: rook's external-mode
`MonitoringSpec` carries `externalMgrEndpoints` and a port, and no scheme, CA or
TLS field, so the ServiceMonitor OpenShift builds from it scrapes plain HTTP
against a TLS listener and that one target stays down. The `StorageCluster`
reconciles and every data path works; only the Ceph metrics panels stay empty.
Removing the management gateway disarms cephadm's monitoring TLS and restores
them.

## A KubeVirt agent ISO clone never leaves CloneInProgress

Booting a virtualized cluster's machines reports, once per machine:

```text
cloning the agent ISO from shared DataVolume <cluster>-agent-iso-source did not
reach Succeeded after 24 polls (115s) (phase CloneInProgress, progress N/A,
running False/Pending); falling back to a direct virtctl image-upload for this
machine, which builds identical media.
```

The wait is not a countdown: it re-reads `.status.phase` on every poll and stops
the moment a clone reports `Succeeded` or `Failed`. What this message says is
that the clone published no progress figure and never got a running transfer,
which is CDI declining the clone rather than a copy that needs longer. CDI holds
such a clone at `CloneInProgress` indefinitely and never marks it `Failed`, so
bootwright concludes at a start deadline
(`bootwright_kubevirt_iso_clone_start_retries`, 24 polls) instead of waiting out
the full budget, then uploads the ISO directly for that machine. The fallback
produces identical media and the boot continues, so there is nothing to clean up
by hand.

`CDI recorded:` in the same message carries the DataVolume's own events, read
before the stalled clone is deleted. Look there first: a size or `volumeMode`
rejection names itself.

## A virtualized node fails "Writing image to disk did not sufficiently progress"

The installer log for a KubeVirt-hosted cluster fails one node and then times
out on bootstrap:

```text
Host master-03.<cluster>.<domain>: updated status from installing-in-progress to
error (Host failed to install because its installation stage Writing image to
disk did not sufficiently progress in the last 30m0s.)
...
Bootstrap failed to complete: : bootstrap process timed out: context deadline
exceeded
```

Everything after that first line is a consequence, not a second problem. etcd's
`DelayedHAScalingStrategy` needs three healthy members, so a lost master leaves
`CheckSafeToScaleCluster found 2 healthy member(s) out of the 3 required`, the
bootstrap control plane never hands off, and every worker sits at `Waiting for
control plane`. `ingress`, `authentication`, `monitoring`, `machine-api` and
`olm` all report unavailable because no node ever joined — fix the disk write and
they clear on their own.

The stage that stalled is the agent writing the RHCOS image to the machine's
root disk, so the question is what that disk is made of. Bootwright requests it
through `spec.storage` and names neither `accessModes` nor `volumeMode`, which
lets CDI complete both from the class's `StorageProfile`. A claim that spells
those out instead falls back to the Kubernetes default `volumeMode: Filesystem`,
and on a Block class — Ceph RBD, including ODF external — that puts a filesystem
inside the RBD image and a `disk.img` file inside the filesystem. Every write the
agent makes then pays journal and extent-allocation overhead on top of the
network round trip, which is slow enough that a whole cluster's machines writing
at once can miss the agent's 30-minute no-progress watchdog. Check what the
machines actually got:

```bash
kubectl -n <namespace> get pvc <cluster>-<machine>-root \
  -o jsonpath='{.spec.volumeMode}|{.spec.accessModes[*]}{"\n"}'
kubectl get storageprofile <storage-class> \
  -o jsonpath='{.status.claimPropertySets}{"\n"}'
```

`volumeMode` is immutable once the claim is bound, so a machine that already has
a Filesystem root disk keeps it — bootwright only ever creates this DataVolume
and never re-sends its spec, since CDI's webhook refuses a spec update and the
claim holds the machine's installed OS. Rebuild the affected machines to pick up
the corrected shape; see
[Operations and Recovery](advanced/operations.md). A half-installed cluster
needs its virtual machines stopped first, or `destroy --stage clusters`.

If the claims are already Block, the disks are shaped correctly and the stall is
capacity, not layout: the host cluster provisions every machine of a cluster
concurrently, so a backing store that cannot absorb N simultaneous RHCOS writes
will still miss the watchdog. Confirm against the storage cluster's own metrics
before changing anything here.

The usual cause is a clone whose two ends were provisioned differently — the
shared source through CDI's `StorageProfile` and the per-machine target through
a hand-written claim spec. Bootwright now requests the target through
`spec.storage` with no `accessModes` or `volumeMode` of its own, so both ends
inherit the same profile and the host cluster's own clone strategy can apply.
Confirm what the class offers:

```bash
kubectl get storageprofile <storage-class> \
  -o jsonpath='{.status.cloneStrategy}{"\n"}'
kubectl -n <namespace> get pvc <cluster>-agent-iso-source \
  -o jsonpath='{.spec.volumeMode}|{.spec.accessModes[*]}{"\n"}'
```

A `copy` strategy means every machine's clone needs its own pod mounting that one
source claim; where the claim is not `ReadWriteMany` the machines of a cluster
cannot all do that at once, and the direct upload is the faster answer anyway.
Raise `bootwright_kubevirt_iso_clone_retries` only when the report shows progress
was moving.

## A host reached status "error" and the rendezvous node never became a Node

The bootstrap wait fails, and the message opens with the classification rather
than the command line:

```text
assisted-service moved a declared host to status "error" and stopped installing
this cluster, so bootwright did not re-run the wait
...
Declared nodes with no Node object in the cluster: master-01.
The absent node master-01 is this cluster's rendezvous host
```

Read that order literally. One host failing an installation stage — most often
`Joined took longer than expected 1h0m0s` on a control-plane node whose images
are still pulling — makes assisted-service stop installing the cluster and
divert into recovery. The recovery does not include the last step of the agent
flow: rebooting the rendezvous host out of bootstrap into its own role. The
rendezvous host is the first master by sorted name, the one whose address
bootwright renders as `rendezvousIP`, and it keeps serving the bootstrap control
plane for as long as it is left alone. Everything below that line in the message
— the degraded `authentication`, `ingress`, `monitoring` and
`openshift-apiserver` operators — is downstream of the missing node, not a
second problem.

Bootstrap can still complete from the two remaining masters, so a cluster in
this state can report success while being permanently one node short. Confirm on
the host itself:

```bash
ls /etc/kubernetes/manifests
```

A real master carries `etcd-pod.yaml` and `kube-apiserver-pod.yaml`; a host
still in bootstrap carries `etcd-member-pod.yaml` and `keepalived.yaml`.

Re-running the apply does not help — the wait resumes, observes the same state,
and fails the same way, which is why bootwright classifies this give-up as
non-resumable and does not spend a second installer window on it. The RHCOS
master image is already on the rendezvous host's install disk, so rebooting it
**from disk** pivots it into its role; check first that its BMC is no longer
presenting the agent ISO through virtual media, and whether it still holds the
API and ingress VIPs. Otherwise, repair whatever failed on the errored host and
rebuild:

```bash
bootwright apply --stage clusters --clusters <cluster> --mode rebuild \
  --authorize data-loss --yes
```
