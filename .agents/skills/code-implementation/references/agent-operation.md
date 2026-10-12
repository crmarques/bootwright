# Agent Operation

Guidance for running coding agents on this repository. Correctness and quality
come first; the model and effort of each task are chosen explicitly, and a
cheaper route is taken only where it produces the same result as a stronger one.

## Quality and cost

- Define acceptance criteria before routing: required behavior, repository
  contracts, relevant gates and review of correctness, safety, scope and
  maintainability. Passing tests alone does not establish equal quality.
- A cheaper model is chosen only where it would produce the same result as a
  stronger one. When that equivalence is in doubt, keep the stronger model on
  the task or add a stronger reviewer; never accept a weaker result to save
  tokens.
- Compare total accepted-task cost: input, cached input, output and reasoning
  usage, service tier, retries and review. Per-token price alone does not
  establish the cheaper route. Keep prompts focused and reuse stable context.
- Check the active runtime's available model IDs, supported effort values and
  account access before selecting them. If a desired model is unavailable,
  use an available candidate that meets the quality requirement, or report the
  capability gap. Do not silently substitute a weaker model.

## Model and effort

Run on a model's default effort unless a measured comparison on this
repository's tasks justifies more. Use `xhigh`, `max` or `ultra` only when
supported and justified by the task; ultra may involve parallel agents rather
than only more reasoning. Set effort once per session: changing it invalidates
the prompt cache. To reduce thinking, lower the supported effort instead of
adding prose to prompts. Do not assume another model's effort has the same
effect.

Fable tends to extend scope and rewrite whole files, so keep the scope rule of
[AGENTS.md](../../../../AGENTS.md) and the targeted-edit rule explicit in its
brief.

## Model routing

Every subagent and every `agent()` call in a workflow script sets its model and
effort explicitly; a worker never inherits the orchestrating session's model.
The orchestrating session itself runs on Opus; Fable is not needed to
orchestrate.

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
a failed attempt (then Opus at high effort, then Fable); when a stronger voter
calls a finding real and blocking and cheaper voters disagree, add one more
strong vote rather than let the cheaper votes win; keep Opus for planning,
finders, synthesis and verification. A brief written for a Sonnet implementer
leaves nothing to infer: exact anchors, exact text or behavior, and the tests
with their assertions.

### OpenAI runtime (unqualified on this repository)

This mapping is separate from the Anthropic table and is unqualified on this
repository: its behavior has not been measured here, so compare a candidate on
a small task set before routing safety-class work to it, and treat the rows as
candidates, not as equal in quality to the Anthropic tiers.

| Task class | OpenAI runtime candidates |
| --- | --- |
| Mechanical | `gpt-6-luna` |
| Implementation | `gpt-6.1-sol`; `gpt-6-sol` or `gpt-5.6-sol` when available |
| Reasoning | `gpt-6-astra`; `gpt-6.1-sol` when a comparison supports it |
| Exceptional | `gpt-6-astra` with higher supported effort or an independent reviewer |

[Codex model guidance](https://learn.chatgpt.com/docs/models) and
[model selection](https://developers.openai.com/api/docs/guides/model-selection)
identify GPT-6 Luna for focused repeatable work, GPT-6.1 Sol for complex coding
at lower cost than Astra, and GPT-6 Astra for the most demanding work. The IDs
are runtime candidates; access depends on the client, account and rollout.
The active tool's supported settings take precedence over an API example;
[API reasoning effort](https://developers.openai.com/api/docs/guides/reasoning)
and Codex defaults can differ. Check [Codex pricing](https://learn.chatgpt.com/docs/pricing)
for subscription usage and credits, and [API pricing](https://developers.openai.com/api/docs/pricing)
for API-key runs. API prices do not estimate included subscription tasks.
Revalidate against the live catalog and official sources when routing rather
than freezing price numbers or availability in repository guidance.

## Reviews and unattended runs

- An independent review of a safety-class diff flags only gaps that affect
  correctness or the stated requirements; broader findings drive
  over-engineering and belong in the backlog.
- In an unattended run, keep the task's parts in a checklist and treat a turn
  that ends with text as a report, not as completion.
- Agents never run `bootwright apply` or `destroy`, or Ansible playbooks against
  hosts, outside tests; the committed Claude Code settings request approval for
  those commands.
