# Agent Operation

Guidance for running coding agents on this repository. Choose the model and
reasoning effort that best fit each task; preserve the highest final code
quality and optimize cost only among choices that deliver equal quality.

## Quality and cost

- Define acceptance criteria before routing: required behavior, repository
  contracts, relevant gates and review of correctness, safety, scope and
  maintainability. Passing tests alone does not establish equal quality.
- Compare candidates on representative repository tasks with the same brief,
  context and checks. A cheaper model or lower effort qualifies only when its
  result is as correct, complete and maintainable as the stronger candidate's.
  Record the comparison and recheck when the model, task class or runtime
  changes; a vendor's tier or benchmark does not prove repository equivalence.
- When equivalence is uncertain, use the strongest suitable available model
  for the task or have it review and correct the cheaper worker's result.
  Delivery requires the quality checks to pass and material review findings
  to be resolved; a stronger review does not automatically qualify a draft.
- Compare total accepted-task cost: input, cached input, output and reasoning
  usage, service tier, retries and review. Per-token price alone does not
  establish the cheaper route. Keep prompts focused and reuse stable context.
- Check the active runtime's available model IDs, supported effort values and
  account access before selecting them. If a desired model is unavailable,
  use an available candidate that meets the quality requirement, or report the
  capability gap. Do not silently substitute an unqualified cheaper model.

## Model and effort

Effort is model- and runtime-specific. Start at the client's supported default
or a repository-qualified setting, then compare lower effort for bounded work
and higher effort for difficult reasoning. Increase effort only when it improves
quality; use `xhigh`, `max` or `ultra` only when supported and justified by the
task. Ultra may involve parallel agents rather than only more reasoning.
To reduce thinking, lower supported effort instead of adding prose to prompts.

For Anthropic, retain Haiku for mechanical work, Sonnet for implementation,
Opus for demanding reasoning and review, and Fable as an exceptional candidate.
These are starting candidates, not guarantees of equal quality or permanent
price rankings. Existing starting efforts are `low` for Haiku, `high` for
Sonnet and `medium` for Opus or Fable where the active client supports them.
Confirm the current model, effort, pricing and cache behavior before changing a
default; do not assume another model's effort has the same effect.

For OpenAI, [Codex model guidance](https://learn.chatgpt.com/docs/models) and
[model selection](https://developers.openai.com/api/docs/guides/model-selection)
identify GPT-6 Luna for focused repeatable work, GPT-6.1 Sol for complex coding
at lower cost than Astra, and GPT-6 Astra for the most demanding work. Compare
these candidates on repository tasks before treating their quality as equal.
The active tool's supported settings take precedence over an API example;
[API reasoning effort](https://developers.openai.com/api/docs/guides/reasoning)
and Codex defaults can differ. Check [Codex pricing](https://learn.chatgpt.com/docs/pricing)
for subscription usage and credits, and [API pricing](https://developers.openai.com/api/docs/pricing)
for API-key runs. API prices do not estimate included subscription tasks.

## Model routing

Classify each work item before selecting its worker and reviewer. The rows
below are candidates for comparison, not a required escalation ladder or a
claim that models from different providers are equivalent.

| Task class | Tasks | Anthropic candidates | OpenAI runtime candidates |
| --- | --- | --- | --- |
| Mechanical | Discovery, extraction, classification, repetitive transformations, basic validation, summaries | Haiku | `gpt-6-luna` |
| Implementation | Coding, tests, bounded fixes and refactors, documentation, integration and gates | Sonnet | `gpt-6.1-sol`; `gpt-6-sol` or `gpt-5.6-sol` when available and qualified |
| Reasoning | Planning, difficult debugging, architecture, ambiguous cross-package work, safety, durability, concurrency, final review and synthesis | Opus | `gpt-6-astra`; `gpt-6.1-sol` when the quality comparison supports it |
| Exceptional | A failed qualified route or unresolved material disagreement | Opus or Fable with appropriate effort | `gpt-6-astra` with higher supported effort or an independent qualified reviewer |

The OpenAI IDs above are runtime candidates; access depends on the client,
account and rollout. Older Sol models require their own comparison; neither
availability nor lower total cost is assumed. Revalidate against the live
catalog and official sources when routing rather than freezing price numbers
or availability in repository guidance.

Every delegated worker and every `agent()` call in a workflow script has an
intentional model and effort choice. Set them explicitly when supported; if
forked context requires inheritance, use a separate supported launch for a
qualified cheaper worker or document the inherited choice. Respect the tool's
launch constraints rather than assuming a model override is available.

Tag work `routine` or `complex`; complex includes safety refusals, privilege
boundaries, durable store transitions, concurrency, frozen requests or digests,
Ansible roles or plugins, and ambiguous changes across packages. Use a
qualified implementation worker for routine work and a qualified reasoning
worker for complex work. Mechanical discovery or gate execution can use a
cheaper qualified worker; interpreting safety or architectural findings needs
a qualified reviewer. Give implementers exact anchors, behavior and tests
with assertions, without broadening authorized scope.

After a failed attempt or a material finding, reassess the brief and evidence
and escalate model capability or effort when needed. Resolve reviewer
disagreements with reproduction, contract evidence and another qualified
review, not vote counts or the number of cheaper agents. Keep demanding
planning, synthesis and final verification on the strongest suitable route
unless an equal-quality cheaper route has been demonstrated. Prefer fewer
necessary agents; parallelism is useful only when it preserves quality and
reduces total accepted-task cost or latency.

## Reviews and unattended runs

- An independent review of a safety-class diff flags only gaps that affect
  correctness or the stated requirements; broader findings drive
  over-engineering and belong in the backlog.
- In an unattended run, keep the task's parts in a checklist and treat a turn
  that ends with text as a report, not as completion.
- Agents never run `bootwright apply` or `destroy`, or Ansible playbooks against
  hosts, outside tests; the committed Claude Code settings request approval for
  those commands.
