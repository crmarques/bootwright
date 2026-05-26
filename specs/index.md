# Spec Index

Load only the specs needed for the task.

| Task | Specs |
| --- | --- |
| Mission, operating model, UX principles | `domain.md` |
| Pipeline, kinds, adapters, orchestration, Ansible, testing | `architecture.md` |
| Desired-state schema, validation rules, CLI contract | `state-model.md` |
| Secrets, credentials, OCP install trust, supply chain | `security.md` |

When in doubt, start with `domain.md` and `state-model.md`. The current
desired-state API is defined in `state-model.md`. Historical
architecture decisions live under `adr/`; specs carry the current
contract when a previous decision has been revised by implementation.
