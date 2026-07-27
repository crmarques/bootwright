# Inventory `ansible_ssh_common_args` replaces the cfg string, and outranks nothing

**Symptom:** SSH options that are present in `ansible/ansible.cfg`
`[ssh_connection] ssh_common_args` have no effect on machine hosts — plays
against inventory machines hang with no keepalive teardown, or host-key
verification behaves as `accept-new` although the inventory pins
`StrictHostKeyChecking=yes`.

**Root cause, part one — variables win outright.** Ansible resolves
`ssh_common_args` from its variables source (`ansible_ssh_common_args`) before
the ini config. The two are **not merged**: an inventory entry that sets
`ansible_ssh_common_args` FULLY REPLACES the cfg string for that host. Every
option the cfg line carries — including the `ServerAliveInterval=15` /
`ServerAliveCountMax=3` keepalives from ansible-ssh-liveness-timeouts.md — is
gone for machine hosts unless the inventory string carries it too. That is why
`inventory.SSHCommonArgWords` emits `BatchMode`, `StrictHostKeyChecking=yes`,
`UserKnownHostsFile`, **and** the keepalives; dropping the keepalives there
regresses machine hosts to unbounded hangs while the cfg looks correct.

**Root cause, part two — never demote the pin to `ssh_extra_args`.** Splitting
the host-key options out into `ansible_ssh_extra_args` looks equivalent and is
not: Ansible appends `ssh_common_args` FIRST, then `ssh_extra_args`, and
OpenSSH honours the FIRST occurrence of a repeated option. The cfg's
`StrictHostKeyChecking=accept-new` would arrive first and win, silently
downgrading host-key verification on every machine — a fail-open change that
looks like a refactor.

**Fix / rule:** keep the pinned host-key options in the value the inventory
writes as `ansible_ssh_common_args`, and keep that string self-sufficient
(keepalives included). Pinned by
`TestInventorySSHCommonArgsPinHostKeysAndKeepConnectionsAlive`, which asserts
both the keepalives and the absence of `accept-new`.
