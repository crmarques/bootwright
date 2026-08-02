# Backlog

Deferred and open engineering work. This is the in-repo catalog of activities
that are known, consciously postponed, and must not be lost between sessions or
agents. It is the companion to the lessons log in
[`KNOWLEDGE.md`](KNOWLEDGE.md): the index records what the codebase already
learned; this file records what it still owes.

## How to use this file

- **Before proposing new work** in an area, scan for an existing entry so a
  known-deferred item is not rediscovered from scratch.
- **Before deferring a review finding**, file an entry here in the same change
  that merges the review's accepted fixes, so the deferral is recorded where the
  next contributor will find it.
- **Entry lifecycle.** An entry is deleted in the same commit that lands its fix;
  git history is the archive. Any durable lesson from the fix moves to the owning
  `.agents/knowledge/` file. A rejected item is deleted and replaced by a
  "evaluated and rejected, do not re-propose" note in the relevant knowledge
  file — the house pattern already used by
  [substrate-destroy-gate-no-shared-helper.md](substrate-destroy-gate-no-shared-helper.md)
  and [openshift-kubevirt-agent-boot.md](openshift-kubevirt-agent-boot.md).
- **An entry must be readable without the review that produced it.** `Origin` is
  provenance, never the problem statement: `Problem` states the defect, the file,
  and where it bites, so a reader who never saw the review can act on it.
- **IDs** are `B-NNN`, assigned in order and never reused, even after deletion.
- **Status** is `open` or `in-progress`.

## Entries

## B-002 — Narrowing-filter OSD reclaim bypasses the emptiness guard
- Status: open
- Area: ceph / osd-safety
- Origin: OSD-install hardening (`--reclaim-devices`), narrowing-filter case
- Problem: `all: true` OSD device sets auto-reclaim dirty disks under
  `--authorize data-loss`, but a *narrowing* device filter (paths/model/size
  selectors) still bypasses the disk-emptiness guard, so a filtered OSD apply can
  proceed against non-empty disks without the same fail-closed check.
- Exit: extend the emptiness/reclaim guard to cover narrowing filters, or
  document a deliberate boundary; add a regression test alongside the existing
  OSD device-safety pins.
- Related: [ceph-osd-device-safety.md](ceph-osd-device-safety.md)

## B-003 — `--mode rebuild` vs the OSD-device-empty install gate
- Status: open
- Area: ceph / apply-modes
- Origin: Ceph apply-modes design (open OSD-device gate)
- Problem: the interaction between `apply --mode rebuild` and the
  OSD-device-empty install gate is unresolved — whether a drift-rebuild should
  be allowed to proceed against a device the install-time gate would refuse is
  still open.
- Exit: decide and document the precedence of the drift-rebuild path over the
  install-time device gate; encode it as a validator/preflight rule with a test.
- Related: [ceph-override-structural-rebuild.md](ceph-override-structural-rebuild.md),
  [ceph-osd-device-safety.md](ceph-osd-device-safety.md)

## B-004 — Standalone managed-OS machine teardown is fail-closed only
- Status: open
- Area: cli / machine-scope
- Origin: `--machines` apply/destroy selection landing
- Problem: `destroy --machines` fails closed for a standalone managed-OS machine
  (one not owned by a cluster substrate); real per-machine teardown of that class
  was deferred rather than implemented.
- Exit: implement (or consciously decline) teardown of standalone managed-OS
  machines and lift the fail-closed refusal.
- Related: [substrate-ownership-markers.md](substrate-ownership-markers.md)

## B-005 — Kickstart merges bond and VLAN into one stanza, capping bond MTU
- Status: open
- Area: managed-os / networking / render
- Origin: managed-OS post-install network review
- Problem: the minimal kickstart emits a single merged bond+VLAN network stanza,
  which cannot set the bond-layer MTU independently; jumbo-frame bonds are capped
  because the MTU can only be expressed on the merged line.
- Exit: de-merge the bond and VLAN stanzas in the kickstart render so each layer
  carries its own MTU (render-only change; add a golden fixture).
- Related: [managed-os-post-install-network.md](managed-os-post-install-network.md)

## B-006 — Redfish Ansible de-noise + `cleanup_media` assert bug
- Status: open
- Area: ansible / redfish
- Origin: Redfish Ansible de-noise review 2026-07-02 (not implemented)
- Problem: the boot_redfish role has accumulated redundant tasks the review
  proposed removing, and carries a latent `cleanup_media` assert bug identified
  but not yet fixed.
- Exit: apply the reviewed de-noise cleanups and fix the `cleanup_media` assert;
  keep the boot-flow guard tests green.
- Related: [redfish-boot-flow-quirks.md](redfish-boot-flow-quirks.md)

## B-007 — dnsmasq bind-order follow-up for controller DNS auto-wiring
- Status: open
- Area: networking / dns
- Origin: controller-DNS auto-wiring landing (latent follow-up)
- Problem: the managed controller resolver is wired via a systemd-resolved
  drop-in, but a latent dnsmasq bind-order issue remains where dnsmasq can race
  to grab `:53` ahead of the intended listener.
- Exit: pin the dnsmasq bind order (or bind address) so the controller resolver
  wins deterministically.
- Related: [external-dns-bootstrap.md](external-dns-bootstrap.md)

## B-008 — Multi-context shared-service degrading clobber
- Status: open
- Area: converge / shared-services / ownership
- Origin: multi-context shared bastion services landing (open item)
- Problem: a reference-aware destroy releases shared services correctly, but a
  *degrading* apply (one context reducing a shared service another context still
  references) can still clobber the shared component.
- Exit: gate degrading edits to shared services on the reference set, mirroring
  the reference-aware destroy release.
- Related: [ownership-records-store.md](ownership-records-store.md)

## B-009 — Remaining drift-detection gaps: NIC defaults and Ceph FIPS
- Status: open
- Area: converge / drift engine
- Origin: recorded-drift (diff --recorded) coverage thrust, remaining items
- Problem: the recorded-state drift classifier does not yet cover NIC default
  drift or Ceph FIPS drift, so those changes are invisible to `diff --recorded`.
- Exit: extend the recorded-state comparison to include NIC defaults and the Ceph
  FIPS gate; add classifier tests.
- Related: [scoped-runs-render-vs-work-set.md](scoped-runs-render-vs-work-set.md)

## B-011 — Storage NFS export and fabric-hash findings left unresolved
- Status: open
- Area: converge / storage
- Origin: scoped audit 2026-07-12
- Problem: two storage findings were deferred at merge time and their content
  now exists nowhere but this entry: the NFS export path (service ordering and
  export idempotency keyed `<serviceID>|<pseudo>`) and the host-scoped fabric
  projection folded into the converge hash. Neither was reduced to a defect
  statement, so the exact defect is no longer recoverable — treat this as
  "re-audit those two surfaces", not as a known bug.
- Exit: re-audit the NFS export and fabric-hash surfaces against current code;
  either file a concrete defect entry or close this one as clean.
- Related: [converge-hash-drift-model.md](converge-hash-drift-model.md),
  [ceph-rgw-nfs-service-ordering.md](ceph-rgw-nfs-service-ordering.md)

## B-015 — Two architecture recommendations left unlanded: cephadopt package, plan-builder split
- Status: open
- Area: architecture
- Origin: arch-recs branch 2026-07-06
- Problem: two package-boundary recommendations were deferred after the rest
  landed. (1) Extract the Ceph live-adoption logic into its own `cephadopt`
  package instead of leaving it inside the diff/adopt path. (2) Split the apply
  plan builder, which carries the whole task-graph construction in one unit.
  Both are refactors with no behavior change; neither has a filed defect.
- Exit: land either refactor or record it as declined in
  [package-boundary-contracts.md](package-boundary-contracts.md).
- Related: [package-boundary-contracts.md](package-boundary-contracts.md)

## B-016 — No reference example for the recommended boot-ISO install path
- Status: open
- Area: examples / managed-os
- Origin: managed-OS docs recommend the boot-ISO path
- Problem: `docs/advanced/managed-os.md` recommends the boot-ISO + profile
  `packageSource` path (~1 GB) over the full DVD (~10 GB), but no shipped example
  demonstrates it, so the recommended path has no runnable reference.
- Exit: ship a boot-ISO / `hostedTree` variant example (or a boot-ISO variant of
  a ceph-ibm lab) and reference it from the managed-OS docs.
- Related: [managed-os-media-staging.md](managed-os-media-staging.md)

## B-017 — No vSphere reference example
- Status: open
- Area: examples / vsphere
- Origin: examples substrate-coverage gap
- Problem: vSphere is a fully documented substrate but has no example tree; users
  have only `example init --provider vsphere` scaffolding.
- Exit: add a vSphere reference example, or point vSphere users at the scaffold
  from `docs/advanced/examples.md`.
- Related: [openshift-vsphere-agent-boot.md](openshift-vsphere-agent-boot.md)

## B-018 — Controller resolver not wired before the machines phase
- Status: open
- Area: converge / name resolution
- Origin: ADR 0017 adversarial review (2026-07-21)
- Problem: ansible_host for name-resolution-wired machines is the machine's
  `fqdn` name, but the controller's systemd-resolved drop-in that points at the
  managed dnsmasq is wired only in the container-cluster agent-install stage.
  On storage-only environments (or before any container cluster converges) the
  controller may not resolve managed-only `fqdn` names during the machines
  phase; the "Name resolution" preflight WARNs but apply can still fail
  mid-run with "Could not resolve hostname". Environments whose `fqdn` names
  resolve via infrastructure DNS (corporate zones) are unaffected.
- Exit: wire the controller resolver drop-in before the machines phase (or as
  part of the managed name-resolution component apply), then tighten the
  preflight WARN.
- Related: ADR 0017; internal/preflight/name_resolution.go

## B-019 — No teardown path for a removed ClusterAddon binding
- Status: open
- Area: addons / lifecycle
- Origin: OCP<->Ceph Data Foundation integration review 2026-07-23
- Problem: removing a `ClusterAddonBinding` entry (or the whole binding) from
  desired state is a silent no-op on `apply` — the add-on's OLM install
  (CatalogSource/Namespace/OperatorGroup/Subscription/CSV), any
  `customResources`, and step-applied manifests (e.g. the Data Foundation
  `rook-ceph-external-cluster-details` Secret, `ocs-external-storagecluster`
  StorageCluster CR) all remain live and Bootwright-unmanaged.
  `internal/addons/oc/execute.go` has no delete verb at all, and
  `Record.ObservedResources` is written but never read back to compute a diff
  against the current desired set.
- Exit: implement (or consciously decline) an uninstall/reconcile path that
  tears down previously-applied add-on resources when a binding or
  addonConfig entry is removed, mirroring how a `ContainerCluster` destroy
  already retires add-on state as a side effect of removing the whole cluster.
- Related: internal/addons/oc/execute.go, internal/addons/records/records.go,
  internal/converge/workflow/apply_plan.go

## B-020 — Step firstReachable retry cannot distinguish "unreachable" from "task failed"
- Status: open
- Area: addons / steps
- Origin: OCP<->Ceph Data Foundation integration review 2026-07-23
- Problem: a `ClusterAddonStep` target with the default `limit: firstReachable`
  retries the next candidate machine on ANY error from the playbook run, not
  only a connection failure — `internal/converge/ansible` only surfaces a bare
  process-exit error, with no parsing of Ansible's per-host unreachable/failed
  stats. A step whose task fails for a real reason (bad input, a genuine
  Ceph/API error) gets silently retried against a second (and third) machine
  before finally failing, risking partial side effects and an N-times
  timeout. The shipped Data Foundation exporter step (multi-node Ceph
  clusters) is exposed to this.
- Exit: thread Ansible's per-host stats/callback result (or at least a
  distinct "unreachable" signal) through `internal/converge/ansible.Runner` so
  the step-retry loop stops after the first REACHABLE machine's task fails,
  retrying only on genuine unreachability.
- Related: internal/converge/workflow/apply_addon_steps_run.go,
  internal/converge/ansible/runner.go

## B-021 — Environment.spec.resources can permanently deadlock once add-ons/_store is populated
- Status: open
- Area: state / validation
- Origin: OCP<->Ceph Data Foundation integration review 2026-07-23
- Problem: `Environment.spec.resources` narrows the loaded file set, but
  `validateSelectedResourceReferences` scans the full unfiltered discovered
  file tree (including `add-ons/_store/<name>` snapshots baked in by `context
  init`/`update`) and rejects any reference the filter excluded. A context
  whose `spec.resources` lists specific sub-paths (rather than the whole
  `add-ons` directory) becomes permanently unable to validate/apply once a
  store-resolved add-on is bound and snapshotted, with an undocumented,
  non-obvious workaround (list the whole parent directory instead of
  sub-paths).
- Exit: decide the right behavior — either auto-include
  `add-ons/_store/**` snapshots in the reference-inventory scan regardless of
  `spec.resources`, or document the workaround explicitly and add a
  validation hint that names it.
- Related: internal/state/desired/resources.go, internal/state/desired/load.go,
  internal/workspace/input.go

## B-022 — readiness.timeout is reused as a full budget by up to three sequential add-on gates
- Status: open
- Area: addons / oc
- Origin: OCP<->Ceph Data Foundation integration review 2026-07-23
- Problem: `spec.readiness.timeout` is documented as one "overall" timeout but
  `waitCatalogSourceReady`, `waitCSVSucceeded`, and `WaitReady` each
  independently apply the same configured value as their own full budget — a
  single apply of `fusion-data-foundation` (which ships a catalogSource) can
  legitimately take up to 3x the configured timeout (135m for a 45m setting)
  before finally failing.
- Exit: either track elapsed wall-clock time across the whole Apply+Wait
  sequence and pass the remaining budget into each subsequent gate, or split
  the field into distinct per-gate timeouts with accurate docs. Land together
  with B-023's poll-loop refactor since both touch the same three functions.
- Related: internal/addons/oc/execute.go (waitCatalogSourceReady,
  waitCSVSucceeded, WaitReady)

## B-023 — Three near-identical poll-loop implementations in internal/addons/oc/execute.go
- Status: open
- Area: addons / oc
- Origin: OCP<->Ceph Data Foundation integration review 2026-07-23
- Problem: `waitCSVSucceeded`, `waitCatalogSourceReady`, and `WaitReady` each
  reimplement the identical parse-timeout / derive-child-context / ticker /
  cancellation-vs-timeout skeleton, differing only in the ready-check callback
  and returned gate-error type. A fix to the cancellation/timeout distinction
  in one must be manually repeated in the other two. This is also the natural
  place to add periodic console progress output during a multi-minute wait
  (currently silent by design, since the workflow layer intentionally uses a
  quiet read-runner for these polls) — doing that three times separately would
  triple the duplication instead of shrinking it.
- Exit: extract a shared `pollUntilReady(ctx, timeout, pollInterval, check,
  onTimeout)` helper; add a rate-limited progress callback as part of the same
  refactor rather than bolting it onto three copies.
- Related: internal/addons/oc/execute.go, internal/converge/workflow/extensions.go

## B-024 — fusion-data-foundation has no end-to-end example or test coverage
- Status: open
- Area: addons / examples / tests
- Origin: OCP<->Ceph Data Foundation integration review 2026-07-23
- Problem: every shipped example and workflow/render-level test binds only
  `openshift-data-foundation`; `fusion-data-foundation`'s distinguishing
  content (its shipped catalogSource, the `ibm-entitlement`
  globalPullSecretMerge input) is proven to load and normalize
  (internal/state/desired/load_store_addons_test.go) but never proven to plan
  or render correctly through the real task-graph/OLM-resource pipeline.
- Exit: add an example (or extend an existing one) that binds
  fusion-data-foundation, and extend the plan/render test coverage to cover it
  alongside openshift-data-foundation.
- Related: internal/converge/workflow/apply_tasks_test.go,
  internal/addons/render/render_test.go, examples/

## B-025 — No observed-CSV-version record for Automatic-approval OLM add-ons
- Status: open
- Area: addons / supply-chain
- Origin: OCP<->Ceph Data Foundation integration review 2026-07-23
- Problem: both Data Foundation catalog entries default `installPlanApproval`
  to `Automatic` against an actively-polled catalogSource, and the addon
  Record tracks only a channel-based DesiredHash, never the resolved CSV
  name/version actually installed — an unattended operator upgrade within the
  subscribed channel is invisible to `status`/`diff`.
- Exit: record the resolved CSV name/version into the addon Record on
  successful apply and surface it via `status`/`diff`; separately decide
  whether either catalog entry should default to `installPlanApproval:
  Manual`.
- Related: internal/addons/records/records.go, internal/addons/oc/execute.go

## B-026 — Fetched Data Foundation exporter script has no audit trail
- Status: open
- Area: addons / security
- Origin: OCP<->Ceph Data Foundation integration review 2026-07-23
- Problem: the exporter step (`run: always`, root-privileged, executes against
  the live Ceph cluster on every apply) fetches its script at runtime from the
  operator-published `rook-ceph-external-cluster-script-config` ConfigMap;
  neither the step digest nor the StepRecord captures any checksum/identity of
  the fetched script, so there is no record of exactly which code ran on a
  given apply.
- Exit: persist the fetched script's SHA-256 (or the ConfigMap's
  resourceVersion) into the StepRecord on every run, for audit purposes;
  optionally support an add-on-declared expected checksum later.
- Related: add-ons/openshift-data-foundation/4.21/playbooks/export-external-details.yaml,
  internal/addons/records/records.go

## B-027 — No shipped example demonstrates register-then-bind for the native add-on catalog
- Status: open
- Area: addons / examples
- Origin: OCP<->Ceph Data Foundation integration review 2026-07-23
- Problem: `bootwright add-ons add` plus binding by `addonRef` against the
  machine-local store is the documented recommended onboarding path, but every
  Data Foundation-bound example instead hand-copies the catalog add-on
  directory in as an authored `ClusterAddon`, so the resolver's store fallback
  (internal/state/desired/load_store_addons.go) is unit-tested but never
  exercised at the example/e2e level.
- Exit: add (or convert) one example to use `add-ons add` plus a bare
  `addonRef` binding instead of a copied `ClusterAddon` directory.
- Related: internal/state/desired/load_store_addons.go, docs/concepts/add-ons.md

## B-028 — fusion-data-foundation catalogSource image pin unverified against the live IBM registry
- Status: open
- Area: addons / supply-chain
- Origin: OCP<->Ceph Data Foundation integration review 2026-07-23
- Problem: `add-ons/fusion-data-foundation/4.21/add-on.yaml` pins its OLM
  catalogSource by mutable tag (`icr.io/cpopen/isf-data-foundation-catalog:v4.21`,
  no digest); the 4.18→4.21 bump (commit 9cc6b017) was a mechanical
  rename/substitution with no recorded verification that v4.21/stable-4.21 is
  IBM's actual current release. This needs live-registry access to confirm,
  which was not available during this review.
- Exit: verify the image/channel against the live icr.io registry (or IBM's
  release notes) and, ideally, pin by digest; record the verification date in
  `.agents/knowledge/`.
- Related: add-ons/fusion-data-foundation/4.21/add-on.yaml, add-ons/catalog.yaml

## B-033 — Incomplete-bootstrap Ceph recovery zaps OSDs without the rebuild-authorized list
- Status: open
- Area: ceph / apply-modes
- Origin: apply/destroy safety audit 2026-07-23
- Severity: low
- Problem: `bootwright_ceph_rebuild_cleanup_required`
  (storage_cluster_cephadm/tasks/phases/bootstrap_steps/apply_mode.yml:127-142)
  ORs in `bootwright_ceph_incomplete_bootstrap` without requiring membership
  in `bootwright_ceph_rebuild_authorized_clusters`, and rebuild.yml:30-41 then
  runs `cephadm rm-cluster --force --zap-osds` — the one destructive Ceph path
  not gated by the controller's positive fail-safe token. The window is
  narrow (owned record + conf fsid + NO on-host marker + unreachable cluster,
  i.e. a bootstrap that died before marker stamping, normally pre-OSD), but
  the token model says an absent authorization must under-authorize.
- Exit: also require the rebuild-authorized (or a dedicated
  incomplete-bootstrap) extra-var for this branch, or record in
  ceph-override-structural-rebuild.md why the pre-OSD window is provably
  data-free.
- Related: [ceph-override-structural-rebuild.md](ceph-override-structural-rebuild.md)

## B-036 — all:true filter-OSD reclaim swallows zap failures
- Status: open
- Area: ceph / osd-safety
- Origin: apply/destroy safety audit 2026-07-23
- Severity: medium
- Problem: the authorized filter-OSD auto-reclaim zaps dirty disks with
  `failed_when: false`
  (storage_cluster_cephadm/tasks/phases/bootstrap_steps/osd_reclaim.yml:171-190),
  so a failed `ceph orch device zap` is silently ignored: the disk stays
  dirty, cephadm keeps refusing it, and the subsequent OSD apply waits on
  OSDs that can never appear (the B-002 emptiness-gap hang) with no message
  linking the hang to the failed zap.
- Exit: fail closed when a zap returns non-zero — or at minimum surface a
  per-device failure summary and exclude the device from the expected-OSD
  count so the readiness wait can diagnose instead of hanging.
- Related: [ceph-osd-device-safety.md](ceph-osd-device-safety.md)

## B-037 — No managed-OS machine in the advanced safety-matrix baseline
- Status: open
- Area: examples / tests / safety-matrix
- Origin: apply/destroy safety contract review 2026-07-26
- Severity: low
- Problem: `examples/baremetal-redfish-multidc-virtualized-odf-ceph` is the
  advanced baseline the safety matrix
  (internal/cli/apply_destroy_safety_matrix_test.go) runs on. Its KubeVirt-hosted
  child clusters do give the matrix real machine-substrate rows (release
  disclosure, machine-granular release), but every Ceph node declares
  `os.provided: true` and every KubeVirt *host* is bare metal, so two paths stay
  unreachable end-to-end: the bare-metal managed-OS reinstall data-loss
  acknowledgment, and a machine-substrate rebuild of a KubeVirt host cluster.
  Both are pinned only at unit level. The matrix now takes a per-case `baseline`
  and already runs `examples/ceph-ibm-libvirt-lab` for the provider-backed
  machine-layer data-loss rows (ADR 0031), so the second-baseline mechanism
  exists; these two scenarios still have no matrix row.
- Exit: promote the bare-metal managed-OS reinstall data-loss acknowledgment and a
  KubeVirt-host machine-substrate rebuild into the matrix, adding whichever
  baseline (a managed-OS bare-metal machine, a libvirt-hosted KubeVirt host)
  each needs.
- Related: [apply-destroy-authorization-guards.md](apply-destroy-authorization-guards.md)

## B-038 — The node sudoers drop-in sorts before hardening drop-ins that override it
- Status: open
- Area: ceph / node-access / sudoers
- Origin: node-access requiretty fix 2026-07-28
- Severity: medium
- Problem: sudo applies `Defaults` in parse order and the last one wins whether
  it is generic or per-user — sudoers(5) defers only *command-specific*
  `Defaults` — so a plain `Defaults requiretty` in any file sorting after
  `/etc/sudoers.d/60-bootwright-<user>` beats Bootwright's per-user
  `!requiretty` exemption. Any site hardening drop-in sorting after `60-`
  defeats the grant. A later-sorting prefix would win by construction, but the
  `60-` constant is pinned across `api/v1alpha1/types.go:87`
  (`NodeAccessSudoersPrefix`), ADR 0019:102, ADR 0024:148, ADR 0027:56,
  `specs/security.md:159`, `specs/state-model.md:323,578,1013`, and
  `machine_os_install_anaconda/templates/ks.cfg.j2:208` — plus four operator-facing
  docs sites naming the path literally (`docs/troubleshooting.md`,
  `docs/concepts/storage.md` twice, `docs/concepts/machines.md`) — and renaming would
  strand a privileged `60-` file on every already-provisioned node with no
  reconciler that removes it. Deferred rather than dropped because the failure is
  loud, not silent: `storage_node_access/tasks/verify.yml:11-36` refuses before
  any root revocation and names this as the first of three causes.
- Exit: either rename the prefix to a later-sorting one together with a
  migration that removes the old file on reconcile, or record in
  ceph-node-access-privileged-channel.md that the fail-closed refusal is the
  accepted answer and the prefix stays.
- Related: [ceph-node-access-privileged-channel.md](ceph-node-access-privileged-channel.md)

## B-039 — `machine exec` / `cluster exec` allocate no terminal, so a remote `sudo` under requiretty fails
- Status: open
- Area: cli / ssh
- Origin: node-access requiretty fix 2026-07-28
- Severity: low
- Problem: `buildSSHInvocation` (`internal/cli/ssh_client.go:170-220`) appends the
  operator's command as arguments and never passes `-t`, so ssh allocates no
  terminal. On a node whose sudoers sets `requiretty`,
  `bootwright machine exec --name <m> -- sudo …` fails with
  `sudo: sorry, you must have a tty to run sudo` even though apply now handles the
  same node. The fix is not simply to add `-t`: `machine exec` is documented as
  returning the command's output (`machine_ssh.go:52-58`), and a terminal injects
  CR into that output via `ONLCR`, breaking redirection and scripted use. The
  conventional guard is to allocate only when stdin *and* stdout are both
  terminals, which makes the behavior depend on how the command is invoked.
  Deferred rather than dropped because `machine rsh` already allocates a terminal
  and is the working path today.
- Exit: either allocate a terminal when stdin and stdout are both character
  devices and pin it with a test, or add an explicit flag, or record that `rsh`
  is the supported answer and document it in troubleshooting.
- Related: [ceph-node-access-privileged-channel.md](ceph-node-access-privileged-channel.md)


## B-041 — The bootstrap-wait resume path is unvalidated without a hardware soak
- Status: open
- Area: container-clusters / install
- Origin: ADR 0022 (cluster wait bootstrap boundary), stated as an open risk
- Severity: medium
- Problem: no test in this repository proves that `openshift-install agent
  wait-for bootstrap-complete` returns promptly against a cluster whose bootstrap
  already completed under an earlier release — no marker file, assisted-service
  already gone. Only a real install and a real resumed install exercise it, so an
  Accepted ADR is currently the repo's only record of an unproven behavior.
- Exit: run one real install and one real resumed install on hardware, record the
  observed `wait-for bootstrap-complete` behavior in
  [cluster-install-record-gates.md](cluster-install-record-gates.md), and delete
  this entry.
- Related: ADR 0022, [cluster-install-record-gates.md](cluster-install-record-gates.md)

## B-042 — Two StorageClusters sharing a node with different clusterSSH identities have no validation rule
- Status: open
- Area: ceph / node-access / validation
- Origin: definitions review 2026-07-28 (recorded inline in
  destroy-scoping-and-sweeps.md, never filed)
- Severity: medium
- Problem: `internal/state/desired/validate_storage_access.go` validates
  `clusterSSH.user`/`keyRef` per cluster only; nothing rejects two managed
  `StorageCluster` objects that list the same `Machine` in their topology under
  different orchestration identities. The inventory renders one `ansible_user`
  per node per run, so whichever cluster runs last wins and the node-access
  reconciliation of the other silently orchestrates as an account it did not
  provision.
- Exit: add a cross-cluster validation rule (one orchestration identity per
  shared node) with a named refusal and a test, or state the shared-node
  restriction normatively in `specs/state-model.md`.
- Related: [destroy-scoping-and-sweeps.md](destroy-scoping-and-sweeps.md),
  [ceph-nonroot-node-access.md](ceph-nonroot-node-access.md)

## B-043 — Seven `Machine.spec.capabilities[]` values have no enforcement site
- Status: open
- Area: api / validation
- Origin: definitions review 2026-07-28
- Severity: low
- Problem: of the eleven accepted values, only `openshift-node`, `ceph-node`,
  `libvirt` and `container-runtime` are read anywhere —
  `internal/state/desired/validate_machine.go` cross-checks the first three
  against an existing reference and `internal/roles/registry.go` asserts the last
  two as host properties. `ceph-admin`, `artifact-server`, `load-balancer`,
  `proxy`, `name-resolution`, `ntp` and `registry` appear only in the accept-list
  at `validate_machine.go:13-24`; they render into inventory
  (`internal/render/inventory/vars.go`) but no playbook consumes them, so
  authoring or omitting them changes nothing.
- Exit: give each remaining value an enforcement site, or retire it — removal
  from `v1alpha1` is an API break and needs its own ADR.
- Related: [api-normalize-bookkeeping.md](api-normalize-bookkeeping.md)

## B-044 — No removal path for `spec.ceph.config` entries and `mgrModules[]`
- Status: open
- Area: ceph / apply-modes
- Origin: definitions review 2026-07-28 (hedge removed from specs/state-model.md)
- Severity: low
- Problem: storage convergence is additive-only across `spec.ceph.config` and
  `mgrModules[]` (`specs/state-model.md`): deleting an entry from desired state
  leaves it set on the cluster and no apply removes it. That is an instance of
  the product-wide "deleting YAML never deletes live state" invariant, but unlike
  a machine or a cluster there is no `destroy` scope that retires an individual
  config key or mgr module either, so the only removal is out of band.
- Exit: decide whether a scoped removal belongs to `destroy` at all (it crosses
  the destroy-authorization boundary ADR 0007 owns) or stays out of band, and
  record the answer where the additive-only rule is stated.
- Related: [ceph-override-structural-rebuild.md](ceph-override-structural-rebuild.md)

## B-045 — `access.rootLogin: revoke` is Ceph-only because no other executor exists
- Status: open
- Area: machines / node-access
- Origin: definitions review 2026-07-28
- Severity: low
- Problem: `internal/state/desired/validate_machine.go` accepts `revoke` only on
  a machine some managed Ceph `StorageCluster` lists under a non-root
  `clusterSSH.user`. The reason is not that other machines lack an account — it
  is that the revoke is executed solely by the `storage_node_access` role against
  `bootwright_storage_hosts`, so on any other machine the field would be inert.
  An operator who wants root SSH revoked on a bastion or a load-balancer host has
  no path.
- Exit: add a generic day-2 node-hardening role that can execute the revoke on
  any machine with a non-root `access.ssh` arm, then widen the validator; or
  record the Ceph-only scope as deliberate in the owning spec.
- Related: [ceph-nonroot-node-access.md](ceph-nonroot-node-access.md)

## B-046 — CA-trust references are spelled four different ways across the API
- Status: open
- Area: api / grammar
- Origin: definitions review 2026-07-28 (corporate-certificates trust-site sweep)
- Severity: low
- Problem: the same concept — "a Secret holding CA certificates to trust" — is
  authored under four names: `additionalTrustBundleRefs`
  (`api/v1alpha1/containercluster.go:43`), `caBundleRefs`
  (`environment.go:220`), `trustBundleRef` (`environment.go:228,243`;
  `entitlement.go:55,62`) and `trustRefs` (`machine.go:198`). ADR 0014's grammar
  fixes the `Ref`/`Refs` suffix but not the noun, so an author cannot predict the
  field name for a new trust site.
- Exit: converge on one noun for the whole API (a `v1alpha1` break — needs an
  ADR amending 0014), or record the four spellings as deliberate with the reason
  each differs.
- Related: ADR 0014

## B-047 — Neither SSH credential has a rotation path
- Status: open
- Area: security / secrets
- Origin: definitions review 2026-07-28 (security.md claims without a contract)
- Severity: medium
- Problem: `specs/security.md` sells the non-root posture on "a credential that
  can be rotated or revoked without touching the root account", but no rotation
  path exists for either key. `Environment.spec.machineAccess.keyRef` is
  authorized exactly once, by the kickstart (`sshkey --username={{ ssh_user }}`,
  `ks.cfg.j2:118`); no machines-phase role touches `authorized_keys`, so rotating
  the Secret bytes strands every installed machine and the ownership probe then
  fails closed. Renaming that Secret additionally reads as structural drift,
  because the install-marker hash uses the basename of `sshPublicKeyPath`
  (`internal/render/inventory/vars_machine_os_marker.go:29-31`). For
  `cephadm.clusterSSH.keyRef` the node side does re-authorize a new public key
  (`storage_node_access/tasks/authorize.yml:33-44`), but nothing reconciles the
  mon config-key private half: `ssh_user.yml` runs `ceph cephadm set-user` only,
  and there is no `set-priv-key` anywhere in the collection.
- Exit: specify and implement a rotation contract for both keys (re-authorize on
  the node, re-key the mon config-key store, re-stamp the marker), or state
  plainly in `specs/security.md` that both are install-time credentials today.
- Related: [ceph-nonroot-node-access.md](ceph-nonroot-node-access.md),
  [ssh-trust-store-invariants.md](ssh-trust-store-invariants.md)

## B-048 — A dead declared seed makes a storage cluster undestroyable
- Status: open
- Area: ceph / destroy-gates
- Origin: ceph-prd-01 full-context destroy 2026-07-29, seed host powered off
- Severity: high
- Problem: storage teardown pins its ownership proof to the *declared* bootstrap
  node, not to any surviving mon. `StorageSeedHostName`
  (`internal/render/inventory/storage_ansible.go:16-25`) resolves the seed from
  `spec.ceph.cephadm.bootstrap.node` with no fallback, and
  `task_storage_cluster_destroy.yml:133-142` then hard-asserts that one host is
  reachable before any node wipes OSD devices. `--authorize unreachable-nodes`
  deliberately cannot skip it, and dropping the token only moves the refusal to
  the blanket per-node assert at line 59. So when the declared seed is powered
  off or permanently gone, the StorageCluster cannot be torn down in-product at
  all: `--mode` is apply-only, `--recover-ceph-ownership` explicitly does not
  bypass device safety, and no skip-node flag exists. The only exits are
  out-of-band power-on, retargeting `bootstrap.node` (which risks the
  `seedHost` ownership-conflict refusal in
  `internal/converge/ceph_ownership_recovery.go:93-97`), or
  `context delete --purge --abandon-resources`, which abandons every resource
  and loses install-captured credentials.
- Exit: let teardown prove cluster ownership from any reachable mon that carries
  a matching controller ownership record, falling back to the declared seed;
  failing that, add an explicitly authorized record-only storage teardown so a
  decommissioned cluster can leave state without a live seed. Pin either with a
  test.
- Related: [ceph-ownership-apply-destroy-gates.md](ceph-ownership-apply-destroy-gates.md),
  [ceph-cephadm-bootstrap-contract.md](ceph-cephadm-bootstrap-contract.md)

## B-050 — Daemons left on a pre-pin image are invisible and unreported
- Status: open
- Area: storage / ceph
- Origin: IBM Ceph `:latest` investigation 2026-07-31, alongside the
  `global container_image` fix
- Severity: medium
- Problem: apply now asserts `container_image`, but `ceph config set` binds only
  the daemons cephadm creates or redeploys afterwards. A cluster that ran on a
  floating tag before the pin keeps every existing daemon on the old build, and
  nothing tells the operator: no apply output names the divergence, and
  `bootwright diff` cannot see it either — `internal/state/cephdiff/cephdiff.go`
  builds eight facets and none carries a daemon image, while the `config` facet
  is built with `ignoreRealOnly: true` so a real-only `global/container_image` is
  dropped before comparison. The remedy (`ceph orch upgrade start --image`) is
  correctly out of band per `docs/concepts/storage.md`, but the operator has no
  signal that it is needed.
- Exit: read `ceph orch ps --format json` after the pin is asserted and warn,
  naming the daemons whose image differs from the pin and the
  `ceph orch upgrade start --image <ref>` remedy; separately decide whether a
  daemon-image facet belongs in `cephdiff` or whether that is the same open
  design item as adopting an out-of-band upgrade into the record.
- Related: [ceph-distribution-packaging.md](ceph-distribution-packaging.md),
  B-044

## B-051 — The sidecar-image advisory misses ingress-only and partially-pinned clusters
- Status: open
- Area: storage / ceph
- Origin: IBM Ceph `:latest` investigation 2026-07-31
- Severity: low
- Problem: `storageSidecarImageAdvisories`
  (`internal/state/advice/storage.go`) returns early when monitoring is
  disabled, yet its own impact text names haproxy and keepalived — which come
  from ingress, and ingress renders independently of monitoring, so a
  monitoring-disabled cluster with an RGW or management-gateway ingress is
  silently unadvised. It also short-circuits on the first pin found, so pinning
  one of the six silences the other five, and it names none of the
  mgmt-gateway sidecars (`container_image_nginx`,
  `container_image_oauth2_proxy`).
- Exit: split the gate so the monitoring half keys on monitoring and the
  ingress half on any bound gateway/export ingress or a declared management
  gateway; compare against the missing subset rather than a non-empty check;
  extend the remediation for mgmt-gateway estates. The two guard tests in
  `internal/state/advice/storage_test.go` that lock the current behaviour must
  change with it. These are cephadm option names, not a vendor catalog.
- Related: [ceph-distribution-packaging.md](ceph-distribution-packaging.md)

## B-052 — No preflight for pre-existing package-mode Ceph daemon RPMs
- Status: open
- Area: storage / preflight
- Origin: cephadm-ansible parity audit 2026-07-31
- Severity: low
- Problem: IBM's `cephadm-preflight.yml` removes `ceph-mds`, `ceph-mgr`,
  `ceph-mon`, `ceph-osd`, `ceph-radosgw` and `rbd-mirror` before bootstrap,
  because a package-mode Ceph install binds the ports and udev/systemd units the
  containerized daemons want. Bootwright has no equivalent on the apply path —
  `roles/check_storage_preflight/tasks/main.yml` gathers no package facts — and
  `destroy_steps/wipe_and_cleanup.yml` removes only Bootwright's own managed
  package list, on destroy. It bites on an `os.provided` node that carried a
  previous package-mode Ceph: the seed is already covered by the
  `/etc/ceph/ceph.conf` gate and the disks by the device-signature gate, so the
  residue is a non-seed host with `ceph-mon`/`ceph-radosgw` still installed.
- Exit: gather `package_facts` in `check_storage_preflight` and fail closed when
  any of those six names is installed, naming what was found and the
  `dnf remove` that clears it. A check, never a mutation — Bootwright must not
  uninstall what it did not install. Six static RPM names are not a catalog.
- Related: [ceph-distribution-packaging.md](ceph-distribution-packaging.md)

## B-053 — `sos` is absent from Bootwright-installed storage nodes
- Status: open
- Area: storage / supportability
- Origin: cephadm-ansible parity audit 2026-07-31
- Severity: low
- Problem: `sos` is in cephadm-ansible's `ceph_defaults_infra_pkgs`, and IBM's
  own `playbooks/checks.yml` computes the missing-package set against that list,
  so it reports "Required Packages Installed: FAIL" naming `sos` on a
  Bootwright-built cluster. Bootwright's kickstart is `@^minimal-environment`
  and `sos` is not in the CentOS Stream 9 BaseOS `core` group, so the node
  genuinely lacks it where a vendor-prepped node has it. Not a functional
  defect — `sos` installs from BaseOS on demand and the repos are configured by
  then — but it is a visible divergence during a support case.
- Exit: decide whether Bootwright owns support tooling on storage nodes. If yes,
  add `sos` to `PrerequisitePackages`
  (`internal/storage/cephprovider/provider.go`) and to both lists in
  `roles/storage_cluster_cephadm/vars/os/RedHat.yml` so ownership records apply
  and destroy does not strip a preexisting copy off a provided node.
- Related: [ceph-distribution-packaging.md](ceph-distribution-packaging.md)

## B-055 — `initialPasswordPath` escapes the install marker's basename normalization
- Status: open
- Area: machines / converge
- Origin: disk-encryption implementation 2026-07-31
- Severity: medium
- Problem: `stableMarkerInput` in
  `internal/render/inventory/vars_machine_os_marker.go` rewrites every
  materialized secret path to its basename so the managed-OS install marker's
  desired hash is portable across per-run secret directories. It covers
  `ssh.privateKeyPath`, `ssh.knownHostsPath`, `ssh.trustDir`,
  `kickstart.sshPublicKeyPath`, the RHSM and Satellite paths, the proxy
  credentials path, and — since this change —
  `kickstart.security.diskEncryption.passphrasePath`. It does **not** cover
  `kickstart.initialPasswordPath`, which `vars_machine_os_install.go` sets to an
  absolute `<secretsDir>/<name>`. A profile using
  `customizations.ssh.initialPassword` therefore hashes the run's secrets
  directory into the marker, so a machine can read as drifted — reinstall-only
  drift — purely because the path moved.
- Exit: add `markerBasename(kickstart, "initialPasswordPath")` beside the
  existing calls and extend
  `TestMarkerHashStableAcrossProxyCredentialsDir` to cover it. Note that the fix
  changes the recorded hash for every profile that sets `initialPassword`, so it
  needs the same one-time reinstall-drift warning any hash-shape change does.
- Related: [converge-hash-drift-model.md](converge-hash-drift-model.md)

## B-056 — Anaconda leaves RHSM material in `/root/anaconda-ks.cfg` on the installed node
- Status: open
- Area: machines / secrets
- Origin: disk-encryption implementation 2026-07-31
- Severity: medium
- Problem: Anaconda copies the kickstart it consumed to
  `/root/anaconda-ks.cfg` and `/root/original-ks.cfg` on the installed system.
  When `packageSource.fromSubscription` is set, that copy carries the RHSM
  organization and activation key in cleartext; when
  `customizations.ssh.initialPassword` is set, it carries the console password.
  Both survive indefinitely at `0600` root-owned. The disk-encryption `%post`
  shreds both files, but only when encryption is enabled — which is exactly the
  case where the disk was already protecting them.
- Exit: shred the kickstart copies unconditionally in the main `%post`, or
  decide deliberately that a root-only file on an owned node is acceptable and
  say so in `specs/security.md`. If they are shredded, note in
  `docs/advanced/managed-os.md` that the installed node no longer keeps a copy
  of the kickstart for diagnosis.
- Related: [disk-encryption-tpm.md](disk-encryption-tpm.md)

## B-057 — Nothing preflights a TPM before an OpenShift node is written to
- Status: open
- Area: openshift / disk-encryption
- Origin: ADR 0037
- Severity: medium
- Problem: `ContainerCluster.spec.security.diskEncryption` renders a
  `MachineConfig` extra manifest. assisted-service's
  `disk-encryption-requirements-satisfied` host validation short-circuits unless
  the cluster record itself declares disk encryption, and it never infers that
  from an extra manifest. A node whose firmware exposes no TPM 2.0 therefore
  passes every check, has RHCOS written to its disk, and only then fails in the
  initramfs — `clevis luks bind` errors, `ignition-disks.service` fails, systemd
  drops to `emergency.target`, and the node never registers. The installer
  reports a host that never joined; the cause is visible only on the serial or
  BMC console. The Anaconda path has a `%pre` gate that refuses before any disk
  is touched; this path has nothing equivalent.
- Exit: probe the TPM out of band before the install writes — the BMC already
  answers Redfish for these machines, so a `Systems/<id>` TrustedModules /
  inventory read at preflight could refuse the run — or revisit
  `AgentClusterInstall.spec.diskEncryption` if the ZTP `cluster-manifests` flow
  ever becomes compatible with the `install-config.yaml` renderer.
- Related: [disk-encryption-tpm.md](disk-encryption-tpm.md),
  [0037](../../specs/adr/0037-a-tpm-holds-the-key-a-passphrase-holds-the-machine.md)

## B-059 — Machine deregistration and node-access teardown still collapse "unreachable"
- Status: open
- Area: ceph / diagnostics
- Origin: ceph-prd-01 full-context destroy 2026-08-01, a running node skipped as unreachable
- Severity: low
- Problem: ADR 0039 classified the teardown connection refusal in
  `task_storage_cluster_destroy.yml` — a node that answers SSH and then rejects
  every identity is no longer skippable — but the two other consumers of
  `bootwright_node_access_connection_available` were left as they were.
  `task_machine_registration_deregister.yml` ("End nodes Bootwright cannot reach
  or escalate on") and `task_storage_node_access_destroy.yml` still read an
  identity refusal as an absent node and end the host silently. Neither wipes a
  device, so the blast radius is bounded: a node whose RHSM registration is never
  released (it collides with the next install of the same hardware) and an
  install account whose Bootwright-authored access is never revoked. See
  [destroy-scoping-and-sweeps.md](destroy-scoping-and-sweeps.md) for how both
  plays end a host today.
- Exit: hoist the classification the destroy play now performs into the
  `storage_node_access` selector so all three consumers read one
  answered/no-answer fact, then decide per consumer whether an answering node
  fails closed or is reported as skipped-with-cause. Pin with a test per play.
- Related: [ceph-node-access-privileged-channel.md](ceph-node-access-privileged-channel.md),
  [0039](../../specs/adr/0039-the-node-a-teardown-left-serving-the-cluster.md),
  B-020

## B-069 — UX-review decisions still awaiting a product call
- Status: open
- Area: api / decisions
- Origin: ux-review 2026-08-01; the review recommended a position on every
  other item in this entry and those landed, leaving the two below
- Severity: low
- Problem: two decisions were surfaced and deliberately not taken, because
  neither has a recommendation the review was willing to make on the user's
  behalf. (1) **The install-config coverage boundary.** `ContainerCluster`
  reaches exactly the keys in `api/v1alpha1/owned_installer_fields.go`, with
  no passthrough and no published not-supported list — asymmetric with the
  Ceph path, where `spec.ceph.services[]` accepts service specs Bootwright
  does not model. Nothing decides whether that asymmetry is the design. An ADR
  must choose between closed-by-design (every authorable key is a modeled
  field; an unmodeled key needs a field and an ADR; day-2 intent goes to
  `ClusterAddon`, which is the only authorable manifest channel today) and a
  narrow `installConfigOverrides` overlay that rejects owned keys. Either
  choice has to engage ADR 0008's rationale for mirroring cephadm
  declaratively, and the honest cost of the closed option is that install-time
  keys such as `capabilities`, `featureSet`, and `cpuPartitioningMode` are
  inexpressible with no workaround at all. (2) **Storage authoring
  shorthands** — a derived `StoragePlacementPolicy.spec.ceph.ruleName` default
  and a CephFS `fs volume`-style pool derivation. Both need re-scoping before
  any ADR: the review's own adversarial verification corrected their
  compositions (the kind has no `type`; failure domain is effective rather
  than authored; policy-less pools are legal, so the proposed "sole policy"
  selection rule is inconsistent), and every shipped policy already sets
  `ruleName` to `metadata.name`, so the urgency is weak.
- Exit: user picks per item; an accepted item becomes its own ADR plus an
  implementation entry, a rejected one becomes a do-not-re-propose note in the
  owning knowledge file.
- Related: [0008](../../specs/adr/0008-ceph-declarative-cephadm-compat.md),
  [0018](../../specs/adr/0018-environment-domain-model.md),
  [0025](../../specs/adr/0025-composed-names-are-labels-plus-explicit-overrides.md)

## B-070 — The published `ceph-mon` lab profile is under the hard preflight floor
- Status: open
- Area: examples / docs / ceph-preflight
- Origin: Ceph arbiter sizing pass alongside ADR 0045
- Severity: medium
- Problem: `examples/ceph-ibm-libvirt-lab/infra/providers/libvirt.yaml:34` ships
  a `ceph-mon` machine profile with `diskGiB: 16`, and
  `docs/getting-started/ceph.md:170` publishes that number as part of the first
  managed-OS + Ceph walkthrough. Meanwhile
  `ansible/collections/ansible_collections/bootwright/core/roles/check_storage_preflight/tasks/main.yml:82-89`
  asserts a **hard** floor of `20971520` KiB (20 GiB) **free** on the filesystem
  holding `/var/lib`, and `docs/concepts/storage.md` publishes the same 20 GiB
  absolute floor plus a mon-node budget of 20 base + 15 mon = 35 GiB. A 16 GiB
  root cannot clear a 20 GiB *free* assert; neither can a 20 GiB one once an OS
  is installed on it. So either the shipped lab fails its own preflight, or the
  preflight is not reached on that path — and the docs advertise a sizing the
  product refuses. Correcting it after the fact is expensive by design: the
  libvirt and vSphere substrate gates both refuse an in-place root-disk resize
  and a disk-count change, naming `destroy --stage infra` as the only exit.
- Exit: maintainer's call on the right lab-scale value. Either raise the
  example profile (and the `docs/getting-started/ceph.md` prose that mirrors it)
  to a size that clears the floor, or introduce an explicit lab-scale relaxation
  of the absolute floor with its own opt-in and refusal message. Do **not**
  silently change the example's numbers without deciding which of the two it is;
  a lab that cannot complete preflight and a floor that does not apply to labs
  are different products.
- Related: [ceph-cephadm-bootstrap-contract.md](ceph-cephadm-bootstrap-contract.md),
  [examples-wip-fixtures.md](examples-wip-fixtures.md)

## B-071 — Nothing validates a Ceph node's `diskGiB` against the root-FS budget
- Status: open
- Area: state/desired · storage sizing
- Origin: vSphere arbiter provisioning review (2026-08-02)
- Problem: `internal/render/ceph/storage_cephadm_rootfs.go` already computes the
  per-node root-filesystem budget (`NodeRootFilesystemGiB`, floor
  `RootFilesystemFloorGiB = 20`), but its only non-test caller is the renderer
  (`internal/render/inventory/storage_ansible.go`). No desired-state rule
  compares that number against the `machineProfiles[].diskGiB` the operator
  authored, so an undersized Ceph node is discovered by
  `check_storage_preflight` *after* the VM is built and RHEL is installed — and
  the substrate gates then refuse an in-place resize, making the only remedy a
  `destroy --stage infra` plus a reinstall.
- Blocker: `internal/repo/checks/import_matrix_test.go` does not allow
  `internal/state/desired` to import `internal/render/ceph`, which is correct —
  a validator must not depend on a renderer. The budget calculation would first
  have to move to a package both may import (`internal/storage/topology` is
  allowed by both), which also drags `MonitoringEnabled` and
  `PrometheusRetentionSize` along.
- Exit: move the root-FS budget into `internal/storage/topology`, then add a
  rule beside `validateManagedOSCephNodeRootDisk` that errors when a topology
  node's effective profile `diskGiB` is below `RootFilesystemFloorGiB` and warns
  when it is below `NodeRootFilesystemGiB`. Note the honest limit: `diskGiB` is
  raw disk and the preflight measures *free space*, so the validator can only
  catch the unambiguous case.
- Related: [ceph-cephadm-bootstrap-contract.md](ceph-cephadm-bootstrap-contract.md)

## B-072 — Standby `ceph-arbiter` candidates are built only on promotion
- Status: open (by design today — recorded so it is a decision, not a surprise)
- Area: storage/arbiter · apply scope
- Origin: vSphere arbiter provisioning review (2026-08-02)
- Problem: `managedMachineOSInstallGroupsVars`
  (`internal/render/inventory/vars_machine_os.go`) derives managed-OS machines
  strictly from `spec.ceph.topology.nodes[].machineRef`. A Machine that carries
  the `ceph-arbiter` capability but is not the current tiebreaker is in no
  topology, so `bootwright apply` never provisions it. It is built by
  `storage-cluster replace-arbiter`, which rewrites the input and then runs a
  scoped apply through the `deps` stage — so a candidate pool of three yields
  one live VM and two that materialize at promotion time. That is deliberate
  (`arbiter.ComputePromotion` refuses a machine that already names a topology
  node, and a standby in the topology would be a cephadm-enrolled host every
  apply must reach), but an operator who declares three candidates and expects
  three VMs after `apply` is surprised, and a promotion that has to build a VM
  is slower and riskier than one that only moves a mon.
- Exit: decide whether a warm standby is wanted. If so, the shape is a
  candidate list that provisions substrate + OS but is never enrolled into
  cephadm — distinct from `topology.nodes[]`, and it must not enter the mon
  count validators in `validate_storage_stretch.go`. If not, document the
  build-on-promotion behaviour where candidates are declared.
- Related: [ceph-arbiter-replacement.md](ceph-arbiter-replacement.md)
