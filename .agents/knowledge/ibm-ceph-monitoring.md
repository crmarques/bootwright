# Ceph monitoring stack and management gateway

Facts the monitoring unit and the management gateway unit rely on, read from
upstream Ceph v20.2.1 sources and from the IBM 20.2.1-405 build research.
Required behavior lives in
[storage](../../specs/storage.md#resources-readiness-and-access) and in the
[native records](../../specs/storage-native-api.md#observability-and-management-gateway-records).
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
- without a binding, the cephadm-signed leaf's digest is only reported
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

Unverified live: whether the self-signed dashboard certificate that the
bootstrap creates proxies cleanly behind the gateway. IBM's 9.9.1 page
[reconfigure the dashboard to use cephadm-signed certificates](https://www.ibm.com/docs/en/SSEG27_9.9.1/data-security/reconfigure-the-dashboard-to-use-cephadm-signed-certificates.html)
recommends cephadm-signed dashboard certificates with the gateway. A live run
settles it, and the direct dashboard certificate is parked
([B682](../../specs/milestones/backlog.md#b682)).

## Prometheus manager module

[`PrometheusService.config`](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/services/monitoring.py#L480-L489)
runs `mgr module enable prometheus` when the manager map's services lack
`prometheus`, and the monitor then respawns the active manager. So the module
is the declared service's derived effect, and `spec.ceph.mgrModules` refuses it
(see the
[native records](../../specs/storage-native-api.md#observability-and-management-gateway-records)).

The unit never enables the module. It reads readiness from `ceph mgr services`
inside one served-manager block, so a respawn reads as unfinished work
(`test_prometheus_module_observed_through_mgr_services`,
`test_unit_waits_for_available_mgr_after_module_respawn`). The IBM image's copy
of `PrometheusService.config` was not read: if a build lacks the call,
readiness fails closed and never reads ready. The remedy is new evidence, not
an enable step.

The exporter on its default port without a Prometheus service is parked
([B684](../../specs/milestones/backlog.md#b684)).

## Alertmanager webhooks

[`generate_config`](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/services/monitoring.py#L312-L324)
reads only `user_data.default_webhook_urls`
(`test_alertmanager_document_carries_default_webhook_urls`). The
[native records](../../specs/storage-native-api.md#observability-and-management-gateway-records)
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
renewal is out of the day-0 scope. It documents no root CA retrieval, so
Bootwright reports only a cephadm-signed leaf's digest. Root CA reporting and
the certificate-source fields are parked
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
