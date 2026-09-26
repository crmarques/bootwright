---
paths:
  - "**/*.go"
  - "go.mod"
  - "go.sum"
---
Go change: run `make quick` before finishing, always through `./scripts/go`
(it pins the toolchain). Conventions: [go](../../.agents/skills/code-implementation/references/go.md)
and [formatting](../../.agents/skills/code-implementation/references/formatting.md).
Placement and dependency rules: [architecture](../../specs/architecture.md#go-package-structure),
enforced by `test/architecture`. Find the owning spec in [the index](../../specs/index.md).
