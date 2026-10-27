---
name: architecture-best-practices
description: Design or review Bootwright specs and technical guidance, component ownership, ports, dependency reuse, the choice between typed variant, implementation and version specialization, and durable state or failure semantics.
---

# Architecture Best Practices

Use [code-implementation](../code-implementation/SKILL.md) for tracked edits.
[Architecture](../../../specs/architecture.md) owns component boundaries and
dependency policy; [state reconciliation](../../../specs/state-reconciliation.md)
owns lifecycle and durable state. The [spec index](../../../specs/index.md) and
its [conventions](../../../specs/index.md#conventions) own placement.

## Place and scope the decision

- Required behavior lives in its owning spec, delivery state and deferred work in
  [milestones](../../../specs/milestones.md), and observed constraints in indexed
  [knowledge](../../knowledge/index.md). Find the owning section through the
  index and read that section, not the page. Search for the rule's other
  statements; keep one and replace each copy with a link.
- Specs state target invariants in the present tense. A rule the code does not
  yet meet carries one Not yet met line in the conventions' form, linking the
  open item that will close it; nothing else in a spec claims availability.
- The code owns inventories. Do not hand-copy command lists, package maps or
  interface catalogs into prose; point at the code, or add a test that holds a
  table to it. A hand-copied inventory or a process rule a tool can check
  becomes a test, and the copy goes; a required behavior stays in its owning
  spec and names the test that holds it. A spec quotes no internal request or
  format version: it names the code symbol that holds it, because a quoted
  literal drifts when the version moves. Literals already quoted give way to a
  code reference when their page is next edited.
- A domain page whose capability has implementations states their selection
  under three headings, adopted on the page's next edit: Selection (the
  selector its implementations claim objects by, as
  [implementation selection](../../../specs/architecture.md#implementation-selection)
  requires), Implementations (each identity and the block families it
  realizes) and Refusal table (each shape no implementation realizes, with its
  reason and remedy).
- Describe the smallest authorized slice through its observable outcome, owner,
  changed boundary and failure behavior. Add an interface or variation only at
  its first concrete consumer. Follow the
  [dependency selection rule](../../../specs/architecture.md#dependency-selection-and-reuse).

## Choose the variation mechanism

[Implementations and version variation](../../../specs/architecture.md#implementations-and-version-variation)
owns the rules.

- Authored intent or a business invariant differs: a typed variant in the
  owning domain.
- Vendor, substrate, transport or release mechanics differ behind one
  capability: an implementation behind the port, selected by the registry.
- One implementation's release mechanics differ: a version specialization
  record at a declared seam, never in trust, credential or ownership code.

Then follow [extend-implementation](../extend-implementation/SKILL.md).

## Review boundaries

- Split components by actor, invariant, volatility and security boundary
  ([mandatory boundaries](../../../specs/architecture.md#mandatory-component-boundaries)).
- Identify the policy, data, interface and translation owners, and keep each
  language on its side of the
  [responsibility boundary](../../../specs/architecture.md#go-and-ansible-responsibility-boundary).
- Define a port's complete request, result, typed failures, cancellation and
  replay semantics before implementing it, and share one implementation of a
  shared protocol rather than copying it per capability.
- Give mutable state one owner. For a durable-format or identity change, resolve
  canonical form, crash boundary, and exact continuation or safe refusal first.

Verify changed invariants with boundary tests, dependency-direction checks,
determinism and failure injection. Report the violated contract, evidence,
impact, smallest correction and verification
([architecture verification](../../../specs/architecture.md#architecture-verification)).
Name the counterevidence checked, such as an existing guard, a refusal or a
recorded decision, and say why it does not settle the finding. Separate
confirmed defects from optional improvements.
