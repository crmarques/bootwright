# Agent Guidance

Route to each fact's owner; do not bulk-read.

- [specs/index.md](specs/index.md) names the spec that owns each question. Load
  only the owning sections.
- [specs/milestones.md](specs/milestones.md): read its Status and Scope rules;
  on the named milestone page read only the opening, the Planned slices row,
  the item's row and its `### B<n>` section, and what they link. Search
  `specs/milestones/` for history and decisions.
- [.agents/knowledge/index.md](.agents/knowledge/index.md): search it by symptom,
  symbol or error text before investigating, then open only matching pages.

Keep versions fixed and add no backward compatibility until the owner changes
the [development policy](specs/project.md#design-priorities-and-non-goals).

Specs own behavior; milestones own delivery and deferred work; knowledge owns
lessons; skills own procedure. Keep one owner per fact and link to it.

Implement only the outcome the [scope rules](specs/milestones.md#scope-rules)
authorize. With no active slice, a tracked edit needs an explicit request. Record
other discovered work as an item of the earliest fitting milestone, or parked,
instead of doing it. Report a pre-existing defect you find; fix one that
blocks the authorized outcome or its gates only in its own commit, after
recording it as an item the Refs line names. Update the owning spec for a
contract change; add future detail only when a requested feature needs it.

Agents never run `ansible-playbook` against a host, or a Bootwright command the
[catalog](specs/cli/commands.md#command-and-flag-catalog) marks as asked, unless
the owner asks in the conversation; `.claude/settings.json` and
`.codex/rules/bootwright.rules` ask before each.

For every tracked edit, follow
[code-implementation](.agents/skills/code-implementation/SKILL.md), the single
owner of verification, commits and integration, and the domain skills of [the
skill index](.agents/skills/index.md). Guidance never authorizes a
push, release, or extra product scope.

Commands (through the pinned wrappers, never a bare `go`):

- `make focus`: checks this branch's diff selects.
- `make quick`: formatting, vet, fitness suites, changed packages.
- `make docs-check`: guidance links, cited paths and spec tables.
- `./scripts/go test ./internal/<package>/...`: one package's tests.
- `./scripts/ansible-check --suite units [PATH...]`: collection unit tests.
- `make check`: every gate; CI runs it on each push.
