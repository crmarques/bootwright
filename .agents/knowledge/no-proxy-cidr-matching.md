# no_proxy matching contract and CIDR expansion

**Matching contract:** `proxy.Bypasses(eff, host)` implements the effective
`no_proxy` decision. The input may be a bare hostname, IP literal, URL, or
`host:port` authority and is reduced to a host first; `*` bypasses
everything; a domain entry in either the leading-dot form (`.example.com`)
or bare form (`example.com`) matches the domain itself AND its subdomains,
case-insensitively; a CIDR entry matches when the host is an IP inside the
prefix; an empty `no_proxy` never bypasses.

**CIDR expansion:** `ResolveNoProxy` expands CIDR `noProxy` entries into
pinned concrete IP literals via `noProxyTargets`, because bypass
implementations that cannot match a CIDR (python-rhsm's proxy bypass and
Ansible's `uri` module — see `redfish-proxy-bypass.md`) silently proxy hosts
inside a declared CIDR like `10.0.0.0/8`.
`TestResolveNoProxyExpandsCIDRForInternalServiceIPs` is the regression test
for the rhsm.conf incident.

**Span constraint:** `noProxyTargets` must span EVERY internal endpoint the
estate talks to — machine addresses, BMC hosts, artifact-server endpoints,
registries, name-resolution and NTP components, the mirror URL, and the RHSM
Satellite hostname/contentBaseURL — not just BMCs. A Satellite, mirror, or
registry reachable only through a `noProxy` CIDR that is missing from the
target set is silently proxied.

**CIDR-stripped literal variant:** `NoProxyForLiteralMatchers` (render:
`noProxyLiteral`, projected as `bootwright_proxy_no_proxy_literal`) returns
the effective NoProxy list with raw CIDR entries dropped — domains,
wildcards, and concrete host/IP literals kept — for consumers whose bypass
matcher cannot handle CIDRs, notably python-rhsm's rhsm.conf `[server]
no_proxy`, which silently ignores an entry like `10.0.0.0/8`. This is safe
only because `ResolveNoProxy` has already expanded each CIDR into the
concrete internal IPs it covers, which survive as literals. Ansible
consumers fall back to the full `no_proxy` list when render did not emit a
literal variant.
