# Ceph stretch mode: CRUSH locations, the two-step rule, and the tiebreaker

**Constraint:** The stretch tiebreaker is a mon-only arbiter in a third site.
It must get a monmap location (`ceph mon set_location`, rendered separately)
but NOT a cephadm host-spec CRUSH `location`: cephadm creates the
failure-domain bucket at host add, so a location on the tiebreaker would add a
third datacenter bucket and `ceph mon enable_stretch_mode` refuses (EINVAL)
when the dividing bucket type has != 2 members.

**Constraint:** A host-spec CRUSH location is only meaningful in stretch mode,
where sites map to real failure-domain buckets. Without stretch the failure
domain is `host`, so a location would parent every host bucket under a bogus
bucket named after the site — outside `root=default`, where no CRUSH rule maps
PGs and **all pool I/O hangs**. Data-site hosts pin `root=default` so their
failure-domain buckets nest under the data root.

**Constraint:** The host spec alone does not guarantee the placement, because
cephadm buckets by `spec.hostname` while the OSDs create a bucket named after the
*short* host. A `set-crush-location-<node>` operation
(`ceph osd crush move <shortName> root=default <failureDomain>=<site>`) therefore
reconciles every sited, non-tiebreaker OSD host before the stretch rule and
`enable_stretch_mode` run. It is skipped when OSD readiness resolves to `skip`
(unmanaged OSD services): bootwright does not move buckets it never created, and
`crush move` on an absent bucket is ENOENT. See
[ceph-host-identity-namespaces.md](ceph-host-identity-namespaces.md).

**Constraint:** The stretch rule must place two replicas per data site:

```text
step choose firstn 0 type <failureDomain>
step chooseleaf firstn 2 type host
```

`ceph osd crush rule create-replicated` cannot express that two-step rule, so
the role compiles it into the CRUSH map directly. `crushtool` is not installed
on the host, so the de/compile steps run inside
the `cephadm shell` container with the work directory mounted. The rule renders
as a structured operation with no argv (idempotency kind `stretch-crush-rule`);
in the generated native-CLI `apply.sh` it becomes a `bw_stretch_crush_rule`
call, since it has no single-native-command form.

**Constraint:** Stretch mode requires the connectivity election strategy before
enabling: `ceph mon set election_strategy connectivity` precedes
`ceph mon enable_stretch_mode`. Both reconcile in place.

**Constraint:** The tiebreaker is authored as a node token, but
`enable_stretch_mode` wants the **mon name**, which is the host's *short* name,
not the orchestrator FQDN — canonicalize the token to a node name, then take the
ceph daemon name. `ceph mon set_location` and the mons-joined-the-monmap poll
(matched against `ceph mon dump` names) take the same short name. See
[ceph-host-identity-namespaces.md](ceph-host-identity-namespaces.md).

**When it bites:** OSD readiness anchors no mon at all. An `in` OSD on a host
says nothing about whether *that host's mon* was deployed — cephadm places mons
asynchronously, one per reconcile pass, and a mon can be filtered out or stalled
while OSDs on the same host are healthy; on a re-apply the OSDs already exist and
the OSD gate returns at once. `ceph mon set_location <mon> <domain>=<site>` and
`ceph mon enable_stretch_mode <tiebreaker> ...` are fail-closed, no-retry ops that
answer `ENOENT: mon.<name> does not exist` for any mon outside the monmap, so
`bootstrap_steps/mon_readiness.yml` polls `ceph mon dump` until **every** declared
mon has joined (not the tiebreaker alone — that gate let
`set-mon-location-<data-site-node>` fire against a monmap holding only the
bootstrap mon) and then fails closed with the mon service, mon daemon placement,
orchestrator hosts, live monmap and recent cephadm events. A mon that never
arrives is usually a mon-only condition: the host holds no address inside
`public_network` so cephadm dropped it from the mon placement (`does not belong to
mon public_network(s)`), the mon container never pulled, or 3300/6789 are blocked
between nodes so the daemon runs but never joins quorum.

**Constraint:** `ceph mon enable_stretch_mode` is also what re-homes the pools
nobody declares — `.mgr`, `.nfs`, `device_health_metrics` and the rgw metadata
pools (`.rgw.root`, `<zone>.rgw.*`) — onto the stretch rule at size 4 / minSize 2.
Bootwright's pool operations only cover authored `StoragePool` objects, so on the
arbiter-less path (no `enable_stretch_mode`) those pools keep CRUSH rule 0 at
size 3: three replicas spread by `host` across both data sites, which drops below
`min_size` when a whole site fails and takes the mgr and the whole object gateway
down with it. A `reconcile-stretch-internal-pools` operation (idempotency kind
`stretch-internal-pools`, phase `late-topology`) therefore enumerates the live
pools and sets `crush_rule`/`size`/`min_size` on every internal one. It runs in
the late phase, after the service-readiness gate, because rgw creates its pools
when its first daemon starts — earlier, the pools do not exist yet and there is
nothing to match. It also runs when the tiebreaker IS authored: pools created
after `enable_stretch_mode` do not get re-homed retroactively.

**Constraint:** Stretch replication is fixed at size 4 / minSize 2 (two
replicas per data site) — a Ceph requirement for two-site stretch. Non-4/2
stretch is unsupported: these are render-time domain constants, validation
rejects authored departures, and rendering applies them as the policy-less
default.

**Constraint:** A 3-node cluster is NOT stretch mode. True stretch requires two
data sites with two mon nodes each plus the arbiter (5 nodes minimum). A
3-node layout is a flat single-site topology where the third node runs only a
tie-breaking monitor: with one OSD host down, I/O pauses but mon quorum (3
mons) survives — quorum HA, not data HA across sites.

**Behavior:** The tiebreaker may be authored later. When
`spec.ceph.topology.stretch` is set but `tiebreaker.node` and `tiebreaker.site`
are both empty, validation does NOT hard-fail the tiebreaker requirements — it
passes with a `Stretch tiebreaker` WARN advisory (arbiter-less stretch is an
incomplete, not-recommended setup: a data-site outage loses mon quorum). This
lets an estate stand up the two data sites first and add the mon-only arbiter
in a third site afterward. Rendering follows suit: the mon `set_location` ops
and the two-per-site CRUSH rule still emit, but `enable_stretch_mode` is skipped
until the tiebreaker is authored, so apply builds a coherent 2-replicas-per-site
cluster without the netsplit tiebreaker rather than a broken
`enable_stretch_mode` with an empty arbiter. A PARTIALLY authored tiebreaker
(one of node/site set) is still a hard error — that is a misconfiguration, not a
deferral. The `dataSites==2` and two-mons-per-data-site checks stay hard in both
cases. The normative statement of this rule belongs in `specs/state-model.md`
under `spec.ceph.topology.stretch`; what this file records is the mechanics.
