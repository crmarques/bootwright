---
name: architecture-best-practices
description: Design or review Bootwright specs and technical guidance, component ownership, ports, dependency reuse, variants, and durable state or failure semantics.
---

# Architecture Best Practices

Use [code-implementation](../code-implementation/SKILL.md) for tracked edits.
[Architecture](../../../specs/architecture.md) owns component boundaries and
dependency policy; [state reconciliation](../../../specs/state-reconciliation.md)
owns lifecycle and durable state. Load the affected contracts from the
[spec index](../../../specs/index.md).

## Place and scope the decision

- Required behavior lives in its owning spec, delivery state and deferred work in
  [milestones](../../../specs/milestones.md), and observed constraints in indexed
  [knowledge](../../knowledge/index.md). Keep one statement and link to it.
- Specs state target invariants. A rule the code does not yet meet links the
  milestone row that will close it; nothing else in a spec claims availability.
- The code owns inventories. Do not hand-copy command lists, package maps or
  interface catalogs into prose; point at the code, or add a test that holds a
  table to it. A rule a tool can check becomes a test, and its prose goes.
- Describe the smallest authorized slice through its observable outcome, owner,
  changed boundary and failure behavior. Add an interface or variation only at
  its first concrete consumer. Follow the
  [dependency selection rule](../../../specs/architecture.md#dependency-selection-and-reuse).

## Review boundaries

- Identify the policy, data, interface and translation owners, and keep Go and
  Ansible responsibilities on their sides of the boundary.
- Define a port's complete request, result, typed failures, cancellation and
  replay semantics before implementing it, and share one implementation of a
  shared protocol rather than copying it per capability.
- Give mutable state one owner. For a durable-format or identity change, resolve
  canonical form, crash boundary, and exact continuation or safe refusal first.

Verify changed invariants with boundary tests, dependency-direction checks,
determinism and failure injection. Report the violated contract, evidence,
impact, smallest correction and verification, and separate confirmed defects
from optional improvements.
