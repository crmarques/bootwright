# IPv6 installation-feature rehearsal

The IPv6-only variant of [lab-features](../lab-features/): same cluster name
`features`, same three masters, TPM2 encryption, managed proxy and external
mirror, with IPv6 machine, pod and service networks. It is a synthetic
preparation profile for [B728](../../../specs/milestones/m3.md#b728) and the
[acceptance matrix](../../../specs/openshift-coverage.md#acceptance-profiles).
No live run or owner acceptance is claimed, and the accepted
[lab-sno](../lab-sno/) and [lab-compact](../lab-compact/) rows are
prerequisites to the consuming M3d run.

## Worth noting

- Capacity is lab-features': at least 28 logical CPUs, 56 GiB available
  memory and 400 GiB pool disk including reserve, plus media space.
- Machine network `2001:db8:2::/64`, API VIP `2001:db8:2::10`, ingress VIP
  `2001:db8:2::11`, cluster network `fd00:10:128::/56` (host prefix 64),
  service network `fd00:172:30::/112`. IPv4 is disabled on the nodes, with
  explicit `dhcp: false` and `autoconf: false`; keep both.
- The managed guest network uses `forward: none`. The controller must be
  reachable at an IPv6 address from the guests, and you provide routing between
  the network and the controller services; IPv4 NAT is not assumed to carry
  IPv6. The proxy, mirror, DNS and NTP endpoints must be reachable from the
  IPv6-only guests.
- Mirror credentials and trust bindings are the same as lab-features. This is
  an IPv6 row of B725/B728, not dual-stack support.
- Replace the documentation IPv6 prefixes consistently, including certificate
  SANs, VIPs, gateway and Machine addresses.

Run it with the e2e [`lab-3-nodes-ocp-features`](../../../e2e/lab-3-nodes-ocp-features/runbook.md) case.
