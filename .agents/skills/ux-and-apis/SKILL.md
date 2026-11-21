---
name: ux-and-apis
description: Design or review Bootwright public schemas, API kinds and fields, CLI journeys, help, diagnostics, human and machine output, examples, and compatibility.
---

# UX And APIs

Use [code-implementation](../code-implementation/SKILL.md) for tracked edits.
[CLI](../../../specs/cli.md), the [command catalog](../../../specs/cli/commands.md)
and [output](../../../specs/cli/output.md) own command behavior;
[API](../../../specs/api.md) and the owning kind page own schemas;
[state reconciliation](../../../specs/state-reconciliation.md) owns lifecycle
journeys.

## Rules this repository has already broken once

- The command catalog in `internal/cli` and the catalog spec agree; documented
  command lines are checked by `TestDocsCommandLinesMatchTheCatalog`.
- JSON mode writes exactly one document to stdout; progress and log locations
  never reach it. Machine output uses CLI-owned, tagged presentation types.
- Every emitted diagnostic code appears in the output taxonomy, and a refusal
  names the object, the reason and the next step.
- `validate` rejects a value the only implementation refuses; no default always
  refuses at plan time.
- A declared flag is read; an unimplemented one is refused, never ignored.
- A runbook lives in its e2e case, `e2e/<case>/runbook.md`, and runs as
  written against the current build.

## Design and review

Start from the operator's outcome and walk discovery, input, success, mistakes,
failure and recovery, for a person and for automation. Add the smallest surface
the authorized slice needs. For each input resolve grammar, omission, null,
empty and zero, defaults and precedence; for each result resolve streams, exit
code, ordering and which bytes are contractual. Review compatibility of accepted
input and emitted output together, and update specs, examples and executable
tests in the same change. Use goldens for contractual bytes and semantic
assertions otherwise.

## Changing a kind or field

1. Read the [grammar](../../../specs/api.md#public-authoring-grammar),
   [defaults and effective state](../../../specs/api.md#defaults-normalization-and-effective-state)
   and the owning kind page section, then the semantic owner section it links.
2. Resolve omission, null, empty and zero, the default and its precedence, and
   whether the field is shared intent or implementation-shaped
   ([native fields](../../../specs/api.md#native-and-implementation-shaped-fields)).
3. Make the field truthful: every admitted value is realized, refused at
   `validate` or plan with a named diagnostic, or reported with the
   `api.deferred` warning; one that plan refuses until an item realizes it is
   a row of the [admitted but unrealized](../../../specs/api.md#admitted-but-unrealized)
   table. Never admit a field that plan preserves but ignores.
4. Change the type in `api/v1alpha1`, the kind page and its semantic owner
   together, and decide whether adding the field moves any frozen request.
5. Update the effective-output goldens golden-first, and every tracked example
   the change makes invalid or newly expressible; keep e2e cases runnable.
6. A field that reaches a frozen request follows the contract check in the
   [Go reference](../code-implementation/references/go.md#contract-versions-and-content-digests).
7. Name in the report the downstream input charts the item lists; never edit
   another repository in this change.
