# ADR 0052: The Port a Scheme Arrives On

## Status

Accepted

Extends [ADR 0049](0049-the-scheme-a-gateway-declares-out-loud.md): the scheme
the gateway declares now also decides the port it defaults to. ADR 0049's
`exposure` field, its explicit-declaration rule on subscription-backed
distributions, and its refusals of `tls` and `oauth2Proxy` under `http` are
unchanged.

## Context

ADR 0049 gave the management gateway a declared scheme and left the port where
it had always been: a single `CephMgmtGatewayDefaultPort = 8443`, applied to
every gateway regardless of what the listener speaks. The two halves compose
into a gateway that serves cleartext on 8443 — the number that TLS owns by
convention everywhere else in the estate, including this project's own artifact
server.

That is not cosmetic. `bootwright cluster info` prints the URL, operators paste
it, browsers and scanners read the port before they read anything else, and a
firewall rule written against 8443 documents a TLS exposure that does not
exist. On subscription-backed distributions, where `exposure: http` is the only
shape that converges today, every cluster lands there — so the misleading form
is the common one, not the edge case.

The classic dashboard's own TLS listener also defaults to 8443, and the
management phase already vacates it so gateway daemons can bind. One constant
was serving both meanings: the gateway's listening port and the classic
dashboard's TLS port. Their values coincide; their reasons do not.

## Decision

**The default port follows the declared scheme: `8443` for `exposure: https`,
`8888` for `exposure: http`.** `mgmtGateway.port` remains authorable and still
wins outright. The default is `8888` and not `8080` because RGW's beast
frontend and the classic dashboard both default to `8080`, and the gateway is
placed on ingress hosts that commonly run RGW — a default that invites a bind
collision is not a default.

**A port whose convention contradicts the scheme is refused:** `http` on `443`
or `8443`, `https` on `80` or `8080`. Nothing downstream can recover the
operator's intent from a number that says the opposite of the spec, and the
refusal names the conventional default for the declared scheme. Ports carrying
no scheme convention stay free.

**The classic dashboard's TLS port becomes its own constant.** It is a fact
about the mgr dashboard module, not about the gateway, and the gateway-less
access summary reads it directly.

## Consequences

- Breaking for any cluster on `exposure: http` that never authored a port: its
  dashboard URL moves from `:8443` to `:8888`. Estates that must hold the old
  URL author `mgmtGateway.port: 8443` — which the new refusal rejects, on
  purpose, because holding a cleartext listener on the TLS port is exactly the
  shape this ADR removes. Environment templates expose the port so the choice
  is made where the rest of the gateway is described.
- The flip ADR 0049 promised stays a single edit: moving `exposure` from `http`
  to `https` on a repaired vendor build carries the port with it, and only an
  estate that pinned a port has anything else to change.
- The dashboard-port vacation is unaffected: it already moves the classic
  dashboard clear of whatever port the gateway occupies, and keys to gateway
  presence rather than to the scheme.
