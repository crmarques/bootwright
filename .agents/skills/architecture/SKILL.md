# Architecture Skill

Use this skill when designing or changing behavior related to OpenShift fleet
provisioning, multi-cluster workflows, providers, or lab BMC
emulation. It covers platform behavior design (substrate, provider, BMC, lab
emulation); for internal package boundaries and the Go↔Ansible split use
`/.agents/reusable-prompts/architecture.md` instead. Day-2 fleet publication
(ACM, OpenShift GitOps, package catalogs, KRC/SRC) is owned by a separate
project and is out of scope here.

## Load First

- `/specs/domain.md`
- `/specs/architecture.md`
- `/specs/state-model.md`

## Guidance

- Keep desired state as the user-facing API.
- Keep provider-specific behavior behind adapters.
- Keep lab emulation close to real bare metal protocols.
- Prefer deterministic rendering and idempotent orchestration.
- Record only cross-cutting architecture decisions in `/specs/adr/`; use the
  definition-stewardship skill for docs/spec cleanup.
