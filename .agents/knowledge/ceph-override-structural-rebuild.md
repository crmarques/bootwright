# Ceph --mode rebuild structural rebuilds: identity, fail-safes, and EC pools

**Semantics:** A rendered create operation carries the sub-object's IMMUTABLE
identity in a `structural` block — the only desired-state difference that
warrants a data-destroying `--mode rebuild` rebuild, because Ceph cannot change it
in place. For a pool that is `type` plus the full erasure profile (every
authored field including opaque `parameters`, keyed by their
`erasure-code-profile get` JSON spellings, stringified); for a CephFS it is the
metadata pool AND the default data pool. Replica size/min-size, CRUSH rule,
application, quota, compression, and autoscale are NOT structural — they
reconcile in place via the `set-*` operations and are never destroyed.

**Semantics:** A rebuild runs only under `--mode rebuild` AND on a proven
structural mismatch against the LIVE cluster (op `structural` vs the probed
live object). `create`/`continue` never reach the branch. Fail-safe rules
throughout: an unreadable live value never triggers a destructive rebuild (an
unreadable pool type falls through to the in-place reconcile); an
absent/unreadable EC profile is just a create; the live profile is narrowed to
the authored keys before comparing, so Ceph's defaulted keys (`w`,
`packetsize`, ...) never force a rebuild — but an authored key missing from the
live profile reads as a mismatch.

**Semantics:** `bootwright_ceph_rebuild_authorized_clusters` is exclusively the
controller's structural-drift allowlist. Markerless recovery after an interrupted
first bootstrap uses the separate
`bootwright_ceph_incomplete_bootstrap_authorized_clusters` allowlist because no
successful converge record exists to classify structural drift. That second list
requires the selected managed cluster's exact controller owner record and desired
seed plus consumed rebuild and `data-loss` intent; Ansible re-validates the same
record and host evidence before a shared cleanup predicate can authorize
`rm-cluster --zap-osds`. Neither list implies the other.

**Semantics:** Live probes used by the decision: pool type is the numeric
`type` field of `ceph osd pool ls detail` (1 = replicated, 3 = erasure).
`ceph fs ls` does not expose data-pool ordering; `ceph fs get` does — the
FIRST entry of the live filesystem's mdsmap `data_pools` is the default data
pool, resolved from pool id to name via `ceph osd pool ls detail`.

**Semantics:** The EC profile is immutable in Ceph and cannot be deleted while
a pool uses it, so its rebuild runs at the create-ec-profile op (which precedes
the pool create): tear down the ONE dependent pool (data-destroying for that
pool) and the stale profile; the op's idempotency probe then sees the profile
absent and recreates it, and the create-pool op recreates the pool against the
new profile.

**Semantics:** Pool deletion is re-disabled (`mon_allow_pool_delete`) in an
`always:` block even when the `pool rm` failed, so a half-finished rebuild
never leaves the cluster permissive; the failure surfaces via the assert.

**Constraint:** omission never renders a removal operation. For one config key
or mgr module, the supported sequence is the native command first
(`cephadm shell -- ceph config rm <who> <option>` or
`cephadm shell -- ceph mgr module disable <module>`), successful live
verification, then deletion from desired state. A future renderer must not
derive `rm` or `disable` from an absent entry; an in-product remover needs an
explicit target and the same authorization/preview/matrix treatment as every
other state-changing operation.

**Constraint:** An erasure-coded pool's create must say `erasure <profile>` —
without it Ceph **silently creates a replicated pool**. The profile must exist
before the create. `size`/`min_size` derive from k+m on EC pools and the CRUSH
rule comes from the profile, so none of the replicated `set-*` operations
apply. RBD images and CephFS data on EC pools require
`allow_ec_overwrites true`; without it the pool converges but every client
write fails.

**Constraint:** A placement policy's CRUSH device class is the optional
trailing argument of `crush rule create-replicated` and is fixed at create
time — changing the class means a new `ruleName`.

**Semantics:** StorageCluster structural-hash invariants (what must NOT move
the hash, because `ceph orch apply` reconciles it in place): scaling out by
adding an OSD host, rebalancing mon/mgr/mds daemons by editing host roles,
enabling the mgmt-gateway/dashboard HA, and editing a cephadm service
passthrough. A change to cluster identity (e.g. the public network) must still
move it — otherwise apply refuses the edit or `--mode rebuild` wipes the cluster's
OSDs for a reconcilable change. RGW gateways, NFS exports, and StorageExports
are stateless services: a config or placement edit classifies as reconcilable
drift (non-empty, stable structural hash), never a structural rebuild.
