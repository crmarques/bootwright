# Cluster-facing address resolution for managed proxy and mirror URLs

**Managed proxy URL:** environment proxy catalog entries with
`management: managed` resolve to InfraComponent proxy services, and the
renderer derives the URL from the component's `(machineRef, port)` via
`internal/infra/proxy.ManagedProxyURL`. The cluster-facing URL substitutes
the primary network's gateway via `proxy.ClusterFacingHostAddress` — e.g. a
bastion whose SSH address resolves to localhost gets the gateway of the
cluster's api-endpoint network substituted, so the proxy or mirror URL is
reachable from cluster guests.

**Fallback chain:** `ClusterFacingMachineAddress` resolves the
cluster-reachable address of a machine (e.g. the managed-proxy host) with a
tested fallback chain: a non-loopback SSH address is used directly;
loopback/`::1`/`0.0.0.0`/empty addresses fall back to the primary network's
default-route gateway; and when the cluster install declares no endpoints,
the primary network is still found via the first install machine's first
interface (`networkConfigRef`). Loopback with no gateway yields empty, which
`ManagedProxyURL` turns into the `has no routable address` error.

**Mirror registry host:** `mirrorRegistryHost` resolves the cluster-facing
host of the managed mirror registry preferring a declared `endpoints[]`
address (parity with `artifactServer`: a named endpoint wins, else a sole
endpoint) or an explicit routable bind address, falling back to the
machine's SSH/route address only when nothing is declared — because a
loopback bastion SSH alias would otherwise silently resolve the mirror to
the network gateway.
