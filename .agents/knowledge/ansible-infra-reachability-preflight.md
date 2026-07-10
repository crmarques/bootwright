# Infra-apply reachability preflight: what is probed, skipped, and why

Scope and rationale of `bootwright.core.check_external_reachability`, the
controller-side validation that runs first in the infra apply workflow (and
`check infra`) so an unreachable operator-owned dependency aborts the apply
before any on-host convergence touches state. It runs against
`bootwright_ocp_hosts`, so a workspace with no on-host provider/infra work
(every service external) still gets meaningful validation during
`apply --stage infra`.

**Probed: only what Bootwright itself drives.** External Redfish BMCs
(Bootwright issues virtual-media inserts and power cycles against them — the
authenticated `/redfish/v1/Systems` probe is detailed in
baremetal-first-install-safety.md) and external HTTP proxies (Bootwright
reaches the public registry / mirror through them).

**Proxy probe is TCP-only, https first.** `tasks/proxy.yml` picks the https
proxy URL first (matching the order `machine_proxy/facts.yml` uses for
`bootwright_proxy_primary`), falls back to http, and verifies TCP
reachability only (`wait_for` on the proxy host:port, default port 3128,
timeout `bootwright_external_validate_proxy_timeout`). An authenticated GET
would also exercise outbound internet from the controller — not required by
every workflow — and would mask the actual "proxy is up" signal.

**Not probed by design: operator-owned DNS and external LB VIPs.** In real
estates the operator's DNS/LB often live on networks the provider host cannot
reach (different VLAN, firewall, jump host), so a fail-closed probe from this
vantage point produces false negatives. Controller-visible DNS is validated
by `install_agent` immediately before boot instead, because
`openshift-install` polls the API from the controller (see
external-dns-bootstrap.md). External-endpoint resolution gets a non-fatal
diagnostic from `diagnostic_cluster_endpoint_dns` (see
container-cluster-ansible-flow.md).

**Skipped by design: components this same apply will create.**

- `bmcRole == 'emulated'` — sushy-tools is stood up by `apply --stage infra`
  itself; it is not reachable yet.
- managed proxy components — Bootwright stands up Squid during the same
  apply; the proxy probe is skipped when any cluster declares a managed
  proxy.

**Fail-closed.** Every probe registers its result and the role fails the play
if any single check fails; the operator fixes the underlying issue and
re-runs. Tunables (timeouts) live in the role's `defaults/main.yml` so a
field run can override them without editing the role.
