# P3: Ceph observability and management

Profile [P3](../../../specs/ceph-coverage.md#profile-catalog): four RHEL
guests running an IBM Storage Ceph cluster (`ceph-lab`) with the observability
and management services. Prometheus (15d/10GB retention), Alertmanager,
Grafana, node and Ceph exporters, an SNMPv3 gateway, Loki with Alloy on every
host, and a management gateway fronted by a keepalive-only ingress VIP with
OAuth2 SSO through oauth2-proxy. The storage side is a size-2/min-size-2 RBD
pool, a placement policy and a pool client over one SSD OSD per guest.

Synthetic qualification input for [M2c](../../../specs/milestones/m2.md#m2c).
No live execution or operator acceptance is claimed.

## Worth noting

- Topology: `rhel-01` to `rhel-04`, 4 vCPU each; RAM 16/16/10/20 GiB, root
  60/60/60/80 GiB, one 80 GiB SSD data disk each. Total 16 vCPUs, 62 GiB RAM.
  Reserve 4 GiB controller memory. MONs on hosts 1-3, MGRs and the management
  gateway on hosts 1-2, the observability stack on `rhel-04`, which has no
  redundancy. The extra RAM is a qualification allowance, not a vendor minimum.
- OSD autotuning is off with a 4 GiB target (`4294967296`), not a process cap.
- Management VIP `198.51.100.50`, port 9443; allow the gateway port and VRRP
  on both ingress hosts.
- Required operator inputs: a trusted OIDC IdP with client registration and
  role mapping (`oidc-client`, `oidc-cookie`, `oidc-ca`), an SNMPv3 receiver
  (`snmp-auth`, `snmp-privacy`), `management-tls`, `grafana-admin`, and DNS for
  the endpoint, IdP and callback. Replace the IdP issuer, redirect URL and
  SNMP destination placeholders.
- Replace the ten synthetic auxiliary image digests (`sha256:1111...` and so
  on) with verified selections from your release.
- Also required: RHEL 9.8 boot and DVD ISOs and the IBM entitlement key for
  `ibm-registry`. The Entitlement `license.accept: true` is a placeholder;
  review IBM's terms before use.
