# Ceph monitoring stack and management gateway

Facts the monitoring unit and the management gateway unit rely on, read from
upstream Ceph v20.2.1 sources and from the IBM 20.2.1-405 build research.
Required behavior lives in
[storage](../../specs/ceph.md#resources-readiness-and-access) and in the
[native records](../../specs/ceph-native-api.md#observability-and-management-gateway-records).
Another build may differ. A difference is absorbed by the general flow or
becomes a declared seam
([version specializations](version-specializations.md#adding-a-specialization)),
never an inline version check. Image references live in
[auxiliary images](ibm-ceph-auxiliary-images.md), which is research and not a
default.

## Gateway certificate store

The gateway's certificate source is
[`get_external_certificates`](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/services/mgmt_gateway.py#L54-L78).
It reads the stored `mgmt_gw_cert` and `mgmt_gw_key` first. With none stored it
takes the spec's `ssl_cert` and `ssl_key`, saved as user made. With neither it
generates a pair. The generated certificate's IP SAN is `virtual_ip` verbatim
(the host address without one,
[`get_mgmt_gw_ip`](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/services/mgmt_gateway.py#L43-L47)),
and with a `virtual_ip` it carries no host FQDN
([:67-71](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/services/mgmt_gateway.py#L67-L71)).
Whichever pair it obtained is saved
([:73-75](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/services/mgmt_gateway.py#L73-L75)).
[`post_remove`](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/services/mgmt_gateway.py#L168-L177)
deletes the stored pair and resets the dashboard standby options.

The consequence: a generated pair stored before a bound one is kept until the
gateway is removed. Bootwright's answer:

- the binding travels in the first apply
  (`test_gateway_document_applied_with_binding_first`);
- the served leaf's SHA-256 is the postcondition
  (`test_gateway_proof_compares_bound_leaf`);
- without a binding, the native certificate-manager CA authenticates the
  endpoint before its leaf digest is reported
  (`test_gateway_without_binding_reports_cephadm_issuer`);
- the bound leaf must name the virtual IP, or every placement host, before a
  fresh apply (`TestGatewayCertificateMustCoverVirtualIP`,
  `TestGatewayCertificateMustCoverHostFQDNs`).

`virtual_ip` is a bare address because the generated certificate's SAN copies
it verbatim. There is one gateway per cluster because the store holds one pair
under one name.

oauth2-proxy follows the same precedence under `oauth2_proxy_cert`
([oauth2_proxy.py:61-82](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/services/oauth2_proxy.py#L61-L82)).

The IBM build's gateway spec also has `certificate_source` and `custom_sans`.
The [auxiliary images research](ibm-ceph-auxiliary-images.md#service-specification-fields)
lists them, and the Decided general row in
[version specializations](version-specializations.md#decided-general) decides
how a build's inferred source is treated; neither is repeated here.

## Dashboard behind the gateway

Deploying the gateway runs
[`config_dashboard`](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/services/mgmt_gateway.py#L49-L52),
which sets the dashboard standby status code to 503 and the standby behaviour
to `error`. Removing the gateway restores 500 and `redirect`. Bootwright writes
no dashboard option (`test_gateway_unit_never_writes_a_dashboard_setting`). The
internal listener is 29443.

`config_dashboard` does not run at the deploy. It runs from
`daemon_check_post` at the end of a later `_check_daemons` pass, for daemons
whose `pending_daemon_config` the deploy set, and only while the `dashboard`
module is enabled. Until then `ceph config get mgr` answers the module
defaults, `redirect` and 500, and a standby dashboard sends the browser to the
active manager's address in `mgr services` (the gateway's nginx skips only a
standby that answers 503). On the 2026-11-24 ceph-stretch run (`ceph-prd-01`)
the pass waited behind a two-minute reconfiguration sweep that began right
after the gateway deployed, the apply reported the gateway ready, and the
owner's browser at the gateway's address landed on `https://10.7.7.2:8443/`.
Gateway readiness therefore reads both options and is not ready until they
read `error` and 503 (blocking entry `standby@mgmt-gateway`); it still writes
nothing, because the wait outlasts cephadm's delay.

`ceph config get <who> <key>` answers one key as its bare value and a newline
(`error`, `503`) and ignores `--format json`
([`ConfigMonitor.cc`](https://github.com/ceph/ceph/blob/v20.2.1/src/mon/ConfigMonitor.cc#L352-L365));
only the keyless dump honours the format. A JSON parse of that answer works
by accident for `true`, `false` and numbers and fails for any other string:
the first standby read on the 2026-11-25 ceph-stretch run
(`op-284a9069`) refused `native-output@ceph_native.command`. Read a single key
as text.

The dashboard also uses its own certificate and key as the TLS client identity
for Prometheus and Alertmanager (see
[`get_access_info`](https://github.com/ceph/ceph/blob/v20.2.2/src/pybind/mgr/dashboard/controllers/prometheus.py)).
The gateway's
[internal listener](https://github.com/ceph/ceph/blob/v20.2.2/src/pybind/mgr/cephadm/templates/services/mgmt-gateway/internal_server.conf.j2)
requires a client certificate trusted by the cephadm CA. A self-signed dashboard
certificate fails that trust: the dashboard may then try to decode an HTML
error as JSON and report `Error parsing Prometheus Alertmanager response`.
IBM documents
[reconfiguring the dashboard to use cephadm-signed certificates](https://www.ibm.com/docs/en/storage-ceph/9.9.0?topic=gateway-reconfigure-dashboard-use-cephadmsigned-certificates).
The native `orch certmgr generate-certificates dashboard` command returns a
pair even before gateway deployment; its private output must stay out of
failure diagnostics. Bootwright's
[dashboard certificate rule](../../specs/ceph.md#dashboard-and-client-data-test)
owns custody and replay. Custom operator-provided direct dashboard certificates
remain parked ([B682](../../specs/milestones/backlog.md#b682)); this generated
client identity does not add that configuration surface.

## Peers addressed by host name

cephadm writes some peers into a daemon's configuration as
`mgr.get_fqdn(host)`, the FQDN the host reported at its last fact gathering,
or the inventory address before any gathering. A host whose resolver cannot
answer that name starts the daemon with an upstream it cannot resolve. In the
owner's two-context stretch run of 2026-11-03 both management gateway
daemons exited with nginx `host not found in upstream
"ceph-03.e2e.example.test:8443"` and cephadm raised `CEPHADM_FAILED_DAEMON`
([B1086](../../specs/milestones/m2.md#b1086)).

Read from the cephadm manager modules of the IBM builds
`ceph-mgr-cephadm-20.2.1-405.el9cp` and `20.2.2-349.el10cp` (container image
layers on the controller, `usr/share/ceph/mgr/cephadm/services/`):

- Always by name: the management gateway's dashboard upstreams, one per MGR
  daemon (`get_dashboard_endpoints` in `cephadmservice.py`); Alertmanager's
  cluster peers, one per Alertmanager daemon, and its dashboard URLs of the
  standby MGRs when no gateway is deployed (`monitoring.py`); the active
  MGR's scrape target that service discovery hands Prometheus
  (`prometheus_sd_config` in `service_discovery.py`).
- By name when the daemon was placed with no address: Prometheus targets
  that Grafana, Alertmanager and the dashboard read, Grafana's and
  Alertmanager's URLs that the dashboard sets, the Loki host that Promtail
  and Alloy push to, and SNMP gateway targets.
- By inventory address, never by name: the gateway's Prometheus,
  Alertmanager, Grafana and oauth2-proxy upstreams
  (`get_service_endpoints` in `mgmt_gateway.py`).

So storage preflight's `host.peer-names` check
([plan and prerequisites](../../specs/ceph.md#plan-and-prerequisites)) runs
on each host that places `mgmt-gateway`, `alertmanager`, `grafana`,
`prometheus`, `loki`, `alloy` or `promtail`, and on each MGR host of a
cluster that declares one of them, and asks for every node name. It reads
`/usr/bin/getent ahosts` with every name at once, which exits 2 when any is
not found, and reads each name alone only then to name the unresolved ones.
Code: [guests.go](../../internal/storage/preflight/guests.go) and
[service.go](../../internal/storage/preflight/service.go).

## Prometheus manager module

[`PrometheusService.config`](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/services/monitoring.py#L480-L489)
runs `mgr module enable prometheus` when the manager map's services lack
`prometheus`, and the monitor then respawns the active manager. So the module
is the declared service's derived effect, and `spec.mgrModules` refuses it
(see the
[native records](../../specs/ceph-native-api.md#observability-and-management-gateway-records)).

The unit never enables the module. It reads readiness from `ceph mgr services`
inside one served-manager block, so a respawn reads as unfinished work
(`test_prometheus_module_observed_through_mgr_services`,
`test_unit_waits_for_available_mgr_after_module_respawn`). The IBM
20.2.1-405 tree carries the same call (`services/monitoring.py`,
`PrometheusService.config`); if a build lacks it, readiness fails closed and
never reads ready. The remedy is new evidence, not an enable step.

### The exporter port and firewalld

A MGR's deployment ports are every port in `mgr services` at the moment
cephadm deploys, redeploys or reconfigures that MGR, plus service discovery's
8765 (`MgrService.prepare_create` in `services/cephadmservice.py`, IBM
20.2.1-405 tree). The cephadm binary opens exactly a daemon's deployment ports
in firewalld on every fresh deploy, redeploy and reconfigure
(`handle_deployment_firewall_updates`, called from
`handle_post_writing_unit_files_deployment_actions` in the 20.2.1-405-el9
binary). A reconfigure also writes the new ports to the daemon's `unit.meta`
and restarts no Ceph daemon (`restart_reconfigured_daemon`). Enabling the
prometheus module only respawns the MGRs, so their ports stay as they were.

The cluster's dashboard phase redeploys every MGR once the dashboard
publishes, before any monitoring unit runs, so every MGR gets ports 8443 and
8765 only; bootstrap opened 9283 on the first host alone. Service discovery
hands every Prometheus one target, the active MGR's FQDN at `:9283`
(`prometheus_sd_config` in `services/service_discovery.py`), so only the
Prometheus colocated with the active MGR reaches it. On the owner's
`ceph-prd-01` (IBM 9.9.1.2, 2026-11-24) `ceph orch ps --daemon-type mgr`
listed `*:8443,8765` on all four MGRs while `mgr services` published
`https://10.7.7.2:9283/`. Three of the four Prometheus daemons saw the target
down, so `CephMgrPrometheusModuleInactive` (`up{job="ceph"} == 0`) fired and
cleared as the gateway balanced alert reads across them
([B1803](../../specs/milestones/m2.md#b1803)).

The remedy is `ceph orch daemon reconfig mgr.<id>`. cephadm's `_daemon_action`
runs `prepare_create` for a reconfigure, so the ports are recomputed from the
current `mgr services`, and it refuses only a redeploy or restart of the
active MGR (`module.py`). The request is scheduled durably per daemon
(`schedule_daemon_action` in `inventory.py`, saved with the host) and a repeat
coalesces. A reconfigured Ceph daemon is not re-added to cephadm's daemon
cache (`_create_daemon` in `serve.py`), so `orch ps` lists the new ports only
after a host refresh, which `--refresh` requests and a later read shows. The
monitoring unit's readiness names `ports@mgr` until then, and its apply makes
the request after an intent record per MGR container, published ports and
active MGR instance. An apply asks each lagging MGR at most once: its first
evaluated call may ask a recorded one again, later calls only unrecorded ones.
So a lost reply repeats nothing within the apply, and the next apply asks a
lost request once more, the policy of the dashboard port repair
(`test_lagging_manager_ports_block_readiness_until_reconfigured_and_refreshed`,
`test_lost_reconfigure_reply_is_never_repeated_within_its_apply`,
`test_a_lost_request_is_asked_again_once_per_apply`).

The exporter on its default port without a Prometheus service is parked
([B684](../../specs/milestones/backlog.md#b684)).

## Monitoring basic-auth logins

Observation: once a `mgmt-gateway` daemon runs, cephadm's monitoring security
is on (`_get_security_config` counts the gateway's daemons), and Prometheus and
Alertmanager require basic authentication. `ceph orch prometheus
get-credentials` and `ceph orch alertmanager get-credentials` are write
commands (`_cli_write_command`) that print `json.dumps` of
`{"user", "password", "certificate"}`, the last being cephadm's root CA in
PEM, and `{}` while security is off; they take no `--format`. Asked about a
service whose login it does not hold, cephadm stores and returns
`admin`/`admin` (`_get_prometheus_credentials`,
`_get_alertmanager_credentials`), so asking is a write. Upstream v20.2.1:
[orchestrator/module.py](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/orchestrator/module.py)
(`_get_prometheus_access_info`, `_get_alertmanager_access_info`) and
[cephadm/module.py](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/module.py)
(`get_prometheus_access_info`, `get_alertmanager_access_info`,
`_get_security_config`). The IBM Storage Ceph build has not been checked live;
the owner's live check of `cluster info --secrets` is owed under
[B1629](../../specs/milestones/m2.md#b1629).

Decision: the management gateway unit asks only for a listed `prometheus` or
`alertmanager`, after readiness, requires `user` and `password`, discards
`certificate`, refuses `{}` as `material`, and keeps the logins as its produced
output `monitoring`
([management gateway unit](../../specs/ceph.md#resources-readiness-and-access)).
A recorded observation asks only once a deployed daemon proves the login is
stored: `PrometheusService.generate_config` reads both logins and
`GrafanaService.get_dependencies` reads Prometheus's, each storing the default
when absent
([services/monitoring.py](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/services/monitoring.py)).
The code is `monitoring_credentials` and `monitoring_credentials_held` in
[ceph_native_units.py](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_native_units.py)
and `native_login` in
[ceph_native_observability.py](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_native_observability.py),
held by
[test_ceph_monitoring_credentials.py](../../ansible/collections/ansible_collections/bootwright/core/tests/unit/plugins/module_utils/test_ceph_monitoring_credentials.py).

Setting a login: `ceph orch prometheus set-credentials -i -` and `ceph orch
alertmanager set-credentials -i -` read the JSON `{"username", "password"}`
from the input buffer only, so the value never reaches an argument
([orchestrator/module.py](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/orchestrator/module.py),
`_get_credentials`). `set_prometheus_access_info` and
`set_alertmanager_access_info` only store the user and password
([cephadm/module.py](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/module.py)).
Each daemon's dependencies hash its login
(`PrometheusService.get_dependencies`, `AlertmanagerService.get_dependencies`),
so cephadm's own serve loop reconfigures the daemon and re-renders `web.yml`
within one serve interval; Grafana's dependencies hash Prometheus's login, so a
Prometheus update also reconfigures Grafana and re-sends the spec's value to the
dashboard ([Grafana admin password](#grafana-admin-password)). `orch ps`
reports each daemon's `last_configured`, which can lag the real
reconfiguration by one host refresh.

Behind a running gateway Prometheus and Alertmanager always require a client
certificate the cephadm root CA verifies (`enable_mtls` follows the gateway;
`web.yml.j2` sets `RequireAndVerifyClientCert`), and basic authentication only
while no oauth2-proxy runs (`enable_basic_auth: not oauth2_enabled`,
[services/monitoring.py](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/services/monitoring.py)).
With an oauth2-proxy the login therefore guards nothing at the daemon. They serve
under `/prometheus/` and `/alertmanager`
([cephadmlib/daemons/monitoring.py](https://github.com/ceph/ceph/blob/v20.2.1/src/cephadm/cephadmlib/daemons/monitoring.py)),
with a server certificate naming the host's inventory address.
`ceph orch certmgr generate-certificates prometheus` returns a cephadm-signed
client pair and stores nothing. The dashboard reads both logins from cephadm per
request, cached for one minute
([dashboard/controllers/prometheus.py](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/dashboard/controllers/prometheus.py),
`get_access_info`), so a login update needs no dashboard write.

Decision (updates): the native update replaces the login only when cephadm's
stored one is the custodied login or the retained candidate, and proves it by
the daemons' reconfiguration plus a `200` for the new login and a `401` for the
old one, skipping the `401` under an oauth2-proxy
([administrator credential updates](../../specs/cli/cluster-admin-credentials.md#monitoring-logins)).
The code is
[ceph_monitoring_logins_native.py](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_monitoring_logins_native.py).

Decision (first install): asking for a missing login stores `admin`/`admin`,
and a gateway daemon that then runs requires it, so the unit's first apply sets
a generated login for each listed service before the gateway deploys
(`gateway_claim` in
[ceph_native_observability.py](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_native_observability.py)),
while its attempt record is absent. Cephadm keeps that login until the existing
post-readiness read captures it, and `monitoring_credentials_held` is
unchanged. A cluster an older build realized may hold the default until
`cluster update-admin-creds --login prometheus,alertmanager` replaces it
([storage](../../specs/ceph.md#resources-readiness-and-access)).

## Grafana admin password

Observation: cephadm renders the spec's `initial_admin_password` into
`grafana.ini` as `admin_password`, which Grafana reads only when it creates its
database, so changing the Secret or the spec changes no running Grafana
([grafana.ini.j2](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/templates/services/grafana/grafana.ini.j2)).
Every Grafana configuration generation, including one a Prometheus login change
causes, runs `dashboard set-grafana-api-password` with the spec's value when the
dashboard module is enabled
(`GrafanaService.generate_config` in
[services/monitoring.py](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/services/monitoring.py)),
so a changed live password alone is reverted in the dashboard on the next
reconfiguration. Each Grafana daemon keeps its own database. Grafana's HTTP API
changes a password with `PUT /api/user/password` and the body `oldPassword`,
`newPassword`, `confirmNew`, under basic authentication
([Grafana user API](https://grafana.com/docs/grafana/latest/developers/http_api/user/#change-password)).
Behind a management gateway Grafana serves from the `/grafana` sub path
(`serve_from_sub_path`); `[auth] disable_login_form` for an oauth2-proxy turns off
only the login form, so basic authentication still works. The dashboard has the
generated getter `dashboard get-grafana-api-password`
([dashboard/settings.py](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/dashboard/settings.py),
`GRAFANA_API_PASSWORD`).

Decision: a Grafana update changes three things: every running daemon's live
password through the API, cephadm's stored spec leaf (the unit's frozen Grafana
document re-applied with the new value, which is at most a reconfiguration), and
the dashboard's Grafana API password, read back and compared in memory. The
value also becomes a new version of the Secret the unit binds, held by a rebind
of the apply's binding
([Grafana administrator password](../../specs/cli/cluster-admin-credentials.md#grafana-administrator-password)).
The IBM Storage Ceph build has not been checked live; the owner's acceptance of
the update is owed under [B1632](../../specs/milestones/m2.md#b1632).

## Alertmanager webhooks

[`generate_config`](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/services/monitoring.py#L312-L324)
reads only `user_data.default_webhook_urls`
(`test_alertmanager_document_carries_default_webhook_urls`). The
[native records](../../specs/ceph-native-api.md#observability-and-management-gateway-records)
state the refusal of other keys.

## oauth2-proxy and dashboard roles

The template fact lives in
[auxiliary images](ibm-ceph-auxiliary-images.md#oauth2-proxy-template).

The proxy appends the gateway's hosts and addresses to `allowlist_domains`
([:87-88](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/services/oauth2_proxy.py#L87-L88)),
and its default redirect is the gateway endpoint plus `/oauth2/callback`
([:51-53](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/services/oauth2_proxy.py#L51-L53)).
Login high availability is managed externally, as
[the upstream guide](https://github.com/ceph/ceph/blob/v20.2.1/doc/cephadm/services/oauth2-proxy.rst)
says.

That guide's example spells `ssl_certificate` and `ssl_certificate_key`, which
are not spec fields: the spec has `ssl_cert` and `ssl_key`, and in Bootwright
the material arrives through bindings.

An identity provider's roles map only onto system roles
([`map_to_system_roles`](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/dashboard/services/access_control.py)),
so a custom role needs local users, and `ac-user-create` reads the password
from `-i`. Local roles and users are parked
([B683](../../specs/milestones/backlog.md#b683)). SSO itself is realized
through the management gateway unit (`TestOAuth2ProxyNoLongerRefusesR16`).

cephadm never enables dashboard SSO:
[`config_dashboard`](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/services/mgmt_gateway.py)
sets only the standby options. The proxy's
[`post_remove`](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/services/oauth2_proxy.py)
disables it through the dashboard's `disable_oauth2_sso` when the last
running oauth2-proxy daemon goes. In
[sso.py](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/dashboard/services/sso.py)
`dashboard sso enable oauth2` answers `EPERM` until the orchestrator's cached
daemon list holds a running oauth2-proxy, then sets the module option
`sso_oauth2` (read as `mgr/dashboard/sso_oauth2`), while `dashboard sso
status` and `disable` answer `EPERM` without `python3-saml`. So the unit waits
for `ceph orch ps --daemon_type oauth2-proxy` to list one running, enables SSO
itself, and observes the option, never `sso status`.

cephadm's `OAuth2ProxySpec` validation admits a `cookie_secret` of 16, 24 or
32 bytes only, counted after URL-safe base64 decoding when it decodes, so
every placeholder the unit normalizes in a secret's place must pass it
(`test_every_bound_placeholder_passes_the_proxy_spec_validation`).

## Certificate manager

The upstream [certmgr guide](https://github.com/ceph/ceph/blob/v20.2.1/doc/cephadm/certmgr.rst)
says certmgr does not renew user-provided certificates automatically, so
renewal is out of the day-0 scope. IBM documents native root CA retrieval as
`ceph orch certmgr cert get cephadm_root_ca_cert` in its
[HTTPS procedure](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=https-configuring-rgw-multi-site-using-cephadm).
The unbound gateway proof uses that CA for chain, validity and endpoint identity
verification, rather than accepting the observed leaf as its own trust anchor.
Root CA export to operators and the certificate-source fields remain parked
([B685](../../specs/milestones/backlog.md#b685)).

## The retired workaround list

The closed token `mgmt-gateway-spec-dependency-recording` entered in
2ecd06567. The storage API spec row that named it left the live spec for the
deferred designs in 9cdda0f81, and no commit or spec recorded a defect it
worked around. The management gateway unit was realized with no specialization,
seam or version check, so the list stays retired. A gateway defect found live
becomes a declared seam with evidence
([adding a specialization](version-specializations.md#adding-a-specialization)),
never a workaround field.

The code is
[ceph_native_observability.py](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_native_observability.py)
and
[ceph_native_units.py](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_native_units.py).
