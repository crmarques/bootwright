# ADR 0049: The Scheme a Gateway Declares Out Loud

## Status

Accepted

Revises [ADR 0047](0047-the-certificate-a-vendor-gateway-never-settles-on.md):
the implicit vendor `ssl: false` pin becomes an authored workaround plus this
declared field. Extended by
[ADR 0052](0052-the-port-a-scheme-arrives-on.md): the declared scheme also
decides the gateway's default port, so `exposure: http` no longer serves
cleartext on the TLS-conventional 8443.

## Context

ADR 0047 established that on subscription-backed distributions no ssl-enabled
`mgmt-gateway` spec converges: the vendor cephadm builds recompute daemon
dependencies with certificate entries (`certificate_source`, inline cert/key
hashes) but record them without, so inline, reference, and cephadm-signed
certificates all reconfigure the gateway daemons forever. Bootwright's answer
was to pin `ssl: false` into every vendor gateway spec implicitly, keyed on
the distribution.

The implicit pin has two problems. First, it hides a real security decision:
the operator reads `mgmtGateway` in their desired state and gets a cleartext
dashboard without ever having said so. Second, it hard-wires today's vendor
defect into the render: the upstream fix is in flight (the dependency
recording gains the spec), and when a vendor build ships it, an implicitly
pinned Bootwright would keep serving plain HTTP until a code change — the
exact coupling a declarative model exists to avoid.

## Decision

**`spec.ceph.mgmtGateway.exposure` declares the scheme the gateway itself
serves: `https` or `http`. The default is `https`.** `http` renders the
gateway spec with `ssl: false` on every distribution and engages the
management phase's persisted-spec repair; it rejects `tls` (a certificate on
a cleartext listener is a contradiction) and `oauth2Proxy` (SSO cookies and
access tokens must not cross the network in cleartext).

**The defect is selected by authored workaround data, never inferred from a
distribution or release number.** The closed
`spec.ceph.cephadm.workarounds[]` token
`mgmt-gateway-spec-dependency-recording` records that the selected cephadm build
has the defect from ADR 0047. Validation accepts that token only with a declared
gateway using `exposure: http`, no `tls`, and no `oauth2Proxy`. The token is
distribution-neutral: any affected build may select it, and a repaired vendor
build simply omits it.

**Without the workaround, native HTTPS, TLS, and OAuth are valid on every
distribution.** Bootwright carries no release-to-defect catalog and does not
make an operator wait for a Bootwright release when a fixed supplier build
appears. Service readiness and the live manager capability probe remain the
authoritative runtime gates.

## Consequences

- Environments using an affected build carry the workaround token and
  `exposure: http`; they remove the token and select the desired native gateway
  shape when a repaired build lands. No Bootwright code change is part of that
  flip.
- The cleartext dashboard is now a visible, versioned line in the desired
  state instead of a rendering side effect — reviewable where security
  decisions get reviewed.
- `exposure` defaults to `https` on every distribution, and `http` remains
  available independently of the workaround.
- ADR 0047's distribution-keyed `ssl: false` pin is superseded by the exposure
  field plus authored workaround validation. Store repair and settle waiting
  key to `exposure: http`, not the workaround token, because the falsy-field
  serialization defect can affect any cephadm lineage. Dashboard-port vacation
  and internal firewall opening key to gateway presence.
