# Ceph service rollout: why HEALTH_OK does not mean the services are deployed

**Root cause:** `ceph orch apply` is asynchronous. It records the service spec and
returns; cephadm then schedules and pulls and starts the daemons on its own
cadence. Applying a spec therefore proves nothing about the daemons, and neither
does cluster health: mds, rgw, ingress, prometheus, grafana and alertmanager are
not health-check inputs, so a cluster whose entire object gateway is still
deploying reports `HEALTH_OK`.

**When it bites:** apply ran to completion, recorded ownership, and reported
success, while `ceph orch ls` showed `rgw.odf 0/6`, `mds.odf-cephfs 0/6`,
`ingress.rgw.odf.dc1 0/6`, `prometheus 1/4`, `grafana 1/4`, `alertmanager 1/4`
with `REFRESHED -` and `VERSION <unknown>`. The OSD readiness poll had passed
(OSDs are gated) and the final-health wait had passed (`HEALTH_OK`), so nothing
in the run noticed. A post-apply validation run minutes later reads as drift when
it is really an ungated rollout still in flight — and a genuinely stuck service
(image pull failure, port conflict, a placement that resolves to no host) reads
as success forever.

**Contract:** `bootstrap_steps/service_readiness.yml` runs after the late service
specs and the secret-bearing management services, and before the late operations
and the final health gate. It polls `ceph orch ls --format json` until every
managed non-`osd` service satisfies `status.running >= status.size`, then fails
closed with the per-service running/declared counts, `ceph orch ps`, and
`ceph log last 100 cephadm`. OSD services are excluded because
`osd_readiness.yml` already gates them against the CRUSH map, which is stricter
than a daemon count. Tunable through `bootwright_ceph_service_readiness_retries`
(default 60) and `bootwright_ceph_service_readiness_delay` (default 10).

**Constraint:** the poll is expressed as a pure Jinja expression, repeated
verbatim in `until` and in the `set_fact` that records the verdict — `until`
conditionals cannot carry `{% %}` statements and task-level `vars` are not a
dependable re-evaluation point inside a retry loop. The `running >= size`
comparison is elementwise without a comprehension: zip the two attribute lists,
`map('max')`, and compare the result to the running list.

**Consequence for ordering:** anything that reads objects a daemon creates at
startup must run after this gate. The rgw metadata pools (`.rgw.root`,
`<zone>.rgw.*`) do not exist until the first rgw daemon boots, which is why the
`stretch-internal-pools` reconcile is a `late-topology` operation — see
[ceph-stretch-mode-constraints.md](ceph-stretch-mode-constraints.md). The
generated native-CLI `apply.sh` has no equivalent wait, so its late stage is
best-effort and its stretch internal-pool helper reports when it matches nothing.
