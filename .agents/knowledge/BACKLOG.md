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
  to grab `:53` ahead of the intended listener. ADR 0055 moves split-DNS
  readiness before machines and limits controller lifecycle ownership to the
  one consumed managed service; neither change proves which process acquired
  the controller-local listener.
- Exit: pin the dnsmasq bind order (or bind address) so the controller resolver
  wins deterministically.
- Related: [external-dns-bootstrap.md](external-dns-bootstrap.md); ADR 0055

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

## B-086 — No preflight proves the release payload is reachable from the nodes
- Status: open
- Area: preflight
- Origin: ocp-prd-02 terminal bootstrap failure (2026-08-09), root-cause direction
- Problem: `CollectChecks` covers bastion tools, secrets, name resolution, SSH host
  trust, installer media and package sources, but nothing proves the cluster's
  release payload can be pulled — from the nodes or from anywhere. A registry path
  that is merely slow is what pushes a control-plane host past assisted-service's
  one-hour `Joined` stage timeout, which strands the rendezvous node (see
  `openshift-agent-host-error-strands-rendezvous.md`), and the first evidence of it
  arrives hours into the install.
- Exit: add a release-payload reachability check that runs against the node network
  rather than the controller, and report measured throughput, not just reachability
  — the failure mode is slowness, not refusal.

## B-088 — Most `cephadm shell` invocations still run unbounded
- Status: open
- Area: ansible / storage
- Origin: prd apply review (2026-08-09)
- Problem: `cephadm shell` starts a container, and a wedged teardown holds the
  command module's stdout pipe open, freezing the play on that banner with no
  error — the failure mode `ansible-ssh-liveness-timeouts.md` describes. Every
  retried poll in the role is now bounded and guarded by
  `TestAnsibleBoundsEveryRetriedCephadmShellPoll`, but roughly eighty single-shot
  invocations (idempotency probes, `override_rebuild` mutations, config
  get/set, `radosgw-admin` reads, crushtool round-trips) still run unbounded.
- Exit: bound them too, but per-command rather than mechanically — a `ceph orch
  apply` of a large spec, a `crushtool` round-trip and a `ceph fs rm` do not share
  one honest ceiling, and a blanket 120s would convert slow-but-working
  operations into failures. Widen the guard predicate to all `cephadm shell`
  argvs once they are covered.
