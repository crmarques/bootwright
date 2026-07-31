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

**Constraint:** cephadm's ingress spec (RGW/NFS HAProxy+keepalived) takes TLS
material through a single `ssl_cert` field — one PEM blob containing both the
certificate chain and the private key concatenated, with source `inline`.
This is a different shape from the management gateway and oauth2-proxy, which
take two separate fields — but the field *names* are the same short pair,
`ssl_cert`/`ssl_key`, not the `ssl_certificate`/`ssl_certificate_key` the
upstream oauth2-proxy example still prints
([ceph-cephadm-bootstrap-contract.md](ceph-cephadm-bootstrap-contract.md)).
Match the count, not the name: one bundled field for ingress, two for the
gateway.
`StorageObjectGatewayIngress.tls` (`certificateRef`+`keyRef`, both a
`tlsCertificate` Secret) mirrors the two-ref user-facing shape everywhere
else in the schema for consistency, and `rgw_ingress_tls.yml` concatenates
the two files' content (`cert + "\n" + key + "\n"`) into `ssl_cert` at apply
time. Like `management_services.yml`, the Go-rendered `late-services.yaml`
(shared with the native `apply.sh` path) never carries the cert bytes — it
still emits the plain cert-less ingress doc unconditionally (see
[ceph-service-rollout-gate.md](ceph-service-rollout-gate.md) for why
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
