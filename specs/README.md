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
- Architecture decisions live under `adr/`. Only current decisions are kept;
  superseded ADR files are removed once the surviving decision and specs carry
  the needed context.
- Human usage guides live in `/docs/` and reference specs for definitive
  detail; they must not duplicate spec content.
- Implementation code must conform to these specs or update the specs in
  the same change.
- Avoid architecture that only works for the initial SNO plus bare-metal
  lab.

## Layout

```text
specs/
├── README.md          This file
├── index.md           Spec selection guide for a given task
├── domain.md          Mission, operating model, UX principles
├── architecture.md    Pipeline, layers, adapters, Ansible, testing
├── state-model.md     Desired-state schema, validation, CLI contract
├── security.md        Secrets, OCP install trust, supply chain
└── adr/               Architecture Decision Records
```
