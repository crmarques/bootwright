# ADR 0047: The Certificate a Vendor Gateway Never Settles On

## Status

Accepted

## Context

`spec.ceph.mgmtGateway.tls` injects an operator-provided certificate into the
`mgmt-gateway` service spec as cephadm's `ssl_cert`/`ssl_key` fields. On IBM
Storage Ceph 9.9.1 (`20.2.1-324.el9cp`), applying that spec put a six-node
production cluster into a permanent reconfigure loop: every cephadm serve pass
logged `Reconfiguring mgmt-gateway.node-0N deps [...] -> [... 'certificate_source:
inline', 'ssl_cert: <md5>', 'ssl_key: <md5>']` for all six gateways, nginx
restarted each cycle, the service never held its declared daemon count at any
single poll, and apply failed closed at the service-readiness gate after 90
attempts with `mgmt-gateway (2/6)`.

The mechanism is a backport asymmetry, confirmed against the upstream `main`
source the vendor build carries. Two paths compute a daemon's dependencies:

- The scheduler's `_check_daemons` calls `get_dependencies(mgr, spec,
  daemon_type)`. The base implementation appends `certificate_source: <src>`
  whenever the spec has `ssl` enabled and a certificate source, plus
  `ssl_cert`/`ssl_key` content hashes when the certificate is inline.
- The recording path — `MgmtGatewayService.generate_config`, whose return value
  becomes the daemon's *stored* dependencies — ends `return daemon_config,
  sorted(MgmtGatewayService.get_dependencies(self.mgr))`, passing no spec, so
  the stored list can never contain any certificate entry.

`last_deps != deps` therefore holds on every pass, forever. The asymmetry rules
out every shape that carries a user certificate on such a build: inline
`ssl_cert`/`ssl_key` loops (observed); routing the certificate through the
cephadm certificate store with `certificate_source: reference` loops the same
way, because the `certificate_source:` entry alone breaks the match; only a
spec with no certificate fields computes no certificate dependencies and
converges — which is how certificate-less clusters on the same build always
deployed cleanly. A TLS-free spec serves the cephadm-managed certificate, not
the operator's: the store entity a previous inline apply ingested is read only
on the inline and reference paths. Upstream community `v20.2.x` carries neither
the certificate dependencies nor the mismatch, so the same block converges on
`distribution: oss`.

There is no knob on Bootwright's side of the contract that repairs this. The
code that disagrees with itself runs inside the vendor's manager container, and
no released vendor build fixes the recording.

## Decision

**Validation refuses `spec.ceph.mgmtGateway.tls` on subscription-backed
distributions** (`redhat`, `ibm`). The refusal names the loop it prevents —
dependencies recomputed with the certificate but recorded without it, gateways
reconfigured every pass, apply failing closed at service readiness — and both
exits: omit the block and serve the cephadm-managed certificate, or run
`distribution: oss`, whose tentacle builds compute no certificate dependencies.
A vendor build that records certificate dependencies lifts the refusal by
narrowing the gate, not by an operator override; a spec that deterministically
wedges a cluster is not an authorization decision, so no token applies
(ADR 0030 governs consequences an operator may accept; a permanently
reconfiguring gateway is not one).

**The management phase runs for every declared gateway, secrets or not.** A
gateway without TLS or OAuth2 renders into the ordinary late-services spec for
the native script path, but the Ansible management phase still runs: disabling
the classic dashboard's SSL listener and moving its HTTP port off the gateway's
— the mitigation that frees the gateway port on every mgr host — is not
certificate work, and gating it on secrets strands the gateway daemons on
every host that also runs a mgr (observed on the first TLS-free apply:
`mgmt-gateway (2/6)`, running only on the two mgr-less nodes). The phase then
(re)applies the gateway document after the port is freed; only the certificate
and oauth2-proxy material stays secrets-gated inside the spec it assembles. A
cluster previously wedged by an inline-TLS spec self-heals on the next apply —
re-applying the TLS-free document makes the computed and recorded lists agree
again, the loop stops, and the daemons settle.

## Consequences

- A vendor-distribution dashboard is reached through the cephadm-managed
  certificate until the vendor repairs the recording; operators who need their
  own certificate on the management VIP need `distribution: oss` or a fixed
  vendor build. Environment templates that mint a management-gateway
  certificate must author the `tls` block only for the community arm.
- The gate is deliberately wider than the one observed build. Both vendor
  streams ship the same downstream source, no released build is known fixed,
  and the failure mode — a production cluster restarting its gateway tier
  forever — costs more than a refusal that a fixed build later narrows.
- `StorageObjectGateway` ingress TLS is untouched: cephadm's ingress spec has
  no `ssl` attribute feeding the same base dependency computation, so haproxy
  certificates do not loop.
- The service-readiness failure now also collects `journalctl` tails over the
  cluster SSH identity from every node whose daemon cephadm reports in
  `error`, `unknown`, `stopped`, or still `starting` after the readiness window
  is exhausted, so a daemon that dies at start — or never finishes starting —
  is diagnosed from the failure output instead of a follow-up session on the
  nodes.
