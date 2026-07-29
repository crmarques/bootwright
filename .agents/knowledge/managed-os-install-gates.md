# Managed-OS install gates: reachability, ownership, override rebuild

Implementation predicates behind the documented ownership model
(docs/advanced/ownership-and-safety.md), from
`machine_os_install_anaconda/tasks/{probe_existing,wait,marker,main}.yml`.

**Port-probe predicate:** `ansible.builtin.wait_for` with `failed_when: false`
always registers `failed=False` (the failure is suppressed), so `.failed` is a
dead signal — it cannot distinguish a reachable host from a timed-out one. The
module sets `msg` only on a failure path (e.g. `Timeout when waiting...`), so
absence of `msg` is the reliable "port is open" predicate. Everything
downstream keys reachability off that fact.

**Probe order (2026-07-23):** reachability (port probe) → substrate-release
membership resolution (`bootwright_substrate_reset_clusters` plus the
`<cluster>/<machine>` pairs in `bootwright_substrate_reset_machines`) → trust
gate (reachable with no `knownHostsPath` refuses) → primary auth as
`osInstall.ssh.user` against the PINNED host key
(`StrictHostKeyChecking=yes`) → fallback auth as
`osInstall.ssh.fallbackUser` (only when the primary was rejected and the
render emitted one) → fail-closed unverifiable refusal (reachable, neither
identity authenticated, machine not released) → marker read as whichever
identity authenticated (`bootwright_os_probe_user`) → `--mode create` live gate
(apply mode `create` refuses a host that already runs an OS, owned or not,
unless released) → foreign/drifted mode gates (all suppressed for a released
machine) → release force-rebuild.

**Reachable but unverifiable fails closed — two shapes:** (1) the SSH port is
REACHABLE but no `knownHostsPath` is configured, so the ownership probe cannot
run at all. (2) Since 2026-07-23: reachable WITH trust configured but every
auth probe rejected — wrong/rotated authorized key, or a changed host key (the
probe pins the recorded key and never re-accepts a different one). Shape (2)
previously fell through as `already_ready=false` → "no OS present" →
unconditional reinstall (disk wipe) in ANY apply mode with no ownership check.
Both shapes now hard-refuse unless the machine's substrate release covers it,
naming the remedies: restore key access for the machine's `access.ssh`
identity; `bootwright machine trust --replace <machine>` after an authorized
out-of-band rebuild changed the host key; `bootwright destroy --stage infra
--machines <m> --force` (or `--clusters <c>`) then re-apply; power off a truly
unused host. A greenfield host has the port closed and never trips this.

**Fallback probe identity for root-revoked nodes:** when the Machine sets
`access.rootLogin: revoke` and its managed StorageCluster's
`cephadm.clusterSSH.user` is non-root, the render emits that account as
`osInstall.ssh.fallbackUser` (`vars_machine_os_install.go`
`managedOSFallbackSSHUser`), EXCLUDED from the install-marker hash
(`vars_machine_os_marker.go` deletes it from the stable input) so adding it
never reclassifies an existing install. The probe retries auth as that account
and reads the marker as it, so a post-revoke rerun classifies a healthy owned
node as present instead of tripping the unverifiable refusal — the 2026-07-23
ceph-prd-01 incident shape, where the cluster's own root revocation would have
sent the next apply down the wipe path. Downstream tasks (the wait-phase auth
verify, nmstate configure, the controller-side marker rewrite) reuse the
identity that authenticated as `bootwright_os_effective_user` (fresh install →
the primary install user) and elevate with `sudo -n` instead of assuming
root.

**Ownership and match are separate predicates:** a Bootwright-owned marker is
one whose owner == bootwright, regardless of whether its hash still matches
desired. `--mode rebuild` may rebuild an OWNED host whose hash drifted, but must
NEVER adopt, reinstall, or wipe a host Bootwright does not own — a reachable
host without a Bootwright-owned marker is foreign (or manually prepared) and
fails closed even under `--mode rebuild`.

**Override rebuild is driven, not merely permitted:** the drifted-owned refusal
only suppresses the fail; a separate `bootwright_managed_os_force_rebuild` fact
feeds `bootwright_managed_os_install_required` so the destructive rebuild is
actually carried out. Without it the host is silently skipped as
`already_ready` — which also re-stamps its marker to the new desired hash
without the OS ever changing. Every downstream "did this run (re)install?"
gate keys off `install_required`, never off not-already-ready, so a forced
rebuild behaves exactly like a fresh install.

**A destroy-released substrate forces rebuild even when the marker still
matches:** `machine_substrate_baremetal/tasks/destroy.yml` never wipes operator
hardware in place — it only clears Bootwright's local records and documents
that "the OS disk is reclaimed by the next apply reinstall." Until 2026-07-23,
`bootwright_managed_os_force_rebuild` additionally required
`not bootwright_os_pre_marker_matches`, so a bare-metal host destroyed and
re-applied with an UNCHANGED desired spec kept answering SSH with its old,
still-matching marker and the reinstall was silently skipped — `destroy` then
`apply` was not equivalent to a from-scratch apply. Membership in the
substrate release (a prior destroy's release record, see
[substrate-ownership-markers.md](substrate-ownership-markers.md)) is now
sufficient on its own to force the rebuild, independent of whether the on-host
marker happens to match; a destroy's own record of "this substrate was torn
down" outranks the still-live host's self-reported hash. Membership is
machine-granular: the cluster must appear in
`bootwright_substrate_reset_clusters` AND the machine must be covered — either
the release record carries no machine list for that cluster (a cluster-scoped
destroy released everything) or the `<cluster>/<machine>` pair appears in
`bootwright_substrate_reset_machines` (written by `destroy --machines`); an
uncovered sibling machine keeps its normal gates. With `--authorize unreachable-nodes`, a
managed storage cluster receives the release only when the storage completion
report proves that no node was skipped; partial storage and teardown paths
without equivalent per-node proof withhold it, so skipped nodes keep failing
closed.
No change was needed in `marker.yml` or
`wait.yml` — both already key off `install_required`, and kickstart's `%post`
(not the ansible task) is what stamps a freshly reinstalled host's marker, so
the rebuilt host is stamped correctly either way.

**Stamp gating:** the marker/ownership is stamped only for a host Bootwright
legitimately owns — freshly installed this run (was not reachable before), or
already owned (matching, or override-rebuilt). The stamp is gated explicitly so
a future task reordering cannot launder a foreign host into an owned one.

**Old-SSH-stop wait:** on an override rebuild the old OS is still up
(`already_ready`) when the run power-cycles it into the installer. The "wait
for previous managed OS SSH port to stop" task is gated on `install_required`,
not not-already-ready — otherwise the following "port started" wait matches the
old OS's SSH immediately and races ahead of the reinstall.

**Marker writes:** the kickstart `%post` already writes the marker on a fresh
install, and the probe records whether the on-host marker matches desired. The
controller-side SSH rewrite runs only when it does not match, and reports
changed honestly instead of hiding the remote mutation behind
`changed_when: false`.

**Media cleanup after SSH is ready:** the boot-component fact exists only when
this run actually installed the machine — an already-ready machine attached no
media, so the cleanup include is gated on the fact being defined (or it would
template an undefined var). Bare metal carries no `mediaPrepareRole`, so that
cleanup path is skipped; its BMC virtual media is ejected through the rendered
`cleanupMediaRole` instead (the registry sets that role only for boot drivers
owning a `cleanup_media` action) — otherwise the media stays mapped and the
node keeps a lingering `/dev/sr0`.

**Skip-noise shape:** the install body is a dynamic include gated at the
include (`Install managed OS media when needed`), so an already-installed node
prints a single skipped line instead of ~31 individually skipped inner tasks;
the block inside keeps the same guard as defense-in-depth.

**Destroy-protection remedy routes machine substrate to the infra stage:**
machine-substrate kinds (managed-OS install, per-host machine-infra steps) are
torn down ONLY by the infra stage — a clusters-stage `destroy --force` never
touches their convergence records, so pointing a blocked managed-OS machine at
the clusters destroy loops forever (destroy, re-apply, blocked again).
`overrideDestroyRemedy` therefore emits
`bootwright destroy --stage infra --clusters <affected> --force`, and hints
`--authorize unreachable-nodes` because a machine whose host substrate was never
provisioned or is powered off (e.g. a nested cluster on a host cluster that
never came up) would otherwise fail closed at the infra destroy.
`OverrideDestructiveMachineSubstrate` reports the distinct clusters so the
remedy can scope the command. Pinned by
TestCheckApplyOverrideDestroyProtectionMachineSubstrateRemedy (which also locks
out the old dead-end "for that scope" guidance).

**`customizations.storage.wipe` was REMOVED (2026-07-23):** the field was
decorative — no validator, renderer var consumer, or playbook ever read it,
and `ks.cfg.j2`'s `clearpart`/`zerombr` are unconditional for a machine whose
install is authorized — so it was deleted from `MachineInstallStorage`
(`api/v1alpha1/machine.go`); strict decode now rejects it (closes backlog
B-001). Do not re-add a wipe toggle without actually wiring it into the
kickstart render; `internal/render/inventory/managed_os_test.go` pins that the
render emits no `wipe` var. Disk-wipe consent lives in the install
authorization gates above, not in a per-profile field.
