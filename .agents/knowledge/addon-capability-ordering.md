# Add-on capabilities: open vocabulary, reserved names, per-binding ordering

**Constraint:** `ClusterAddon spec.provides`/`spec.requires` capabilities are
an OPEN vocabulary (any token-shaped string) so add-on content — not just
compiled bootwright — can declare and order on its own capabilities. Three
names carry reserved planning semantics in core: `kubevirt` (host-cluster
provisioning of KubeVirt-backed nodes), `dataFoundation` (the storage-export
attachment effect provider), and `nmstate` (ordering-only). Their special
handling lives in the planner, not the validator. `provides` mandates a
readiness check (e.g. `csvSucceeded`); `requires` does not.

**Ordering algorithm:** `plan.orderByCapabilities` is a stable topological
sort over `spec.requires`/`spec.provides` within one binding: add-ons with no
requires/provides edge between them keep their original
binding/profile-expansion order (Kahn's algorithm draining ready nodes in
original index order for determinism), and only a requirement that would
otherwise resolve too late forces a move. A `requires` whose capability no
add-on in the binding provides imposes no edge in the planner — validation
reports the unsatisfied requirement separately — while a requires/provides
cycle is an error. plan_test pins that the provider (nmstate) is pulled ahead
of its consumer while an independent add-on keeps its authored slot.

**Per-binding scope:** Capability ordering is resolved PER BINDING, so a
`spec.requires` capability must be provided by an add-on in the SAME binding —
a provider in a sibling binding on the same cluster cannot be ordered against.
Diagnostics deliberately say "this binding", not "this cluster", to point at
the real fix. Cycles are also detected per binding, and listing order is
irrelevant when requirements are satisfied.
