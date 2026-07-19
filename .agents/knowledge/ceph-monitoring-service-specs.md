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
