# Ceph diff (live) and --adopt: facet filters, advisories, and adopt policy

**Semantics:** `bootwright diff` runs live by default (`--recorded` is the
opt-out; `--adopt` implies live). It derives each facet's desired value the way apply
would (via `internal/storage/topology`, the same resolver the renderer uses),
so a pool's effective size or a rule's failure domain match exactly. Desired is
the `---` baseline, live is `+++`. An object declared but absent is
desired-only; an object on the cluster but not declared is real-only — under
the storage additive-only rule that is NOT drift, it is the candidate
`--adopt` pulls into desired state. An external (imported) or unprobed cluster
yields a report with no facets.

**Semantics:** Real-only filters keep Ceph-internal objects out of the diff:
pools `.mgr`, `device_health_metrics`, `.nfs`, and every `*.rgw.*` pool — RGW
auto-creates `.rgw.root` and the per-zone `<zone>.rgw.log/control/meta/otp`
and `<zone>.rgw.buckets.*` pools whenever a gateway is declared; none is ever
authored, so none may be flagged real-only or synthesized into adopt YAML.
Live infra services are filtered the same way: `crash`, the monitoring stack
(`prometheus`, `grafana`, `alertmanager`, `node-exporter`, `ceph-exporter`),
and the HA management daemons (`mgmt-gateway`, `oauth2-proxy`, their keepalive
ingress) are deployed by cephadm/management, not modeled by the desired side.
Config, crush-rules, and mgr-modules facets ignore real-only entirely (their
live side is dominated by Ceph defaults, not operator intent).

**Semantics:** Health diffs only the status against the implicit `HEALTH_OK`
invariant. A transient `HEALTH_WARN` is treated as OK, matching live `diff`'s
`storageHealthDegraded` gate (only `HEALTH_ERR` is degraded) — the
two read-only surfaces must agree on whether a WARN cluster is in sync. OSD
counts are deliberately not compared as health fields.

**Semantics:** The osd-devices facet compares only hosts that pin plain
`/dev/<name>` kernel paths, whose basenames compare reliably against the short
device names `ceph osd metadata` reports (`devices`, e.g. `sdb` or `sdb,sdc` —
the only read mapping an OSD to its physical devices). Stable aliases
(`/dev/disk/by-*`, `/dev/mapper`) are already reconstruction-faithful and not
comparable to kernel names, so they are left uncompared. Filter/`all` hosts
surface as `UnpinnedOSDHosts` reconstruction advisories carrying the devices
actually consumed — excluded from `InSync()` (the filter intent is satisfied)
and never auto-pinned: pinning would disable cephadm's automatic consumption of
replacement disks, an intent change only the operator should make.

**Semantics:** An absent discovery read means "unknown, do not diff", never
"empty on the cluster" (`ErrReadAbsent`). A malformed individual read is kept
as raw bytes and fails per facet, so one bad blob never blinds the whole diff.
`Probed: false` means the seed was reached but no read succeeded (e.g. never
bootstrapped); an unreachable cluster has no Discovery at all.

**Semantics:** Adopt policy — only cleanly representable facets auto-fold:
replicated pool sizing, declared `spec.ceph.config` values, purely additive
OSD-device pins (a host pinning explicit per-host paths that grew OSDs out of
band gets the new devices appended), and pools that exist only on the cluster
(a new sibling StoragePool file). Everything else is reported as
detected-but-not-adopted — adopt never silently drops a difference it cannot
safely fold in, never rewrites or removes an existing entry, and writes only
through the workspace input-mutation component, which snapshots history first.

**Semantics:** Adopt refusals worth knowing: `public_network` /
`cluster_network` are owned by `spec.ceph.networks`, not `config` — adopting
them under config would be rejected on the next load. A live erasure-coded
pool is never synthesized: discovery exposes only the profile name, not the
k/m chunk counts, so the file would carry `ceph.type: erasure` with no erasure
block and fail the next load/validate. A host whose devices come from a
drivegroup or `pathSpecs` (per-device CRUSH class) is not safely appendable,
and a declared device that no longer backs an OSD is reported, never removed.
