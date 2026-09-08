# Specification Index

Start with [project intent](project.md). Read [milestones](milestones.md) for the
current implementation slice, then load only the contracts needed for the task.
A specified capability is not necessarily implemented or authorized.

| Question | Owner |
| --- | --- |
| Product purpose, scope and non-goals | [Project](project.md) |
| Domain boundaries, Go/Ansible responsibilities and dependency policy | [Architecture](architecture.md) |
| Self-explanatory code, minimal comments and retained implementation knowledge | [Code clarity](architecture.md#self-explanatory-code-and-retained-knowledge), [knowledge catalog](../.agents/knowledge/index.md) |
| Desired-state grammar, compilation and kind schemas | [API](api.md), with field tables under `api/` |
| Command behavior, invocation and lifecycle journeys | [CLI](cli.md), [command catalog](cli/commands.md), [output](cli/output.md) |
| Named contexts, immutable input, current selection and durable publication | [Contexts](contexts.md) |
| Apply, destroy, continuation, ownership and GitOps readiness | [State reconciliation](state-reconciliation.md) |
| Trust, secrets, filesystem, process, network and supply-chain boundaries | [Security](security.md) |
| Declarative add-on packages and driver contract | [Add-ons](add-ons.md) |

Each requirement has one owner; other pages link to it. Keep specs focused on
observable behavior and invariants. Add detail with its first requested feature
or a demonstrated ambiguity; avoid speculative fields, mechanisms and duplicate
checklists. Milestones own delivery state and deferred work, skills own the
working procedure, and knowledge records observed lessons.
