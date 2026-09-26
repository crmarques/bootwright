---
paths:
  - "ansible/**"
---
Ansible change: run `./scripts/ansible-check --suite units` and `--suite lint`
before finishing. Conventions: [ansible](../../.agents/skills/code-implementation/references/ansible.md).
Go owns policy and Ansible owns bounded effects:
[boundary](../../specs/architecture.md#go-and-ansible-responsibility-boundary).
Destructive paths also follow [security-safety](../../.agents/skills/security-safety/SKILL.md).
