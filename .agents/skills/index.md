# Skill Index

Use [code-implementation](code-implementation/SKILL.md) for every tracked
change. Add domain skills when their decisions or boundaries are affected;
load language references only as directed by that workflow. Skills are
canonical here, where Codex discovers them; `.claude/skills/` holds thin
wrappers with the same frontmatter so Claude Code discovers them too. Edit the
canonical file, and give a new skill its wrapper in the same change: the docs
gate fails on a skill without a wrapper, a wrapper without a skill and a
wrapper whose frontmatter differs.

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
`AGENTS.md`. The tracked `.claude/settings.json` and
`.codex/rules/bootwright.rules` ask before every command AGENTS.md forbids an
agent to run on its own, and
`TestDocsAskRulesCoverEveryEffectfulCommand` keeps both complete against the
[command catalog](../../specs/cli/commands.md#command-and-flag-catalog). Codex
reads `<repo>/.codex/rules/` only once the project's `.codex/` layer is trusted;
a session that does not trust it copies the file to `~/.codex/rules/`. A bypass
permission mode skips both. A Codex prefix rule matches only the leading words:
a global flag between the binary and the command, an absolute binary path and
`git -C <dir>` do not match, and a flag-gated command (`cluster info`,
`render effective`) prompts on every run; `.claude/settings.json` matches
those forms. Both runtimes run the same advisory
[end-of-turn check](code-implementation/references/verification.md#end-of-turn-hook):
`.claude/settings.json` and `.codex/hooks.json` call `scripts/hooks/stop-check`.
