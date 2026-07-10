# RHSM proxy convergence and repo CA under TLS inspection

## rhsm.conf [server] proxy must be converged, both directions

**Symptom:** on a proxied storage node, dnf fails on the certificate-based
RHEL repos (baseos/appstream `repomd.xml` unreachable) while third-party
vendor `.repo` files still work; `/etc/yum.repos.d/redhat.repo` entries show
`proxy = _none_`. Conversely, a node moved off the proxy keeps failing
through a dead proxy.

**Root cause:** `subscription-manager` and the RHSM dnf plugin take their
proxy from rhsm.conf's `[server]` section, ignoring the `http(s)_proxy` task
environment; without it the plugin stamps `proxy = _none_` into every
redhat.repo entry.

**Fix:** the `storage_cluster_cephadm` subscription task ALWAYS runs and
converges `[server]` `proxy_scheme`/`proxy_hostname`/`proxy_port`/
`no_proxy`/`proxy_user`/`proxy_password`: written when a proxy is declared
and the RHSM server is not CIDR-bypassed, BLANKED otherwise — blanking
matters because a stale proxy left in rhsm.conf would force a now-direct
node through a dead proxy. Any change (either direction), a fresh
registration, or a repo-CA change triggers `subscription-manager refresh`,
regenerating redhat.repo so the proxy and `sslcacert` land in the RHEL repos
before the vendor license package pulls RHEL dependencies. rhsm.conf is
tightened to `0600` when proxy credentials are written into it.

## RHSM CDN fails TLS behind an SSL-bump proxy despite system trust

**Symptom:** under a TLS-inspecting proxy, `get_url`/podman/vendor-`.repo`
fetches work after the proxy CA is installed, but dnf against the RHSM repos
still fails with `unable to get local issuer certificate`.

**Root cause:** subscription-manager stamps
`sslcacert=/etc/rhsm/ca/redhat-uep.pem` into every redhat.repo entry, and
libcurl uses that as `CURLOPT_CAINFO` — REPLACING the system trust store,
not augmenting it. dnf verifies `cdn.redhat.com` against redhat-uep.pem
alone, which does not contain the proxy's signing CA.

**Fix:** when the proxy declares `connection.trustBundleRef`, point
rhsm.conf `[rhsm] repo_ca_cert` at `/etc/pki/tls/certs/ca-bundle.crt` (the
system-extracted bundle `update-ca-trust` already merged the proxy CA into).
This converges both ways — the stock redhat-uep.pem is restored when no
inspecting proxy is declared — and a change triggers `subscription-manager
refresh` so redhat.repo regenerates with the new `sslcacert`.

## Trust anchors: ordering and filename layout

**Constraint:** the egress proxy's TLS-inspection CA
(`bootwright_proxy_trust_bundle_ref`) must be installed into the node's
SYSTEM trust store before ANY package work — before both the community and
subscription repository stages — so the merged bundle exists first. The
system store covers `get_url`, podman, and third-party `.repo` fetches
(whose `sslcacert` defaults to the system bundle); the RHSM repos are the
separate `repo_ca_cert` case above. The anchor converges both ways: it is
removed when no inspecting proxy declares a `trustBundleRef`.

**Why three anchor files:** three deliberately distinct filenames prevent CA
collisions on one node: `bootwright-cephadm-registry.pem` (registry trust),
`bootwright-satellite-ca.pem` (Satellite CA, matching the managed-OS
Kickstart anchor name), and `bootwright-proxy-ca.pem` (proxy SSL-bump CA) —
all under `/etc/pki/ca-trust/source/anchors/` and all removed by destroy.
