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

**Reachable but unverifiable fails closed:** when the SSH port is REACHABLE but
no `knownHostsPath` is configured, the ownership probe is skipped, readiness
defaults to false, and the install would wipe an occupied host blind — foreign,
or lost-record Bootwright production. That combination is refused. A truly
greenfield host has the port closed and never trips this.

**Ownership and match are separate predicates:** a Bootwright-owned marker is
one whose owner == bootwright, regardless of whether its hash still matches
desired. `--converge-drifted` may rebuild an OWNED host whose hash drifted, but must
NEVER adopt, reinstall, or wipe a host Bootwright does not own — a reachable
host without a Bootwright-owned marker is foreign (or manually prepared) and
fails closed even under `--converge-drifted`.

**Override rebuild is driven, not merely permitted:** the drifted-owned refusal
only suppresses the fail; a separate `bootwright_managed_os_force_rebuild` fact
feeds `bootwright_managed_os_install_required` so the destructive rebuild is
actually carried out. Without it the host is silently skipped as
`already_ready` — which also re-stamps its marker to the new desired hash
without the OS ever changing. Every downstream "did this run (re)install?"
gate keys off `install_required`, never off not-already-ready, so a forced
rebuild behaves exactly like a fresh install.

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
`--skip-unreachable` because a machine whose host substrate was never
provisioned or is powered off (e.g. a nested cluster on a host cluster that
never came up) would otherwise fail closed at the infra destroy.
`OverrideDestructiveMachineSubstrate` reports the distinct clusters so the
remedy can scope the command. Pinned by
TestCheckApplyOverrideDestroyProtectionMachineSubstrateRemedy (which also locks
out the old dead-end "for that scope" guidance).

**`customizations.storage.wipe` is a latent dead field:** the renderer projects
a var for it, but `ks.cfg.j2` never reads that var — the kickstart's `clearpart`
is unconditional, so authoring `storage.wipe: false` does NOT preserve existing
partitions. Do not treat the field as a working guard; wiring it up (or removing
it) is unfinished work, and any managed-OS install currently wipes the disk
regardless of its value.
