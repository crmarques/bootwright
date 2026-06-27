# Repo Stewardship Skill

Use this skill when changing repository structure, generated-output
boundaries, tests, Ansible organization, or security hygiene. Use
`definition-stewardship` first for specs, docs, ADRs, examples, and agent
guidance.

## Load First

- `/specs/architecture.md` (Layers and Testing sections)
- `/specs/security.md`
- The Repository Layout section in `/README.md`

## Guidance

- Keep generated files out of source directories.
- Add tests proportional to the behavior being introduced.
- Do not commit secrets or local runtime state.
- Update specs when repository boundaries change.
