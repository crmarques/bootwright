---
name: extend-implementation
description: Add or change a Bootwright implementation behind an existing capability, such as a substrate arm, distribution, storage or service implementation, add-on driver, or version specialization.
---

# Extend An Implementation

Use [code-implementation](../code-implementation/SKILL.md) for the change and
[architecture-best-practices](../architecture-best-practices/SKILL.md) for the
mechanism choice. The general flow must not learn which implementation runs.

## 1. Choose the mechanism

Choose a typed variant, an implementation or a version specialization as
[architecture-best-practices](../architecture-best-practices/SKILL.md#choose-the-variation-mechanism)
says, and state the choice and why.

## 2. Find the touch set before editing

- A new capability follows
  [adding a lifecycle capability](../../../specs/architecture.md#adding-a-lifecycle-capability).
- A new implementation of an existing capability touches its package, its
  registry entry, composition and playbook bindings, its role, the domain's
  support statement and its contract suite. Until architecture names this
  touch set, read it from the family's existing implementation.
- List every other file the change needs. A change in another domain's general
  code means a seam is missing: add the seam as its own reviewed step first, in
  the same slice, and say so in the commit body.

## 3. Keep specifics behind the seam

- Consumers dispatch through the capability's port or role entry points, never
  on an arm or implementation name; add no arm literal to application code.
- A version specialization follows
  [adding a specialization](../../knowledge/version-specializations.md#adding-a-specialization):
  a record with id, seam, variant and a condition over facts from frozen
  artifacts, and its catalog row. It never keys on a release label, encodes no
  version pairing (D163), and no seam sits in trust, host-key, signature,
  redaction or credential code.

## 4. Contract and digest

Declare the implementation's identity, content identity and execution
dependencies. A change to what an effect is moves the content digest, as the
[Go reference](../code-implementation/references/go.md#contract-versions-and-content-digests)
says.

## 5. Prove generality

- Run the family's shared port or contract suite against the new
  implementation; a new lifecycle binding joins
  `cmd/bootwright/capability_contract_test.go`.
- Where the family has a second implementation or arm, show that this one
  needed no edit to the general flow.
- Update the observable-field table when the implementation projects state.

## Report

List the touch set, any seam added, the digest decision and the gates run.
Name the qualification tier each new destructive path needs: in-tree, emulated
rehearsal or real hardware
([completion and verification](../../../specs/milestones.md#completion-and-verification)).
