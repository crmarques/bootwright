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

## Adding a built-in add-on package

No generic add-on code names a package; a descriptor declares each fact the
generic code reads. Touch, in order:

1. `api/v1alpha1`: the `spec.packages` member and its Go configuration type;
   its `timeout` schema default equals the descriptor's `Budget`, at most
   `addons.MaxBudget` (`TestPackageTimeoutDefaultsEqualTheirBudgets`). A name
   is at most `addons.MaxPackageNameBytes`.
2. `internal/addons/packages/<name>.go`: the `addons.Package` with `Summary`,
   `Provides` and `Requires`, `Normalize` (defaults and ordering belong here,
   never in `NormalizePackages`), `Admit`, `Render`, and `Consumes` when
   the configuration names a `CephConsumer` (`Render` derives its `Requires`
   from it). An `OLM` operator that approves its own plans sets
   `OperatorManagedReason`; no other package may.
3. `Builtin` in `registry.go`: one entry.
4. A token it provides ahead of its first consumer is added to
   `DefinedCapabilityTokens`, an API edit that
   [specs/api/addons.md](../../../specs/api/addons.md#clusteraddon) defines;
   any other token needs a reader in the registry.
5. Tests: an admission test per rule, a row in `firstPhaseOpensWithCRDGate`,
   the render golden (`TestPackageRenderGoldens`) and the request golden
   (`TestBuiltinPackageRequestGoldens`, fed by `authoredPackageConfig`), a
   row in `TestSchemaMaximalPackagesCompileDecodePlanAndCoverEveryMember`,
   and the `internal/addons/catalog` listing and description tests.
6. Records: its row in the
   [coverage table](../../../specs/add-on-coverage.md#built-in-packages) (the
   parity test holds it), its API section, a
   [knowledge](../../knowledge/index.md) page for any native fact, and the
   milestone item. The request is a contract path: state `Contract:` in the
   commit.
7. Gates: `make focus`, then `make quick` and `make docs-check`.

## Report

List the touch set, any seam added, the digest decision and the gates run.
Name the qualification tier each new destructive path needs: in-tree, emulated
rehearsal or real hardware
([completion and verification](../../../specs/milestones.md#completion-and-verification)).
