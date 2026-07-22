# Ceph host identity: orchestrator FQDN vs ceph short name

**Root cause:** A cephadm cluster carries *two* names for the same host and they
are not interchangeable.

- **Orchestrator identity** — the hostname registered with `ceph orch host add`.
  cephadm requires it to equal `hostname` output on the node, so with ADR 0017
  node identity it is the node **FQDN**. It is what `ceph orch host ls`,
  `ceph orch ps` (`hostname:`), `ceph orch device ls` (HOST), host specs
  (`service_type: host`), and service `placement.hosts` speak.
- **Ceph-internal identity** — the **short** name (first DNS label). Ceph derives
  it itself and never sees the rendered spec: `ceph-osd` builds its default
  CRUSH location by truncating `gethostname()` at the first dot, so CRUSH host
  buckets in `ceph osd tree` are short; cephadm likewise names mon/crash/exporter
  daemons after the short host, so `ceph mon dump` names, `ceph mon set_location`,
  and `ceph mon enable_stretch_mode` take the short name.

**When it bites:** Comparing an orchestrator FQDN against a Ceph-internal name
never matches, and the mismatch is silent — the poll simply never satisfies.
The OSD readiness gate spent its full retry budget and then failed closed with
`did not create an in OSD placed under dynamic-selection host <fqdn> in the
CRUSH map` while reporting `30 OSD daemon(s) exist (0 stray, 0 down)` — the
OSDs were healthy and `in` under CRUSH buckets `node-01`…`node-06`. The same
mismatch aims `ceph mon set_location` / `enable_stretch_mode` at a mon name that
is absent from the monmap, and starves the tiebreaker-joined-the-monmap poll in
`bootstrap.yml`, which matches against `ceph mon dump` names.

**Contract:** Anything compared against CRUSH or monmap output resolves through
`topology.CrushHostNames` (match set: short plus FQDN, so a bare-hostname estate
and a pre-created FQDN bucket both match) or `topology.CephDaemonName` (single
argument value: short). `osdReadiness.dynamicHosts` therefore renders as
`{name, crushNames}` objects rather than bare hostnames — `name` is the
orchestrator identity used in operator-facing messages, `crushNames` is what the
CRUSH tree is matched on. Anything compared against *orchestrator* output
(`ceph orch device ls`, `ceph orch ps`) keeps using the FQDN `cephHostname`.

**Constraint:** A cephadm host spec `location` (stretch data sites) is applied by
cephadm as `osd crush add-bucket <spec.hostname>`, and bootwright cannot restate
that name — it is cephadm's own call. If cephadm buckets by the FQDN, the real
short host bucket the OSDs create is left parented directly under `root=default`,
outside the failure domain, and the stretch rule's
`choose … type <failureDomain>` then finds only empty buckets: **PGs never map
and pool I/O hangs**. So the host spec keeps its declarative `location` and a
`set-crush-location-<node>` topology operation reconciles the real bucket with
`ceph osd crush move <shortName> root=default <failureDomain>=<site>`. That move
is correct under both cephadm behaviors — a no-op when cephadm already shortened
the name, the actual repair when it did not — and an empty FQDN host bucket left
inside the failure domain is inert (weight 0, never selected by straw2).
