---
name: ux-and-apis
description: Design or review Bootwright public schemas, CLI journeys, help, diagnostics, human and machine output, examples, and compatibility.
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
- A runbook in `examples/` runs as written against the current build.

## Design and review

Start from the operator's outcome and walk discovery, input, success, mistakes,
failure and recovery, for a person and for automation. Add the smallest surface
the authorized slice needs. For each input resolve grammar, omission, null,
empty and zero, defaults and precedence; for each result resolve streams, exit
code, ordering and which bytes are contractual. Review compatibility of accepted
input and emitted output together, and update specs, examples and executable
tests in the same change. Use goldens for contractual bytes and semantic
assertions otherwise.
