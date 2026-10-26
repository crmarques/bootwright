# Skill Index

Use [code-implementation](code-implementation/SKILL.md) for every tracked
change. Add domain skills when their decisions or boundaries are affected;
load language references only as directed by that workflow. Skills are
canonical here, where Codex discovers them; `.claude/skills/` holds thin
wrappers with the same frontmatter so Claude Code discovers them too. Edit the
canonical file, and give a new skill its wrapper in the same change: the docs
gate keeps each wrapper's frontmatter in step, but does not yet fail on a
skill without a wrapper or a wrapper without a skill
([B822](../../specs/milestones/backlog.md#b822)).

| Work | Skill |
| --- | --- |
| Tiered verification, the safety class, commits, and local or pull-request integration | [code-implementation](code-implementation/SKILL.md) |
| Specifications, guidance, component ownership, ports, variation mechanism, state, or dependencies | [architecture-best-practices](architecture-best-practices/SKILL.md) |
| Schemas, kinds and fields, CLI, diagnostics, output, or compatibility | [ux-and-apis](ux-and-apis/SKILL.md) |
| Trust, secrets, I/O, remote operations, privilege, or supply chain | [security-safety](security-safety/SKILL.md) |
| A new or changed implementation behind an existing capability, or a version specialization (provisional) | [extend-implementation](extend-implementation/SKILL.md) |
| Milestone items, Not yet met lines, decisions, delivered records, or knowledge pages (provisional) | [milestones-and-knowledge](milestones-and-knowledge/SKILL.md) |

A provisional skill stays only while a measured comparison shows that it loads
on its task and no other and improves the result over a reference linked from
an existing skill; otherwise its content moves to that reference.

## Runtime parity

[AGENTS.md](../../AGENTS.md) is the root instruction file of both runtimes;
`CLAUDE.md` only imports it. A personal or local override never replaces it:
use `~/.claude/CLAUDE.md` or `CLAUDE.local.md` for Claude Code and
`~/.codex/AGENTS.md` for Codex, or make the local file defer to the tracked
`AGENTS.md`. The tracked `.claude/settings.json` asks before every command
AGENTS.md forbids an agent to run on its own, and `TestDocsAskRulesCoverEveryEffectfulCommand` keeps that list complete against the [command catalog](../../specs/cli/commands.md#command-and-flag-catalog); a bypass permission mode skips
those rules, and Codex needs the same commands under its own approval policy,
which this repository does not track. Both runtimes run the same advisory
[end-of-turn check](code-implementation/references/verification.md#end-of-turn-hook):
`.claude/settings.json` and `.codex/hooks.json` call `scripts/hooks/stop-check`.
