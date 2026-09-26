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

## Reviews and unattended runs

- An independent review of a safety-class diff flags only gaps that affect
  correctness or the stated requirements; broader findings drive
  over-engineering and belong in the backlog.
- In an unattended run, keep the task's parts in a checklist and treat a turn
  that ends with text as a report, not as completion.
- Agents never run `bootwright apply` or `destroy`, or Ansible playbooks against
  hosts, outside tests; the committed Claude Code settings deny those commands.
