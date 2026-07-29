# Project Specs

This directory holds the project's source-of-truth definitions: the
contracts, boundaries, and decisions that govern the codebase. Specs are
written for **both humans and coding agents**.

- Humans use specs to understand intent, accepted boundaries, and the API
  contract before reading code or contributing changes.
- Agents load only the specs needed for the current task. Use `index.md`
  to decide which to load. Do not bulk-load every spec unless the task
  spans the whole repository.

## Source-Of-Truth Rules

- Specs describe intent, constraints, and accepted boundaries.
- Architecture decisions live under `adr/`; see [`adr/README.md`](adr/README.md)
  for the decision table and the ADR-retirement policy.
- Human usage guides live in `/docs/` and reference specs for definitive
  detail; they must not duplicate spec content.
- Implementation code must conform to these specs or update the specs in
  the same change.

## Layout

`index.md` describes what each spec covers; this tree only lists them.

```text
specs/
├── README.md
├── index.md
├── domain.md
├── architecture.md
├── state-model.md
├── security.md
└── adr/
```
