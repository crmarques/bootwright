# ADR 0049: The Scheme a Gateway Declares Out Loud

## Status

Accepted

Revises [ADR 0047](0047-the-certificate-a-vendor-gateway-never-settles-on.md):
the implicit vendor `ssl: false` pin becomes this declared field.

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

**On subscription-backed distributions the field must be authored
explicitly.** A defaulted `https` there is a silent walk into the reconfigure
loop, so validation refuses an absent field and names both choices: `http`
converges on every vendor build shipped to date, `https` is the deliberate
declaration for a vendor build that repairs the dependency recording. The
explicit `https` passes validation today — it wedges at the service-readiness
gate on current builds, with the loop visible in the collected diagnostics —
because the alternative, refusing it outright, would demand a Bootwright
release on the same day a fixed vendor build appears. The flip is one edit in
the operator's input, which is where a scheme change belongs.

**`oauth2Proxy` is refused on subscription-backed distributions outright.**
The upstream fix names `oauth2-proxy` as sharing the same spec-less
dependency recording, so vendor SSO daemons loop exactly as the gateway does.
The refusal lifts with the same evidence that lifts the gateway's.

The `tls` refusal on vendor builds (ADR 0047) is unchanged; `tls` remains an
`oss`-plus-`https` combination.

## Consequences

- Vendor environments carry `exposure: http` in their inputs today and flip
  the same field to `https` when a repaired vendor build lands — no
  Bootwright change is part of that flip. Environment templates author the
  field explicitly for their vendor arms.
- The cleartext dashboard is now a visible, versioned line in the desired
  state instead of a rendering side effect — reviewable where security
  decisions get reviewed.
- Community clusters are untouched: `exposure` defaults to `https`, and
  `http` is available there for whoever wants a proxy of their own in front.
- ADR 0047's distribution-keyed `ssl: false` pin is superseded by the
  exposure field. Its store-repair and settle-wait machinery now keys to
  `exposure: http`; its dashboard-port vacation and internal firewall-port
  opening key to gateway presence, because an https gateway collides with the
  classic dashboard's port and serves the internal monitoring endpoint all
  the same.
