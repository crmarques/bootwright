# ADR 0012: Proxy Fan-Out, Per-Directive Bypass, and TLS-Inspection Trust

## Status

Accepted

## Context

Real estates sit behind corporate egress proxies, often authenticated and
sometimes TLS-inspecting (SSL-bump). Bootwright has three proxy consumers
with different lifecycles: its own runtime actions (`bootwright`), the
OpenShift/OKD installer input (`containerClusterInstall`), and the managed-OS
Anaconda install fetch (`machineOSInstall`), which runs before any
Bootwright-managed component exists.

Downstream proxy-bypass implementations are inconsistent. Anaconda's
`rhsm`/`url`/`repo` kickstart commands have no `no_proxy` directive at all.
`dnf.conf`/`yum.conf` accept a `proxy=` line but no `no_proxy` exceptions.
python-rhsm's rhsm.conf `[server] no_proxy` matcher and Ansible's `uri`
module cannot match CIDR entries. A TLS-inspecting proxy presents its own CA
on every origin, and RHSM's stamped `sslcacert` replaces rather than
augments the system trust store. Ad-hoc handling produced silently proxied
internal hosts, nodes stuck behind dead proxies, and certificate failures
deep inside plays.

## Decision

**Fan-out model.** Proxy catalog entries live under
`Environment.spec.infraComponents.proxies[]`; at most one entry may be
`default: true` (validated; `DefaultProxyName` returns the first
defensively). `spec.proxyFor` is a per-consumer override map with a closed
consumer set (`bootwright`, `containerClusterInstall`, `machineOSInstall`):
each slot names a proxy (override), carries the sentinel `none` (opt out),
or is empty (inherit the default). Default election is opt-in — a single
un-defaulted entry is NOT an implicit default, deliberately unlike
registries; with no default, an empty slot means no proxy.
`machineOSInstall` accepts only an `external` proxy: a boot ISO carries no
packages, so Anaconda fetches over the network before a managed proxy could
exist — a managed value, or inheriting a managed default, is rejected at
validation rather than silently skipped. Managed entries select an
`InfraComponent` by `componentRef`; the URL is derived from `(machineRef,
port)` via `ManagedProxyURL`, with the cluster-facing host substituted by
`ClusterFacingHostAddress` when the machine's own address is not routable.
`ResolveEnvForContext` yields no bootstrap proxy env for a managed
`bootwright` selection, since the proxy does not exist yet. Rendered proxy
vars echo the resolved (post-default, post-opt-out) proxy each consumer
actually routes through, not the raw authored slot.

**Per-directive bypass at render time.** Because Anaconda has no `no_proxy`,
the bypass decision for `machineOSInstall` is made per directive at render
time (`installTargetProxied`) and threaded down as per-target flags:
`rhsm.proxied`, `installer.sourceProxied`, `repositories[].proxied`. A
directive gets `--proxy=` only when its target host is not bypassed by the
effective `no_proxy`; an empty host (the public Red Hat CDN rhsm case) is
never bypassed. Credentialed proxy URLs are assembled inside the kickstart
from the unauthenticated URL plus a credentials-file path, keeping the proxy
password out of vars.yaml.

**no_proxy CIDR handling.** `ResolveNoProxy` expands CIDR entries into
pinned concrete IP literals drawn from `noProxyTargets`, which must span
every internal endpoint the estate talks to (machines, BMCs,
artifact-server endpoints, registries, name-resolution and NTP components,
the mirror URL, and RHSM Satellite hosts). CIDR-blind matchers get the
literal variant (`NoProxyForLiteralMatchers`/`noProxyLiteral`) with raw
CIDRs dropped; a Satellite known only by FQDN is resolved node-side
(`getent`) and, when its address falls inside a `noProxy` CIDR, the proxy is
left out of rhsm.conf entirely. `proxy.Bypasses` defines the single matching
contract (domains with and without leading dot, `*`, CIDRs, empty list).

**TLS-inspection trust wiring.** A proxy that re-signs HTTPS declares its CA
as `connection.trustBundleRef`. Bootwright installs it into the system trust
anchors of every managed host egressing through the proxy before any package
work, and removes it when no inspecting proxy declares one. RHSM repos are
special-cased via rhsm.conf `[rhsm] repo_ca_cert` pointed at the
system-extracted bundle, because the stamped `sslcacert` replaces the trust
store. Cluster installs auto-fold the proxy CA into install-config
`additionalTrustBundle`, mirroring the mirror-CA fold. A plain
CONNECT-tunnelling proxy contributes nothing.

**Environment hygiene.** Controller bootstrap subprocesses never inherit
ambient proxy env: `MergeBootstrapEnv` strips
`HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY` (both cases) and the operator proxy is
re-injected explicitly; sudo-wrapped installs preserve exactly
`SudoPreservedProxyVars` on request. Package managers are driven from the
environment only — never a `proxy=` config line, which would defeat
`no_proxy`.

## Consequences

- Adding a proxy consumer means a new `proxyFor` slot plus explicit
  render-time bypass decisions for any consumer whose fetch mechanism lacks
  `no_proxy` support.
- Every new kind of internal endpoint must be added to `noProxyTargets`, or
  hosts reachable only through a `noProxy` CIDR are silently proxied by
  CIDR-blind consumers.
- A managed `default: true` proxy forces `machineOSInstall` to be set
  explicitly (external proxy or `none`).
- Operator documentation lives in `docs/advanced/disconnected-proxy.md`;
  engineering gotchas in `.agents/knowledge/no-proxy-cidr-matching.md`,
  `rhsm-proxy-and-repo-ca.md`, `machine-proxy-persistence.md`, and
  `url-authority-gotchas.md`.
- Rendered vars expose the resolved per-consumer proxy, making fan-out
  decisions debuggable from render output alone.
