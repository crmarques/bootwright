---
name: ux-and-apis
description: Design or review Bootwright public schemas, CLI journeys, help, diagnostics, human and machine output, examples, and compatibility.
---

# UX And APIs

Use [code-implementation](../code-implementation/SKILL.md) for tracked edits.
Load [CLI](../../../specs/cli.md) for command behavior,
[API](../../../specs/api.md) and the owning kind page for schemas,
[state reconciliation](../../../specs/state-reconciliation.md) for lifecycle
journeys, and [security](../../../specs/security.md) for sensitive surfaces.

- Start with the user's outcome and trace discovery, input, success, mistakes,
  failure, and recovery for an operator and an automation consumer. Add the
  smallest complete surface needed by the authorized slice; avoid speculative
  flags, fields, aliases, and success-producing placeholders.
- For each changed input, resolve grammar, cardinality, omission/null/empty/zero
  semantics, defaults, precedence, and provenance. Check every source of
  ambient context. Reuse established vocabulary and preserve declarative
  intent rather than exposing adapter controls.
- For each changed result, resolve stdout/stderr, machine envelope, exit code,
  ordering, and safe diagnostic content. Identify which bytes are canonical
  and which presentation may evolve. Ensure progress cannot corrupt machine
  output and that recovery guidance matches actual operation state.
- Check the selected framework's defaults against the public catalog: implicit
  commands and flags, inheritance, aliases, suggestions, completion, sorting,
  usage-on-error, and TTY styling can expose unintended behavior.
- Review compatibility in both directions: accepted input and emitted output.
  Optional fields, enum additions, defaults, warnings, and stricter validation
  can break strict consumers. Apply the owning version or migration policy;
  update affected specs, examples, and executable tests together.

Exercise changed journeys through the command runner, including invalid input,
help, stream separation, exit codes, cancellation, and promised effect bounds.
Use goldens for contractual bytes and semantic assertions otherwise. Use
property, round-trip, or fuzz tests for broad grammars when examples are
insufficient. Keep examples minimal, valid, synthetic, and accurate about
implementation availability. Contract conformance alone does not prove that
the journey is understandable; walk it through to recovery.
