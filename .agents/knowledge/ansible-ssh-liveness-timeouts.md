# Ansible play hangs forever on a dead or half-dead SSH host

**Symptom:** An apply or destroy play sits indefinitely on one task with no
error and no timeout. The target host is mid-reboot, powered off after the
play started, or accepts the TCP connection / prints the SSH banner and then
stalls (half-dead box, wedged sshd). Ansible's fork for that host never
returns, so the whole play appears frozen.

**Root cause:** With stock OpenSSH client settings there is no bound on
either phase of a connection: a dead host can hold the TCP/auth handshake
open for the kernel's default (minutes), and a peer that completes the
handshake and then stops responding is never torn down at all. Ansible's
`timeout = 30` in `[defaults]` bounds the connection plugin's own wait, but
the underlying ssh process can still hang past it on a banner-stalled peer.

**Fix:** The shipped `ansible/ansible.cfg` pins liveness flags in
`[ssh_connection] ssh_common_args`:

```text
-o ConnectTimeout=15 -o ServerAliveInterval=15 -o ServerAliveCountMax=3
```

`ConnectTimeout` bounds the TCP/auth handshake so a dead host fails its fork
in fixed time. `ServerAliveInterval`/`ServerAliveCountMax` detect a peer that
completes the handshake and then stalls, tearing the connection down after
~45s instead of blocking the fork indefinitely. Keep these flags when
touching `ssh_common_args` (the same line carries
`StrictHostKeyChecking=accept-new`); removing them regresses to unbounded
hangs that look like a frozen apply.

This cfg line does NOT reach hosts whose inventory entry sets
`ansible_ssh_common_args` — that variable fully replaces it, so the inventory
string must carry the keepalives itself. See
ansible-ssh-common-args-precedence.md.

## The frozen task is the NEXT one, not the last one printed

**Symptom:** a play stops producing output partway down a task's per-item
`ok:` lines, or right after the last host reported one, and no `TASK [...]`
banner ever follows. The natural reading — "it hung in the task whose name is
on screen" — is wrong, and it sends the diagnosis to the wrong file.

**Root cause:** `ansible/ansible.cfg` sets `display_skipped_hosts = False`.
Ansible's `default` stdout callback prints the banner at task start ONLY when
both `display_skipped_hosts` and `display_ok_hosts` are on; otherwise
`_task_start` just caches the name and `_print_task_banner` is deferred until
that task produces its first *displayed* result. A task that has not returned
for any host has therefore printed nothing at all — no banner, no host lines.
So the hung task is the first one AFTER the last banner on screen that is not
skipped for every host, which for a seed-only or delegated task can be several
task definitions further down the file.

**How to read it:** take the last banner, then walk forward through the
imported task files applying each `when:` to the hosts in the play; the first
task that would run is the one that is hung. Confirm from the other end with
`ps` on the target (`pgrep -af cephadm`, `podman ps`) — the remote command is
still running there.

## An unbounded remote command hangs a play the SSH keepalives cannot save

The `ssh_common_args` above bound the *connection*, not the *command*. A
remote process that keeps running keeps the channel alive, so
`ServerAliveInterval` sees a healthy peer and Ansible waits forever — there is
no per-task timeout in Ansible to fall back on. Any task invoking a command
that can block indefinitely on network or quorum must carry its own bound.

`cephadm shell -- ceph <cmd>` is the recurring offender in this repo, and it
blocks two independent ways: `cephadm shell` pulls its container image when it
cannot infer one from a local cluster (unbounded behind a proxy or a registry
that black-holes), and the `ceph` CLI retries mon connections forever when the
configuration names mons that are gone. Both fire on the SECOND destroy of a
cluster, where the first run removed the local state that made the first run
fast. Wrap such probes in `timeout <n>` (see
`phases/bootstrap_steps/apply_mode.yml` and
`destroy_steps/cluster_gate.yml`) and, where the probe can only ever answer
when local cluster state exists, gate it on that state as well so the common
case does not pay for the container at all.
