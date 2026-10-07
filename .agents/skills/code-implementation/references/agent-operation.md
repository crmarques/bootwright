# Agent Operation

Guidance for running coding agents on this repository. Measure before changing
a default; the sources are the vendors' published prompting guides.

## Model and effort

- Claude Opus 5.5 defaults to `medium` effort, which its prompting guide
  reports matching earlier `high` results on agentic coding. Use `xhigh` or
  `max` only where a comparison on this repository's tasks shows a gain, and set
  effort once per session: changing it invalidates the prompt cache.
- Claude Fable 5.1 defaults to `high`; its guide reports `medium` and `low` as
  cheaper where quality holds. It tends to extend scope and rewrite whole files,
  so keep the scope rule of AGENTS.md and the targeted-edit rule explicit.
- Codex on GPT-6 Astra: this repository has not measured its behavior; compare
  it on a small task set before routing safety-class work to it.
- To reduce thinking, lower the effort instead of adding prose to prompts.

## Model routing

Correctness comes first: a cheaper model is chosen only where it produces the
same result as a stronger one, and when that is in doubt the stronger model
stays on the task or a stronger reviewer checks the result. Every subagent and
every `agent()` call in a workflow script sets its model and effort
explicitly; a worker never inherits the orchestrating session's model.

| Tier | Model | Effort | Tasks |
| --- | --- | --- | --- |
| Mechanical | Haiku | low | File discovery, search, listings, extraction, simple classification, repetitive transformations, basic validation, result summaries |
| Implementation | Sonnet | high | Coding, tests, straightforward fixes, refactors with clear requirements, documentation, bounded reviews (reproduce a finding, check a spec clause), integration and gate runs, delivery records |
| Reasoning | Opus | medium | Planning and briefs, difficult debugging, cross-cutting refactors, architecture, ambiguous requirements, security, privilege, durability or concurrency reasoning, digest-moving changes, reviewing Sonnet's work, reachability judgments, synthesis across agents |
| Exceptional | Fable | medium | Only after Opus failed on the same problem, or final arbitration when agents disagree |

Workflow rules: classify each task before spawning it; let the planning agent
tag each work item `routine` or `complex` and route its implementer by the
tag, where `complex` means a safety refusal, the privilege boundary, durable
store transitions, concurrency, a frozen request or digest, an Ansible role or
plugin, or several packages under ambiguous requirements; prefer many Sonnet
workers plus one Opus reviewer over Opus workers; start a fix for a blocking
or security finding on Opus and a minor one on Sonnet; escalate one tier after
a failed attempt; when a stronger voter calls a finding real and blocking and
cheaper voters disagree, add one more strong vote rather than let the cheaper
votes win; keep Opus for planning, finders, synthesis and verification.
A brief written for a Sonnet implementer leaves nothing to infer: exact
anchors, exact text or behavior, and the tests with their assertions.

## Reviews and unattended runs

- An independent review of a safety-class diff flags only gaps that affect
  correctness or the stated requirements; broader findings drive
  over-engineering and belong in the backlog.
- In an unattended run, keep the task's parts in a checklist and treat a turn
  that ends with text as a report, not as completion.
- Agents never run `bootwright apply` or `destroy`, or Ansible playbooks against
  hosts, outside tests; the committed Claude Code settings deny those commands.
