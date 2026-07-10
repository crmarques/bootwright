# Managed-OS install: ownership probe and override-rebuild gates

The anaconda managed-OS install role decides per host whether to install,
skip, or refuse. These are the fail-closed edges and the facts that drive them
(machine_os_install_anaconda: probe_existing.yml, marker.yml, wait.yml).

**Reachable-but-unverifiable fails closed:** the one combination that would wipe
a host blind is SSH port REACHABLE but `osInstall.ssh.knownHostsPath` not
configured — the ownership probe is skipped, readiness defaults to false, and
the install would proceed onto a host that could be foreign or lost-record
production. A truly greenfield host has the port closed and never trips this;
only reachable-but-unverifiable hosts are refused.

**Owned and matching are separate predicates:** `bootwright_os_pre_marker_owned`
(marker owner == `bootwright`) is independent of
`bootwright_os_pre_marker_matches` (marker hash equals desired). `--override`
may rebuild an OWNED host whose hash drifted, but must NEVER adopt, reinstall,
or wipe a host Bootwright does not own — a reachable host without a
Bootwright-owned marker is foreign (or manually prepared) and fails closed even
under `--override`.

**Override only suppresses the fail — a dedicated fact drives the rebuild:**
`bootwright_managed_os_force_rebuild` = already_ready AND owned AND
not-matching AND apply_mode==override. Without it the drifted host is silently
skipped as "already ready" — and its marker would be re-stamped to the new
desired hash without the OS ever changing.
`bootwright_managed_os_install_required` = not-already-ready OR force_rebuild;
every downstream "did this run (re)install?" gate keys off install_required,
never off not-already-ready, so a forced rebuild behaves exactly like a fresh
install.

**Post-boot wait gates on install_required:** on an `--override` rebuild the old
OS is still up (already_ready) when the run power-cycles the node into the
installer — a "port started" wait gated on not-already-ready would match the
old OS's SSH immediately and race ahead of the reinstall.

**Marker stamping is ownership-gated:** `bootwright_managed_os_stamp_owned`
allows the stamp only for a host freshly installed this run or already owned
(matching or override-rebuilt). A foreign host fails closed earlier and never
reaches the stamp, but the gate exists so a future task reordering cannot
launder a foreign host into an owned one. The kickstart `%post` writes the
marker on a fresh install; the marker task skips the redundant SSH rewrite on a
converged host and reports `changed` honestly when a real (re)write happens.

**Destroy-protection remedy must route machine substrate to the infra stage:**
machine-substrate kinds (managed-OS install, per-host machine-infra steps) are
torn down ONLY by the infra stage — a clusters-stage `destroy --override` never
touches their convergence records, so pointing a blocked managed-OS machine at
the clusters destroy loops forever (destroy, re-apply, blocked again).
`overrideDestroyRemedy` therefore emits
`bootwright destroy --stage infra --clusters <affected> --override`, and hints
`--skip-unreachable` because a machine whose host substrate was never
provisioned or is powered off (e.g. a nested cluster on a host cluster that
never came up) would otherwise fail closed at the infra destroy.
`OverrideDestructiveMachineSubstrate` reports the distinct clusters so the
remedy can scope the command. Pinned by
TestCheckApplyOverrideDestroyProtectionMachineSubstrateRemedy (which also locks
out the old dead-end "for that scope" guidance).
