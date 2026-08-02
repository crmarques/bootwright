# ADR 0044: The Endpoint a Single-Node Cluster Answers At

## Status

Accepted

Adds `endpoints.<slot>.source.type: node`; extends
[ADR 0043](0043-one-cluster-one-address-family.md) with a third way a slot owns
its one address, and follows the source union of
[ADR 0014](0014-api-grammar.md).

## Context

A single-node cluster has no VIP. There is one machine, it answers at its own
install address, and `api`, `api-int`, and `ingress` all resolve to that same
address. The spec already knew half of this: single-node clusters reject
`source.type: openshift` on all three slots, because `openshift-install agent`
rejects a bare-metal or vSphere platform block for one control-plane node and
those clusters render `platform.none`.

What the spec did not have was a way to say it. The operator was left writing
the node's own address three times, into `api`, `api-int`, and `ingress`, with
`source.type: external` — an `external` source meaning "an operator-owned load
balancer outside Bootwright", which for a single node is not true of anything.

Nothing validated the result. Not that the three agreed with each other, and
not that any of them matched the `Machine` the cluster's one node is bound to.
A typo in one of four places — three endpoints and the machine address — passed
validation and rendered an install-config whose API address belonged to nothing.
The symptom is an install that never converges, hours after the mistake.

The documentation had already been reduced to warning about it. Both
`docs/advanced/networking.md` and `docs/concepts/container-clusters.md` told the
operator to "repeat it verbatim — nothing validates that the two agree". A doc
that has to warn an operator about a gap the schema could close is the schema
asking for a field.

## Decision

**A fourth source: `endpoints.<slot>.source.type: node`.** It means "this slot
answers at the cluster's single node". It joins `openshift`, `external`, and
`infraComponent` in the same closed union, so the question the union already
asks — *who owns this address?* — gains the answer that was missing rather than
a new field beside it.

**Valid only on a cluster with exactly one node.** Any other cluster is refused
naming the node count and the reason: a multi-node cluster answers at a VIP no
single node owns. `external` and `infraComponent` remain the answers there.

**It forbids `address` on the same slot**, the same shape as the existing
`infraComponent` rule and for the same reason: the source owns the address, so
authoring one beside it creates two sources of truth that can disagree. The
refusal names the `Machine` the address comes from.

**One resolution rule, reused.** The address is the node's install address —
the `Machine.spec.addresses[]` entry its
`spec.network.config.interfaceAddresses[]` points at. That is the same path the
install already resolves for the VIP/node-IP collision check and the same path
the network-config renderer walks to place the static install IP, now factored
into one helper both call. No second notion of "the node's address" is
introduced.

**Ambiguity fails closed.** A node whose `interfaceAddresses[]` resolve to no
address, or to more than one, is refused naming what was found. Bootwright will
not guess which address an endpoint answers at, and inventing a tie-break here
would be exactly the second resolution rule this decision avoids.

**Normalize materializes it.** The resolved address is written into
`endpoints.<slot>.address` during normalization, so validators, renderers, and
`render effective` all read one resolved value — the repo's rule that a default
consumed by more than one pipeline stage is materialized by normalize, not
recomputed per stage. Which slots normalize filled is recorded in
`DefaultedRefs`, so the "no authored `address`" refusal can tell an operator's
address from its own.

## Consequences

- A single-node cluster now declares its endpoints in three lines that cannot
  disagree with the machine, instead of three addresses that could. The
  duplication the docs warned about is gone, and so is the warning.
- `render effective` shows the resolved address on a `node` slot. Re-authoring
  that rendered output as input would trip the "address must be empty" rule —
  effective state is an output, not authored source of truth, and the freshness
  comparison that does read it back compares normalized state to normalized
  state, so it is unaffected.
- The `openshift`-source refusal for single-node clusters stays. `node` is the
  positive form of the same fact; it does not relax the negative one.
- Multi-node clusters are untouched. No existing input changes meaning, and the
  `external` and `infraComponent` sources keep the semantics they had.
