---
paths:
  - "ansible/**"
---
Ansible change: run `make focus` before finishing and `make check` before
integrating. Conventions: [ansible](../../.agents/skills/code-implementation/references/ansible.md).
Go owns policy and Ansible owns bounded effects:
[boundary](../../specs/architecture.md#go-and-ansible-responsibility-boundary).
Destructive paths also follow [security-safety](../../.agents/skills/security-safety/SKILL.md).
