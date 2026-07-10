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

**Constraint:** The stretch rule must place two replicas per data site:

```text
step choose firstn 0 type <failureDomain>
step chooseleaf firstn 2 type host
```

`ceph osd crush rule create-replicated` cannot express that two-step rule, so
the role compiles it into the CRUSH map directly. `crushtool` is not installed
on the host (only cephadm + ceph-common), so the de/compile steps run inside
the `cephadm shell` container with the work directory mounted. The rule renders
as a structured operation with no argv (idempotency kind `stretch-crush-rule`);
in the generated native-CLI `apply.sh` it becomes a `bw_stretch_crush_rule`
call, since it has no single-native-command form.

**Constraint:** Stretch mode requires the connectivity election strategy before
enabling: `ceph mon set election_strategy connectivity` precedes
`ceph mon enable_stretch_mode`. Both reconcile in place.

**Constraint:** The tiebreaker is authored as a machine name, but
`enable_stretch_mode` wants the **mon name**, which is the registered
(fully-qualified) hostname — the machine token must be canonicalized first.

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
