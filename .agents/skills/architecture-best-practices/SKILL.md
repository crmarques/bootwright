---
name: architecture-best-practices
description: Design or review Bootwright specs and technical guidance, component ownership, ports, dependency reuse, variants, and durable state or failure semantics.
---

# Architecture Best Practices

Use [code-implementation](../code-implementation/SKILL.md) for tracked edits.
[Architecture](../../../specs/architecture.md) owns component boundaries and
dependency policy; [state reconciliation](../../../specs/state-reconciliation.md)
owns lifecycle and durable state. Load the affected domain contracts from the
[spec index](../../../specs/index.md).

## Place and scope the decision

- Keep required behavior in its owning spec, delivery scope and deferred work
  in [milestones](../../../specs/milestones.md), and observed constraints in
  indexed [knowledge](../../knowledge/index.md). Retain one authoritative
  statement and link to it. Preserve rationale only when it changes a future
  decision; omit generic tutorials and speculative detail.
- Describe the smallest complete authorized slice through its observable
  outcome, semantic owner, changed boundary, and failure behavior. Introduce
  interfaces or variation only at their first concrete consumer. Accepted
  future schema is not a claim of implemented behavior.
- Follow the [dependency selection rule](../../../specs/architecture.md#dependency-selection-and-reuse)
  when choosing libraries or tools.

## Review boundaries

- Identify policy, data, interface, and translation owners. Keep domain
  vocabulary independent of vendor types and application sequencing separate
  from adapter effects. Check that Go/Ansible responsibilities follow the
  architecture contract, including who authorizes and records an operation.
- Define the port's complete request, result/evidence, typed failures,
  cancellation, and replay semantics before adding an implementation. Reuse
  a port only when every implementation preserves that contract.
- Give mutable state one owner. For a durable-format or implementation-identity
  change, resolve canonical form, compatibility, crash boundary, and exact
  continuation or safe refusal before changing storage.
- Compare alternatives when they affect maintained code, safety, operational
  cost, or future variation. Keep necessary rationale with the owning contract.

Verify changed invariants with boundary tests, dependency-direction checks,
determinism, and failure injection as applicable. For reviews, report the
violated contract, evidence, impact, smallest correction, and verification;
distinguish confirmed defects from optional improvements.
