# Endpoint DNS missing: bootstrap API never initializes

**Symptom:** `openshift-install agent wait-for install-complete` loops on
`Bootstrap Kube API never initialized`, then fails with a controller-side
lookup error such as `lookup api.<cluster>.<baseDomain> ... no such host`.

**Root cause:** Cluster endpoints need TWO independent resolver paths to
work, whether they use operator-owned `externalVip` or Bootwright-managed
`providedBy`: (1) the controller host, because `openshift-install` polls
the API from there; (2) every resolver listed in the DNS servers in the
rendered host NMState from `NetworkConfig`, because the cluster's nodes
use those at boot and the API VIP backed by HAProxy cannot accept the
bootstrap join until the node has resolved its own
`api.<cluster>.<baseDomain>`. Lab-emulated cases 002 and 004 can reuse
001's bootwright-managed dnsmasq when their `NetworkConfig` resolver points
at that managed resolver address. If either resolver path is missing or stale,
`openshift-install` reaches the 90-minute bootstrap wait before surfacing
the underlying `no such host`.

**Fix:** `install_agent` runs two preflights before booting nodes.
1. **Controller-side:** `getent hosts` for each cluster endpoint and fail
   with all missing or stale answers. Bootwright does not mutate
   `/etc/hosts`; endpoint name resolution must come from operator-owned
   DNS, Bootwright-managed DNS, or another controller-visible resolver.
2. **Node-side:** for every DNS server rendered from `NetworkConfig` × every
   endpoint hostname, `dig +short @<server> <host>` and assert the
   answer set contains the expected VIP. A `validateNoWildcardDNS.<cluster>
   .<baseDomain>` negative probe also runs — a wildcard pattern that
   catches it would fail the assisted installer. Both preflights collect
   ALL failures and fail once with a single actionable message rather
   than per-probe noise. Managed dnsmasq records are rendered from Go as
   exact `host-record` entries for `api` and `api-int` plus an anchored
   `apps.<cluster>.<baseDomain>` subtree.
