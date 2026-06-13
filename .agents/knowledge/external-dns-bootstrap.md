# Endpoint DNS missing: bootstrap API never initializes

**Symptom:** `openshift-install agent wait-for install-complete` loops on
`Bootstrap Kube API never initialized`, then fails with a controller-side
lookup error such as `lookup api.<cluster>.<baseDomain> ... no such host`.

**Root cause:** Cluster endpoints must resolve from the controller host,
because `openshift-install` polls the API from there. If this resolver path
is missing or stale, `openshift-install` reaches the 90-minute bootstrap wait
before surfacing the underlying `no such host`.

**Fix:** `install_agent` first wires the controller resolver, then gates on it.

- Wiring (`stage/controller_resolver.yml`, runs on the localhost controller
  before the gate): when a cluster's node network references a **managed**
  `nameResolution` component, the renderer projects
  `bootwright_current_cluster.controllerNameResolvers = [{bindAddress, domain}]`
  (see `ClusterControllerNameResolvers`). The stage installs a systemd-resolved
  drop-in (`/etc/systemd/resolved.conf.d/bootwright-<cluster>.conf`) routing
  `~<baseDomain>` to the dnsmasq bind address and restarts resolved.
  `agent_destroy` removes the drop-in and restarts resolved. A routing-domain
  drop-in (not per-link DNS) is used because the gate runs before nodes boot,
  when the cluster bridge has no carrier; the bind address is a local/
  controller-reachable address so it answers regardless.
- Gate (`stage/controller_dns.yml`): `getent hosts` for each cluster endpoint;
  fails listing all missing or stale answers.

Auto-wiring needs an explicit non-wildcard `bindAddress` on the managed
component: a `0.0.0.0`/unset bind (dnsmasq `bind-dynamic`) has no single
controller-reachable address to point resolved at, so it is skipped and the gate
falls back to the operator.

Bootwright still does not touch `/etc/hosts`, and it only auto-wires through
systemd-resolved. For **external** name resolution, a controller without
systemd-resolved, or any unmanaged path, endpoint resolution remains
operator-owned (point the controller's resolver at the records yourself).
Managed dnsmasq records are rendered from Go as exact `host-record` entries for
`api` and `api-int` plus an anchored `apps.<cluster>.<baseDomain>` subtree.
