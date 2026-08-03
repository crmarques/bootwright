# Ceph monitoring service specs: retention keys, Loki wiring, defaults

**Constraint:** `retention_time` / `retention_size` exist only on cephadm's
`PrometheusSpec`. Every other monitoring service (loki, promtail, grafana,
alertmanager, node-exporter) maps to `MonitoringSpec`, which rejects the keys
and fails `ceph orch apply -i`. Retention therefore validates and renders for
the `prometheus` service only.

**Constraint:** There is no `ceph dashboard set-loki-api-host` command: the mgr
dashboard's `set-*` commands are generated 1:1 from its Options, none of which
is `LOKI_API_HOST`. Emitting one would fail every apply. cephadm provisions
Grafana's Loki datasource itself, so authoring `monitoring.loki` renders only
the service spec — no dashboard wiring op.

**Constraint:** Explicit monitoring specs render only for services with a
declared placement — via the `prometheus`/`grafana`/`alertmanager` topology
roles or an authored placement block. A service with neither keeps cephadm's
own default deployment, so a zero-config cluster renders no monitoring specs
at all. A role-less service (`node-exporter`, `loki`, `promtail`) renders only
when its block is authored; with no block, cephadm's all-hosts default
deployment stands.

**Constraint:** `MonitoringEnabled` semantics: an absent `monitoring` block or
`enabled` nil/true means the cephadm default stack deploys;
`enabled: false` renders `--skip-monitoring-stack` and no specs.

**Constraint:** The management gateway is a cephadm singleton (no
`service_id`). Both the mgmt-gateway and its `keepalive_only` ingress land on
the same resolved ingress hosts — the local gateway is the one the keepalived
VIP fronts when it floats to that host. In `keepalive_only` mode the ingress
contributes only the VIP/failover: `backend_service: mgmt-gateway` and no
HAProxy frontend is rendered (the gateway, not HAProxy, reverse-proxies the
dashboard and monitoring UIs). `enable_auth` renders only when authored so an
unset spec keeps cephadm's default (off).

**Root cause:** cephadm's classic mgr dashboard module keeps its own HTTPS
listener (`mgr/dashboard/ssl_server_port`, default `8443`) bound on every mon
host via the mgr container's host-network port mapping. Ceph does not disable
that listener automatically when a `mgmt-gateway` spec is applied — upstream
docs require the operator to flip `mgr/dashboard/ssl` off as a manual
prerequisite. If that never happens, the mgmt-gateway nginx daemon (also
host-port `8443` by default) fails to bind on every host that also runs a mgr
daemon (`CEPHADM_DAEMON_PLACE_FAIL: ... Cannot bind to IP 0.0.0.0 port 8443:
[Errno 98] Address already in use`), while it starts fine on ingress hosts
that don't run mgr — the service readiness gate
([ceph-service-rollout-gate.md](ceph-service-rollout-gate.md)) then reports
`mgmt-gateway (running/mon-count)` stuck partway forever while every other
service reaches full count.

**Trap, third order (fixed):** the whole mitigation above lives in
`management_services.yml`, and the Go inventory only emitted the `management`
context vars that drive it when the gateway carried TLS or oauth2-proxy
material. A declared gateway *without* secrets — the only shape a vendor
build accepts since ADR 0047 — therefore skipped the SSL flip, the port move,
and the mgr restarts entirely, and the late-services gateway doc placed
daemons only on the mgr-less hosts. Observed 2026-08-03 on a fresh prd apply:
`mgmt-gateway (2/6)` with exactly the two ingress nodes that run no mgr up.
`storageMgmtGatewayVars` now emits the vars for every declared gateway
(tls/oauth2 keys only when authored), so the management phase always frees
the port and (re)applies the gateway document after it; only the material
inside the assembled spec stays secrets-gated.

**Trap:** `ceph mgr fail` does not fix this. It only tells the mons to hand
"active" to an already-running standby — it never restarts the standby mgr
processes, so their dashboards never re-read `mgr/dashboard/ssl=false` or
release the port; the failure reproduces identically after `mgr fail`.

**Trap, worse:** `ceph orch restart mgr` is not a restart either. cephadm
only *schedules* per-daemon restart actions for its serve loop
(`_schedule_daemon_action`), and on ceph-prd-01 (Ceph 20.2.2, 2026-07-23)
those scheduled actions sat unexecuted for 20+ minutes: every mgr kept its
original container while the play's bounded container-id wait quietly hit
its retry cap and fell through, the mgmt-gateway spec was applied against
mgr hosts still holding 8443, and the failure only surfaced 15 minutes
later as a misattributed service-readiness timeout (`mgmt-gateway 2/6`
with daemons present only on the two mgr-less hosts).
`management_services.yml` therefore restarts the `ceph-<fsid>@mgr.*`
systemd units directly on every cluster host (delegated per inventory
host; hosts without a mgr no-op via the unit glob), snapshots every mgr
`container_id` before, polls `ceph orch ps --daemon-type mgr` after, and
**asserts** full container turnover instead of proceeding on timeout. It
also probes `ceph mgr services` on every run: an `https://` or wrong-port
dashboard URL forces the restart path even when the config values are
already correct, so a run that died between the config flip and the
restart self-heals on the next apply. The mgmt-gateway spec apply retries
until rc==0 while the restarted active mgr's orchestrator warms back up.

**Trap, second order:** disabling `mgr/dashboard/ssl` makes the dashboard
fall back from its HTTPS port (`ssl_server_port`, default `8443`) to its
plain-HTTP port (`server_port`, default `8080`) — and `8080` is also the
conventional cephadm RGW frontend port (`rgw_frontend_port`) that
`StorageObjectGateway` renders. On any host that colocates `mgr` with `rgw`,
the dashboard module then fails to bind and shows up as its own separate
health error — `MGR_MODULE_ERROR: Module 'dashboard' has failed: Port 8080
not free on <mgr public IP>` — distinct from the `CEPHADM_DAEMON_PLACE_FAIL`
above and only surfacing on the subset of mgr hosts that also run rgw.
`management_services.yml` therefore also pins `mgr/dashboard/server_port` to
`bootwright_ceph_dashboard_http_port` (default `8081`, tunable) whenever it
isn't already that value, folds that into the same "did anything change"
check as the SSL flip, and restarts mgr once for both changes together. The
dashboard's HTTP endpoint is never exposed externally once mgmt-gateway
fronts it (nginx reverse-proxies to it over the container network), so the
exact port number has no external meaning — it only has to avoid whatever
else is already bound on that host.

**Trap, fourth order (fixed):** the TLS-free vendor gateway loops too. On IBM
9.9.1 the base dependency computation appends `certificate_source:
cephadm-signed` for a spec carrying no certificate fields (ssl defaults true
on MgmtGatewaySpec), the recording side still drops it, and all six gateways
reconfigure every ~30s forever — observed as `diff {'certificate_source:
cephadm-signed'}` spam, gateways cycling through `starting` (the service polls
0/6..6/6 unstably), and downstream churn such as ceph-exporter redeploy loops
whose deps list the gateway daemons. No ssl-enabled gateway shape converges on
that build, so bootwright pins `ssl: false` in the vendor gateway spec (a
plain-HTTP gateway on the authored port); oss keeps cephadm-signed HTTPS,
which converges upstream (ADR 0047).

**Trap, fifth order (fixed):** `ssl: false` does not survive the spec store.
`ServiceSpec.to_json` drops every falsy field (`if val:` on the spec dict,
`if self.ssl:` on the TLS block) and the store persists specs through it, so
`ceph orch apply` of the pinned document leaves the in-memory spec correct
(loop stops, cephadm-signed certs get removed on the next reconfigure) while
`mgr/cephadm/spec.mgmt-gateway` stores an inner spec with no `ssl` key at all
— field-observed as `{"port": 8443, "virtual_ip": ...}` and a vanished
`enable_auth: false`. Any mgr failover/restart reloads the stored copy and
resurrects `ssl: true`, restarting the loop with nothing having changed. Two
corollaries: `ceph orch ls --export` round-trips the same serializer and
prints the resurrected default (`ssl: true`) for a gateway that provably runs
ssl-off — read the config-key, not the export — and the management phase now
repairs the stored copy (`ceph config-key set` with `ssl: false` injected)
and asserts the read-back, failing the apply closed if the switch is still
missing. The repair must wait for the store to settle: `orch apply` persists
with `needs_configuration: true`, and the serve pass that configures the
service ends with `mark_configured`, which re-serializes the in-memory spec
through the same falsy-dropping serializer — a repair written before that
rewrite is silently clobbered minutes after the apply goes green, so the
read polls until the envelope reports `needs_configuration: false` and the
assert refuses an unsettled or unreadable store. Related but distinct: a spec change alone never rewrites a *running*
daemon's config (only a dependency mismatch does), so gateways deployed
during a loop keep serving stale HTTPS until `ceph orch reconfig
mgmt-gateway` — and the dashboard's Prometheus/Alertmanager calls go through
the gateway's always-on internal mTLS server on 29443, a port cephadm never
registers with firewalld (`get_port_start` returns only `spec.port`); the
dependencies phase opens 29443/tcp on storage nodes whenever a gateway is
declared, or every monitoring panel dies with no-route-to-host while the
dashboard itself works.

**Constraint (revised):** cephadm ingress inline TLS is the `ssl_cert` +
`ssl_key` PAIR with `ssl: true` — never one combined PEM bundle. The certmgr
lineage (IBM 9.x, upstream main) routes ingress certificates through the
generic certificate machinery whose inline source is defined as the pair; its
spec normalization refuses a cert without a key, and the 9.9.1 build in the
field accepted a lone bundled `ssl_cert` but rendered a haproxy.cfg whose
`ssl crt /var/lib/haproxy/haproxy.pem` referenced a file it never wrote —
every haproxy daemon crash-looped with `unable to stat SSL certificate from
file '/var/lib/haproxy/haproxy.pem'` (ingress.rgw stuck at N/2N with all
keepaliveds running). Upstream tentacle writes the pair as `haproxy.pem` plus
`haproxy.pem.key`, which haproxy loads as certificate and companion key, so
the pair deploys on BOTH lineages; `rgw_ingress_tls.yml` emits it. Ingress
does not inherit the mgmt-gateway reconfigure loop: IngressService's
generate_config passes the spec when recording deps, so recorded and computed
lists agree. The field NAMES are still the short pair `ssl_cert`/`ssl_key`
([ceph-cephadm-bootstrap-contract.md](ceph-cephadm-bootstrap-contract.md)).
`StorageObjectGatewayIngress.tls` (`certificateRef`+`keyRef`, both a
`tlsCertificate` Secret) maps one ref to each field, and the Go-rendered
`late-services.yaml` (shared with the native `apply.sh` path) never carries the
cert bytes — it still emits the plain cert-less ingress doc unconditionally
(see [ceph-service-rollout-gate.md](ceph-service-rollout-gate.md) for why
secret-bearing specs are always a separate ansible-side reapply rather than
baked into a rendered/state-tracked asset file); `rgw_ingress_tls.yml` reapplies
the complete spec immediately afterward for every ingress that declares
`tls`, which cephadm's declarative `ceph orch apply` treats as a normal spec
update — no explicit restart/wait is needed the way the mgr dashboard flip
needed one, since `ceph orch apply` isn't a live-config toggle here.

**Root cause:** the first RGW ingress TLS implementation validated that
`certificateRef` and `keyRef` named `tlsCertificate` objects but omitted their
material roles from storage preflight. A missing context/generated certificate
therefore survived into the base phase, where the controller-side Ansible file
lookup failed against the task's short-lived `artifacts/runtime/secrets` path
after earlier Ceph mutations had already run. Storage preflight must require the
certificate ref's primary material and the key ref's `tls-key` material for the
owning storage cluster in the base phase; generated-material checks must resolve
the requested role so a missing key reports `<name>.key`, not the certificate
path.

**Root cause, vendor-build gateway TLS (ADR 0047):** on IBM Storage Ceph 9.9.1
(`20.2.1-324.el9cp`) a `mgmt-gateway` spec carrying `ssl_cert`/`ssl_key` makes
cephadm reconfigure every gateway daemon **each serve pass, forever**. The
vendor build backports upstream `main`'s certificate dependencies: the
scheduler computes daemon deps *with* the spec (appending `certificate_source:
inline` plus `ssl_cert`/`ssl_key` md5 hashes), but
`MgmtGatewayService.generate_config` — whose return value is what gets
*recorded* as the daemon's deps — calls `get_dependencies(self.mgr)` with no
spec, so the recorded list can never contain a certificate entry and
`last_deps != deps` holds on every pass. Signature: `ceph log last cephadm`
dominated by `Reconfiguring mgmt-gateway.<node> deps [...] (diff {'ssl_cert:
<md5>', 'ssl_key: <md5>', 'certificate_source: inline'})`, gateways stuck
`starting` with `pending_daemon_config: true`, apply failing closed at service
readiness with `mgmt-gateway (N/M)`. Routing the certificate through the
cephadm cert store (`certificate_source: reference`) loops identically — the
`certificate_source:` entry alone breaks the match — and a TLS-free spec
serves the cephadm-managed certificate, not a store-held user one, so **no
spec shape carries a user certificate convergently on such a build**.
Validation therefore refuses `spec.ceph.mgmtGateway.tls` on
subscription-backed distributions
(`validateStorageCephMgmtGatewayTLSDistribution`); upstream community
`v20.2.x` computes no certificate dependencies and keeps the block. A cluster
already wedged by an inline-TLS spec self-heals on the next apply: the
late-services TLS-free document makes computed and recorded deps agree again
and the daemons settle.
