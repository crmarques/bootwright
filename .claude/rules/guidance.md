---
paths:
  - "specs/**"
  - ".agents/**"
  - "docs/**"
  - "*.md"
---
Guidance change: run `make docs-check`. One owner per fact: link to the owning
section, never restate it ([index](../../specs/index.md)). Delivery state goes in
`specs/milestones/`, lessons in `.agents/knowledge/`. A spec names the Go
constant of an internal version, never its number.
Spec or guidance edits also load
[architecture-best-practices](../../.agents/skills/architecture-best-practices/SKILL.md).
