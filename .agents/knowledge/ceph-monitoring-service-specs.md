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
"active" to an already-running standby — it never restarts any mgr daemon
process, so none of the 4 (or however many) mgr containers ever re-read the
new `mgr/dashboard/ssl=false` config or release the port; the failure
reproduces identically after `mgr fail`. The daemons must actually be
recreated: `ceph orch restart mgr`, which cephadm executes as an async
rolling restart (the command returns immediately having only scheduled it).
`management_services.yml` snapshots every mgr daemon's `container_id` before
issuing the restart, then polls `ceph orch ps --daemon-type mgr` until none
of the current container IDs appear in that snapshot (i.e. every daemon has
actually been recreated) before applying the mgmt-gateway spec.

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
