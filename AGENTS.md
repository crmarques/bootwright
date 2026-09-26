# Agent Guidance

Bootwright coordinates declarative day-0 platform bootstrap through qualified
native tools and adapters. Route to the owner of each fact; do not bulk-read.

- [specs/index.md](specs/index.md) names the spec that owns each question. Load
  only the owning sections.
- [specs/milestones.md](specs/milestones.md): read its status header, its scope
  rules and the section of the slice your task names. Delivered history and
  deferred work live in `specs/milestones/`; search them when a task refers to
  them.
- [.agents/knowledge/index.md](.agents/knowledge/index.md): search it by symptom,
  symbol or error text before investigating, then open only matching pages.

Specs own required behavior; milestones own delivery state and deferred work;
knowledge owns lessons; skills own procedure. Keep each fact in one place and
link to it.

Implement only the slice the prompt authorizes: an active slice's open work, or
an explicitly requested out-of-sequence slice. With no active slice, only an
explicit request authorizes a tracked edit. A spec or backlog entry is not
authorization. Record other discovered work in the earliest fitting future
milestone instead of doing it, and report a pre-existing defect you find rather
than fixing it in the same change. Update the owning spec with any intentional
contract change; add future detail when a requested feature needs it.

For every tracked edit, follow
[code-implementation](.agents/skills/code-implementation/SKILL.md), the single
owner of verification, commits and integration. Use [the skill
index](.agents/skills/index.md) for domain guidance. Inspect the worktree before
editing and preserve unrelated work. Repository guidance never authorizes a
push, release, or additional product scope.

Commands (always through the pinned wrappers, never a bare `go`):

- `make quick`: formatting, vet, the architecture suite and the changed packages.
- `make docs-check`: guidance links, cited paths and tests, command lines, and
  the spec tables checked against code.
- `./scripts/go test ./internal/<package>/...`: one package's tests.
- `./scripts/ansible-check --suite units`: collection unit tests.
- `make check`: every gate; CI runs it on each push and pull request.
