# IBM Ceph auxiliary images of one release

Research about one release's auxiliary service images, not a catalog. Under
D157 operators select every Ceph artifact and Bootwright keeps no image
default, pin table or per-release profile;
[M2c's question OD28](../../specs/milestones/m2.md#m2c-open-decisions) decides
how auxiliary images are selected. Use these facts to author an example or to
investigate a native failure, never as a default or an admission rule.
[Version constraints](ibm-ceph-version-constraints.md) routes other
version-specific findings.

## Source

On 2026-10-15 the session read the then-pinned image
`cp.icr.io/cp/ibm-ceph/ceph-9-rhel9:v9.9.1-25074` (manifest digest
`sha256:c5b181f15d4ebff833a1c0de84398d2d55ff416d9dd5ed3816a9baa817eb4bde`;
`ceph-common` and `cephadm` `20.2.1-405.el9cp`) read-only, with the container
network disabled (D161). Nothing here was read from the `9.9.2.0` coordinates
the current three-node example authors.

## Default auxiliary images

`ceph.cephadm.images.DefaultImages` inside that image:

| Service | Default reference |
| --- | --- |
| prometheus | `cp.icr.io/cp/ibm-ceph/prometheus:v4.15` |
| alertmanager | `cp.icr.io/cp/ibm-ceph/prometheus-alertmanager:v4.15` |
| node-exporter | `cp.icr.io/cp/ibm-ceph/prometheus-node-exporter:v4.15` |
| grafana | `cp.icr.io/cp/ibm-ceph/grafana-rhel10:v9.9.1` |
| loki | `cp.icr.io/cp/ibm-ceph/logging-loki-rhel8:v3.1.0` |
| promtail | `cp.icr.io/cp/ibm-ceph/promtail-rhel9:v3.0.0` |
| alloy | `cp.icr.io/cp/ibm-ceph/alloy-rhel10:v9.9.1` |
| haproxy | `cp.icr.io/cp/ibm-ceph/haproxy-rhel10:v9.9.1` |
| keepalived | `cp.icr.io/cp/ibm-ceph/keepalived-rhel10:v9.9.1` |
| nvmeof | `cp.icr.io/cp/ibm-ceph/nvmeof-rhel9:1.6` |
| snmp-gateway | `cp.icr.io/cp/ibm-ceph/snmp-notifier-rhel10:v9.9.1` |
| samba | `cp.icr.io/cp/ibm-ceph/samba-server-rhel10:v9.9.1` |
| samba-metrics | `cp.icr.io/cp/ibm-ceph/samba-metrics-rhel10:v9.9.1` |
| nginx (mgmt-gateway) | `cp.icr.io/cp/ibm-ceph/nginx-124-rhel9:latest` |
| oauth2-proxy | `cp.icr.io/cp/ibm-ceph/oauth2-proxy-rhel10:v7.14.0` |

The nginx default is the floating tag `latest`: whatever selects it must freeze
the digest it resolves, so two operations a day apart may hold different
digests. Every auxiliary repository differs from the Ceph image's, which
matters to the pull broker's single frozen repository and to the Entitlement
namespace rule.

## Known service types

`ServiceSpec.KNOWN_SERVICE_TYPES` there: agent, alertmanager, alloy,
ceph-exporter, cephfs-mirror, container, crash, grafana, ingress, iscsi, loki,
mds, mgmt-gateway, mgr, mon, nfs, node-exporter, node-proxy, nvmeof,
oauth2-proxy, osd, prometheus, promtail, rbd-mirror, rgw, smb, snmp-gateway.
A known type establishes no vendor support; `iscsi` is listed.

## Service specification fields

That build's `GrafanaSpec` has `certificate_source` and `ssl` (default true);
`MgmtGatewaySpec` has `virtual_ip`, `enable_auth`, `certificate_source` and
`custom_sans`; `OAuth2ProxySpec` has `ssl_insecure_skip_verify` (default false)
and no provider CA field; `IngressSpec` has `enable_haproxy_protocol`,
`keepalive_only`, `virtual_ips_list`, `use_keepalived_multicast`,
`vrrp_interface_network`, `verify_backend_ssl_cert`, `verification_ca_cert`,
`monitor_ssl` and `generate_cert`; `NFSServiceSpec` has
`enable_haproxy_protocol`, `enable_nfsv3`, `ssl`, `tls_*`, `kmip_*` and
`cluster_qos_config`; `NvmeofServiceSpec` has `enable_auth`, mTLS key and
certificate fields, `group` and `max_gws_in_grp` (16); `RGWSpec` has
`rgw_realm_token`, `zone_endpoints`, `generate_cert`, `wildcard_enabled` and
`concentrator*`. Reproduce a signature inside the image with
`python3 -I -c "import inspect; from ceph.deployment import service_spec as s; print(inspect.signature(s.GrafanaSpec.__init__))"`.

## oauth2-proxy template

The manager's `cephadm/templates/services/oauth2-proxy/oauth2-proxy.conf.j2`
writes `ssl_insecure_skip_verify=true` only when the spec field is true. That
build therefore verifies the IdP certificate against the container's own trust
store and offers no provider CA option, unlike the upstream `v20.2.1` reading
that verification is always skipped. Another release may render differently,
so SSO proves verification from the deployed daemon's rendered configuration
(D161).
