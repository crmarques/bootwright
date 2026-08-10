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

**Count the host lines before walking forward.** The same deferral means a
task that has returned for SOME hosts prints its banner and only those hosts'
lines, and is still running for the rest — so a banner is not evidence the
task finished. Where the host lines are short of the play's host count, the
frozen task IS the one named on screen, on whichever host is missing from the
list, and walking forward sends the diagnosis to a task that has not started.
Measured 2026-08-07 on ceph-prd-01: `Prove the storage node container runtime
can start a Ceph container` showed six `ok:` lines in a seven-node play and
node-04 was still inside its `podman run` 31 minutes later. Take the host
count from the inventory, never from the longest task on screen.

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
case does not pay for the container at all. The same bound is required for
single-shot mutations and tool calls: any `cephadm shell` can stall in image
resolution or cluster connection before its child command makes progress.

A bare `podman run` is the same class and fires on the FIRST apply, not only
on a second destroy: the container-runtime gate proves the pinned Ceph image
with `podman run --entrypoint stat`, which pulls that image when the node does
not already carry it. Measured 2026-08-07 on ceph-prd-01 node-04, a stalled
pull of `cp.icr.io/cp/ibm-ceph/ceph-9-rhel9` held the play for 31 minutes with
no output, no failure and a live SSH channel throughout; the pull only ended
when the process was killed by hand.

Coreutils `timeout` is the right wrapper for both because, absent
`--foreground`, it puts the child in a NEW process group and signals the whole
group. That matters more than the bound itself: the podman a wrapped `cephadm`
forked dies with it instead of surviving to hold the command module's stdout
pipe open, which would leave the task hung after the bound had already fired.
Pair it with `--kill-after=<n>` so a child that ignores SIGTERM still takes
SIGKILL. `TestAnsibleBoundsCommandsThatCanHangForever` in
`internal/repo/checks` enforces both for every `podman run` and every
`cephadm rm-cluster` argv in the collection.

The managed Ceph role classifies every `cephadm shell` argv instead of applying
one blanket ceiling:

| Class | Default | Examples |
| --- | ---: | --- |
| probe | 120s | health, monmap, config and idempotency reads |
| inventory probe | 300s | `ceph orch device ls --refresh` |
| configuration mutation | 300s | config/config-key writes, registry and SSH setup |
| orchestration | 600s | spec apply, redeploy, reconfigure, host enrollment |
| removal | 1800s | pool, filesystem, profile, daemon, host and device removal |
| tool round-trip | 300s | `crushtool` and interpreter probes inside the container |
| operation batch | 1800s | one staged batch under a fixed finite ceiling |

All classes use a 15s kill escalation. The role defaults live in
`roles/storage_cluster_cephadm/defaults/main.yml`; the vars contract owns their
meaning. The role rejects non-positive classes before its first command, caps a
batch at 1800s, and constrains kill escalation to 1–60s so an Ansible-precedence
override cannot disable the wrapper.
`TestAnsibleBoundsEveryCephadmShellCommand` recursively walks tasks, blocks,
loops, and playbooks; rejects free-form, unclassified, or unbounded argv; and
follows role/task include boundaries and enclosing `no_log`, `ignore_errors`,
and rescue controls as well as task-local failure, retry, and loop controls that
could hide rc 124/137. A `no_log` command must use the shared fail-closed relay;
it emits only the task name, bound, exit code, and read/write classification,
never protected command output. The command-count floor keeps a scanner
regression from silently shrinking coverage.

The bound turns an invisible stall into rc 124, or rc 137 when the kill
escalation is surfaced. Every task fails immediately on either code; a loop or
retry stops before its next mutation or observation. Ordinary probe absence
may still reach a later evidence gate, but a timed-out probe cannot and never
infers a mutating remedy. For a state-changing timeout the runner extracts the
exact resolved `bootwright_mutating_invocation` fact for its retry message; it
never rebuilds a command from task names or treats the timeout as success,
absence, ownership, or authorization evidence.
