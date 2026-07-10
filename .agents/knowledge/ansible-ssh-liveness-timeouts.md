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
