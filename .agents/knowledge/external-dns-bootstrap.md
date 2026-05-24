# Endpoint DNS missing: bootstrap API never initializes

**Symptom:** `openshift-install agent wait-for install-complete` loops on
`Bootstrap Kube API never initialized`, then fails with a controller-side
lookup error such as `lookup api.<cluster>.<baseDomain> ... no such host`.

**Root cause:** Cluster endpoints must resolve from the controller host,
because `openshift-install` polls the API from there. If this resolver path
is missing or stale, `openshift-install` reaches the 90-minute bootstrap wait
before surfacing the underlying `no such host`.

**Fix:** `install_agent` runs a controller-side preflight before booting
nodes: `getent hosts` for each cluster endpoint and fail with all missing or
stale answers. Bootwright does not mutate `/etc/hosts`; endpoint name
resolution must come from operator-owned DNS, Bootwright-managed DNS, or
another controller-visible resolver. Managed dnsmasq records are rendered
from Go as exact `host-record` entries for `api` and `api-int` plus an
anchored `apps.<cluster>.<baseDomain>` subtree.
