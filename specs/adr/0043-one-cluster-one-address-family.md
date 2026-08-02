# ADR 0043: One Cluster, One Address Family

## Status

Accepted

Declares single-stack the `v1alpha1` scope for a `ContainerCluster` and fails
closed on a second address family; extended by
[ADR 0044](0044-the-endpoint-a-single-node-cluster-answers-at.md), which adds
the third way a slot can own its one address.

## Context

`ContainerCluster.spec.install.endpoints.<slot>.address` is a scalar. The native
install-config fields it renders into — `platform.baremetal.apiVIPs` and
`ingressVIPs` — are lists, and they are lists for exactly one reason: a
dual-stack cluster carries one VIP per address family, so `apiVIPs` holds an
IPv4 VIP and an IPv6 VIP and the installer configures keepalived for both.

Bootwright renders one entry into each list, from one scalar. So a fleet
authored with an IPv4 machine network and an IPv6 service network — or an IPv6
`clusterNetwork` alongside an IPv4 endpoint — decoded, normalized, validated,
and rendered a **single-stack** install-config. Nothing refused it and nothing
warned. The operator got a cluster with half the addressing they wrote, and the
first symptom was a workload that could not reach a service.

The rest of the codebase had already assumed single-stack without saying so.
`applyClusterNetworkDefaults` derives the default `clusterNetwork` and
`serviceNetwork` family from *the* machine-network family — one boolean,
`clusterMachineNetworkIsIPv6Only`, with no third state. `vipsFromEndpoints`
appends at most one VIP per list. A partial family check existed
(`clusterNetwork[0]` versus `serviceNetwork[0]`) that caught one pairing out of
several and named `openshift-install`'s ordering requirement rather than the
scope it was actually enforcing.

Meanwhile IPv6-only was, and is, a supported posture: the IPv6 defaults above
exist precisely to serve it. The gap was never "IPv6 does not work" — it was
that *mixing* silently did not.

Dual-stack is real work: `address` would have to become `addresses[]`, every
address-consuming validator (endpoint-in-machine-network, VIP/node-IP collision,
prefix length) would have to reason per family, and the DNS and load-balancer
renderers would have to emit records per family. None of that is in scope for
`v1alpha1`, and none of it is served by pretending the scalar can express it.

## Decision

**Single-stack is the declared `v1alpha1` scope.** One `ContainerCluster`
carries one IP address family. This is a stated boundary of the API, not an
accident of the implementation.

**A second family fails closed at validation.** The effective networking of a
cluster is collected across the inputs that reach the rendered install-config,
in the order the install reads them:

- every `machineNetwork[].cidr` of the `NetworkConfig`s the cluster's nodes
  consume, plus any inline `spec.network.config.spec.machineNetwork[]` a node
  declares;
- `spec.networking.clusterNetwork[].cidr` and `spec.networking.serviceNetwork[]`;
- the resolved address of each `api`, `api-int`, and `ingress` endpoint — the
  authored `address` or the `infraComponent` source's bind address.

Node install addresses are deliberately **not** collected separately: an
`interfaceAddresses`-resolved install IP must already fall inside a selected
machine network, so the machine networks cover them and a second rule would
only produce a second message for the same input.

**The refusal names both values and the scope.** It names the cluster, the
first value with its family, the conflicting value with its family, and states
that single-stack is the current `v1alpha1` scope — because the operator's next
question is "which of the two did I mean", and an error that names only one of
them cannot answer it.

**Mixing is refused; IPv4 is not required.** An all-IPv6 cluster stays legal,
including its IPv6 `clusterNetwork` and `serviceNetwork` defaults. The rule has
one subject: two families in one cluster.

**The narrower rule it replaces is removed.** The `clusterNetwork[0]` versus
`serviceNetwork[0]` check was a strict subset of this one. Keeping both would
emit two refusals for one authoring mistake.

## Consequences

- A fleet that authored a second family and silently rendered single-stack now
  fails validation instead of installing. That is the intended break: the
  refusal arrives before the install rather than after it.
- `render effective` and `apply` inherit the rule for free — it is a validation
  rule, so every verb that loads desired state enforces it.
- The deferred alternative is recorded and shaped: promote
  `endpoints.<slot>.address` to `addresses[]`, at most one entry per family,
  and teach the address-consuming validators and the DNS/load-balancer
  renderers to reason per family. Nothing in this decision blocks that; the
  refusal becomes the definition of what `addresses[]` would relax.
- The rule reads the *resolved* endpoint address, so an `infraComponent` bind
  address of the other family is caught at the same place as a literal one.
