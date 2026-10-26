# Agent Operation

Vendor-neutral guidance for running coding agents on this repository.
Correctness and quality come first; the model and effort of each task are
chosen explicitly, and a cheaper route is taken only where it produces the same
result as a stronger one. Model names, tiers, effort tables and staffing
preferences belong in each runtime's user-level configuration, not here.

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
- Run on a model's default effort unless a measured comparison on this
  repository's tasks justifies more. Set effort once per session: changing it
  invalidates the prompt cache.

## Routing

Every subagent and every worker of a workflow script sets its model and effort
explicitly; a worker never inherits the orchestrating session's model. Classify
each work item `routine` or `complex` before routing it, where `complex` means
a safety refusal, the privilege boundary, durable store transitions,
concurrency, a frozen request or digest, an Ansible role or plugin, or several
packages under ambiguous requirements. A brief for an implementer leaves
nothing to infer: exact anchors, exact text or behavior, and the tests with
their assertions. Keep the scope rule of [AGENTS.md](../../../../AGENTS.md) and
the targeted-edit rule explicit in every brief.

## Reviews and unattended runs

- A [safety-class](../SKILL.md#safety-class) diff gets an independent review
  by a model at least as strong as its implementer's. The review flags only
  gaps that affect correctness or the stated requirements; broader findings
  drive over-engineering and belong in the backlog.
- A review verdict that decides a disagreement cites evidence: a contract
  clause, a reproduction or a failing test. Model rank alone is not a verdict.
- In an unattended run, keep the task's parts in a checklist and treat a turn
  that ends with text as a report, not as completion.
