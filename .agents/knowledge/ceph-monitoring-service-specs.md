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

**Constraint:** The sidecar-image advisory is a read-only, feature-derived
preflight for IBM and disconnected clusters. Monitoring requires
`container_image_{prometheus,grafana,alertmanager,node_exporter}` only while it
is enabled. An RGW or NFS ingress requires `container_image_{haproxy,keepalived}`.
A declared management gateway requires `container_image_{nginx,keepalived}` and
also `container_image_oauth2_proxy` when its OAuth2 proxy is declared. It reports
every missing `mgr/cephadm/container_image_*` option, including an empty one, in
one finding and its remedy; an unrelated pin cannot silence another required
sidecar. The management gateway's `keepalive_only` ingress is deliberately not
an HAProxy requirement.

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
that build, so the gateway spec carries `ssl: false` whenever
`spec.ceph.mgmtGateway.exposure: http` is declared (a plain-HTTP gateway on
the authored port) — and on subscription-backed distributions the exposure
field is REQUIRED explicitly, so the cleartext choice is the operator's, not
a render side effect (ADR 0047, ADR 0049). oss defaults to cephadm-signed
HTTPS, which converges upstream; an explicit `exposure: https` on a vendor
build is accepted as the forward declaration for a build that repairs the
recording. `oauth2Proxy` is refused on vendor builds outright — upstream
names oauth2-proxy as sharing the same spec-less dependency recording.

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

**Constraint:** deploying a mgmt-gateway arms monitoring-stack security by
itself. cephadm's `_get_security_config` computes `security_enabled =
secure_monitoring_stack OR mgmt_gw_enabled`, and the prometheus/alertmanager
`web.yml` renders `basic_auth_users` whenever security is on and oauth2-proxy
is absent — so the daemons demand HTTP Basic (default `admin`/`admin`;
`ceph orch prometheus|alertmanager get-credentials` / `set-credentials`) even
though bootwright never touches `secure_monitoring_stack`. The gateway's
`/prometheus` and `/alertmanager` locations proxy the browser through with
only the internal mTLS client cert — no Authorization header — so the 401
challenge surfaces browser-side as an origin-wide Basic popup; the dashboard
(`/`, JWT) and `/grafana` (Authorization stripped by the template) never
challenge, and the dashboard's own monitoring reads go mgr-side through
`https://<vip>:29443/internal/...` with the credentials attached, so panels
keep working while the popup fires.

**Constraint:** alert `generatorURL`s never route through the gateway.
`--web.external-url` is computed by the cephadm *binary* on the prometheus
node — scheme hardcoded `http`, host `socket.getfqdn()` (the machine's
resolver identity, not the inventory addr, not the VIP), port the daemon's
(9095) — plus `--web.route-prefix=/prometheus/` whenever any mgmt-gateway
exists, yielding `http://<machine-fqdn>:9095/prometheus`. Prometheus stamps it
into every alert, and the dashboard renders it verbatim as the Source link.
No override channel exists: `PrometheusSpec` has no URL field (v20.2.1 and
main), `extra_entrypoint_args` append AFTER the computed args so the flag
repeats and prometheus refuses to start, and `set-prometheus-api-host` (the
mgr-side proxy target) is re-asserted by `config_dashboard`. The working
access path is the same path+query on the gateway origin, with the monitoring
credentials.

**Trap:** the gateway's dashboard upstream lists EVERY mgr host
(`get_dashboard_endpoints` enumerates all mgr daemons); mgmt-gateway flips the
dashboard module to `standby_behaviour=error` (503) and relies on
`proxy_next_upstream error timeout invalid_header http_500 http_502 http_503
http_504` to walk to the active one. With nginx defaults (`max_fails=1`,
`fail_timeout=10s`), ONE failed exchange with the active dashboard — restart,
timeout, or a genuine 500 (it is in the next-upstream list) — marks it down
for 10s, and since standbys are perpetually "failing", the origin answers
502 "no live upstreams" instantly until the window passes: intermittent 502
on polled `/api/*` endpoints with everything else fine. Recorded gateway deps
name mgr daemons WITHOUT ports, so mgr failover and dashboard port moves never
trigger a reconfig — daemons deployed mid-loop keep stale backend maps (e.g.
the dashboard's old HTTPS 8443, now the gateway's own port) behind the
floating VIP until `ceph orch reconfig mgmt-gateway`.
`MgmtGatewayService.get_dependencies` is the proof: prometheus, alertmanager,
grafana and oauth2-proxy contribute `name:port`, mgr contributes `name` alone,
and nothing contributes the dashboard's port or scheme. The management phase
flips `mgr/dashboard/ssl` off and moves the dashboard to 8081 on every apply,
which changes neither — so on a cluster whose gateways predate that flip the
apply goes green over six gateways serving 502.
`management_gateway_health.yml` closes it after service readiness: it GETs
each gateway's own port, treats 502/503/504 as the stale-config signature,
runs ONE `ceph orch reconfig mgmt-gateway` when any host shows it, re-probes
with retries and fails closed if the fault survives (which means the dashboard
itself is down, not the nginx in front of it). The probe covers gateways on
`exposure: http` only — an https gateway serves a cephadm-signed certificate,
and probing it would mean disabling verification, which
`TestValidateCertsFalseIsAllowlisted` refuses; it needs the cluster's cephadm
root CA as a trust anchor instead, which nothing publishes yet.

**Constraint:** the same `security_enabled` that arms the Basic popup also
arms TLS on the ceph-mgr *prometheus module* (`:9283`). The module asks the
orchestrator for the security config, and when it is on calls
`orch certmgr generate-certificates --module_name prometheus`, serves cherrypy
over TLS and publishes `https://<addr>:9283/` through `set_uri` — so
`ceph mgr services` reports the scheme. There is no basic auth there, only the
certificate, and it is signed by the per-cluster cephadm root CA. Rook's
external-cluster exporter reads that URI, re-dials it through `endpoint_dial`
with no CA argument (`requests.head(ep)` against the system trust store) and
exits 1 with a bare `CERTIFICATE_VERIFY_FAILED`, killing the Data Foundation
attach step.

**Omitting the endpoint is NOT a legal answer** — this was tried first
(`--skip-monitoring-endpoint`) and is wrong: ocs-operator treats
`monitoring-endpoint` as a REQUIRED external resource and parks the
StorageCluster in `Phase: Error` with `Unable to retrieve
"monitoring-endpoint" external resource`, so every data path stays down over a
metrics detail. `export-external-details.yaml` therefore probes
`ceph mgr services` and, on an `https` prometheus URI, retrieves the cluster's
own cephadm root CA (`cert_mgr` stores it as `cephadm_root_ca_cert`, and
`cert_ls` always includes it "for trust chain purposes"), concatenates it onto
the node trust store into a bundle inside the exporter working directory, and
runs the exporter as `env REQUESTS_CA_BUNDLE=<bundle> python3 …` with that one
directory `--mount`ed into the `cephadm shell` container. Three retrieval paths
are tried in order — `ceph orch certmgr cert get cephadm_root_ca_cert`,
`ceph orch sd dump cert`, the `mgr/cephadm/cert_store.cert.cephadm_root_ca_cert`
config-key — and the step fails closed naming all three when none returns a PEM,
because the alternative is the opaque CERTIFICATE_VERIFY_FAILED. The whole
working directory is mounted (rather than the script file alone) so one
`--mount` value covers script and bundle; a pre-placed
`bootwright_exporter_script` is copied into it for the same reason.

What the emitted endpoint does NOT buy: rook's `MonitoringSpec` carries
`externalMgrEndpoints` and `ExternalMgrPrometheusPort` and NO scheme, CA or TLS
field, so the ServiceMonitor built from it scrapes plain HTTP against the TLS
listener and that target stays down. The StorageCluster reconciles and the data
paths work; only the Ceph metrics panels stay empty. Declaring a mgmt-gateway is
what costs those panels; dropping it restores them.

**Constraint:** the gateway's default port follows `exposure` — 8443 for
`https`, 8888 for `http` (ADR 0052) — and a port contradicting the scheme
(`http` on 443/8443, `https` on 80/8080) is refused. 8888 rather than 8080
because RGW's beast frontend and the classic dashboard both default to 8080
and the gateway lands on ingress hosts. `CephMgmtGatewayDefaultPort` is gone;
`CephDashboardDefaultTLSPort` now names the classic dashboard's own listener,
which is what the gateway-less access summary was always reading.

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
