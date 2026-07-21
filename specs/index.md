# Spec Index

Load only the specs needed for the task.

| Task | Specs |
| --- | --- |
| Mission, operating model, UX principles | `domain.md` |
| Pipeline, kinds, adapters, orchestration, Ansible, testing | `architecture.md` |
| Desired-state schema, validation rules, CLI contract | `state-model.md` |
| Secrets, credentials, OCP install trust, supply chain | `security.md` |
| Designing or changing behavior in a decided area | `adr/README.md` decision table |

When in doubt, start with `domain.md` and `state-model.md`. The current
desired-state API is defined in `state-model.md`. Architecture decisions live
under `adr/`, which keeps only current decisions — superseded ADRs are deleted
once the surviving decision and specs carry their context; specs carry the
current contract when a previous decision has been revised by implementation.
