# Agent Guidance

Bootwright coordinates declarative day-0 platform bootstrap through qualified
native tools and adapters. Start with [specs/index.md](specs/index.md) and load
only the relevant contracts. Read [milestones](specs/milestones.md) for current
delivery scope and search [knowledge](.agents/knowledge/index.md) for observed
constraints.

Specs own required behavior; milestones own delivery state and deferred work;
knowledge owns lessons. Keep each fact in one place and link to it. Implement
only the slice the prompt authorizes: the current milestone's open work, or an
explicitly requested out-of-sequence slice. With no open milestone, only an
explicit request authorizes a tracked edit. Record other discovered work in the
earliest fitting future milestone. Update the owning spec with any intentional
contract change; add future detail when a requested feature needs it.

For every tracked edit, follow
[code-implementation](.agents/skills/code-implementation/SKILL.md), the single
owner of verification, commits, linear integration, and task-created Git
cleanup. Use [the skill index](.agents/skills/index.md) for domain guidance.
Inspect the worktree before editing and preserve unrelated work. Repository
guidance never authorizes a push, release, or additional product scope.
