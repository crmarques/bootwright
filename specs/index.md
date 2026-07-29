# Spec Index

Load only the specs needed for the task.

| Task | Specs |
| --- | --- |
| Mission, operating model, kind inventory, UX principles | `domain.md` |
| Pipeline, layers, per-kind ownership boundaries, adapters, orchestration, Ansible, testing | `architecture.md` |
| Desired-state schema, validation rules, CLI contract | `state-model.md` |
| Secrets, credentials, OCP install trust, supply chain | `security.md` |
| Designing or changing behavior in a decided area | `adr/README.md` decision table |

When in doubt, start with `domain.md` and `state-model.md`. Architecture
decisions live under [`adr/`](adr/README.md), which owns how specs and ADRs
relate.
