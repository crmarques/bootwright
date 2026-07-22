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
ceph daemon name. `ceph mon set_location` and the tiebreaker-joined-the-monmap
poll (matched against `ceph mon dump` names) take the same short name. See
[ceph-host-identity-namespaces.md](ceph-host-identity-namespaces.md).

**When it bites:** The OSD-readiness wait only anchors mon hosts that carry
OSDs. A stretch cluster's tiebreaker/arbiter has no OSDs, so its mon can still
be deploying when the topology operations run — `ceph mon set_location` and
`enable_stretch_mode` error for a mon absent from the monmap. The role polls
until the tiebreaker mon has joined so those fail-closed, no-retry ops do not
miss on a slow arbiter rollout.

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
`spec.ceph.topology.stretch` is set but `tiebreaker.host` and `tiebreaker.site`
are both empty, validation does NOT hard-fail the tiebreaker requirements — it
passes with a `Stretch tiebreaker` WARN advisory (arbiter-less stretch is an
incomplete, not-recommended setup: a data-site outage loses mon quorum). This
lets an estate stand up the two data sites first and add the mon-only arbiter
in a third site afterward. Rendering follows suit: the mon `set_location` ops
and the two-per-site CRUSH rule still emit, but `enable_stretch_mode` is skipped
until the tiebreaker is authored, so apply builds a coherent 2-replicas-per-site
cluster without the netsplit tiebreaker rather than a broken
`enable_stretch_mode` with an empty arbiter. A PARTIALLY authored tiebreaker
(one of host/site set) is still a hard error — that is a misconfiguration, not a
deferral. The `dataSites==2` and two-mons-per-data-site checks stay hard in both
cases.
