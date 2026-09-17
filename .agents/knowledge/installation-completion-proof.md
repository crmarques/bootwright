# Proving a managed installation finished

Diagnosed 2026-09-17 on `lab-rhel`, after three aborted applies of the same
machine. [Managed OS](../../specs/managed-os.md#completion) owns the required
behavior; this page records why the implementation did not meet it and what a
progress row has to say for an operator to trust it.

## Symptom

`apply` sits on one row for minutes:

```
[RUNNING]  [8/8] install the operating system of rhel-01: prove the installation comple...  still running, 8m12s
```

The machine shows a login prompt, so the run looks wedged and gets interrupted.
The next `apply` continues, resolves the block from an observation in **6
seconds**, and reports `state: done`.

## Three defects, in the order they cost time

**The row named a check while it performed a wait.** `apply.yml` reported the
`verify-installation` group as running and then entered
`identity_until_answered.yml`, which polls the guest agent
`identity_attempts: 120` times at `identity_delay: 30` — a **60-minute**
budget. A second 60-minute wait (`retries: 180`, `delay: 20`, for the installer
to power the machine off) sat inside `boot-installer`, whose description is
"insert the image and boot the machine from it". Both long waits were hidden
behind labels that read like assertions, so a healthy install was
indistinguishable from a hang. The waits now have their own groups,
`await-installation` and `await-machine`, and `boot.yml` was split so each
group brackets the wait it names.

**The SSH proof could never run.** `verify.yml` built its `known_hosts` from
`managedos_install_anaconda_host_key.content`, but that fact is a **string**:
`identity_read.yml` publishes `... key_read.content | default('') | trim`, and
both `apply.yml` and `observe.yml` already used it as a string. Dereferencing
`.content` on it raises, so the one task that proves the fleet account can log
in never completed. It went unnoticed because the observation path does not run
it, so only that path ever completed an installation.

**An observation resolved a completed apply on weaker evidence.** `Observe` and
the apply share `ValidatePresence`, but `observe.yml` gathered no reachability
fact, so the shared rule had nothing to check: an interrupted apply resolved to
`done` having proved the marker, the host key and the Redfish power state, and
never that the machine was usable. Evidence now carries `reachable`, the
protocol's postcondition requires it, and `observe.yml` runs the same probe with
a single-attempt budget. A machine holding the marker that has not answered yet
validates as **partial**, so the verb converges instead of refusing — sshd
finishes starting after the identity channel already answers.

## The rules

- A presentation group's description names what the step does while it holds
  the row. A bounded wait says it is waiting, because elapsed time alone cannot
  distinguish it from a hang.
- An observation that resolves a verb proves what the verb proves. Sharing the
  validator is not enough — the observation has to gather every fact the
  validator reads, or the rule silently passes.
- A censored task still owes a reason. The probe keeps `no_log`, because its
  command line names the fleet identity; the refusal beside it names the
  account, the address, the budget and the ssh exit status, none of which is
  secret. Relates to
  [operation-store-entry-confirmation.md](operation-store-entry-confirmation.md),
  which is the same failure of a refusal that discards its own cause.

**How to apply:** `until:` with `failed_when: false` survives exhaustion (see
the retry note in that role), so the give-up path must be an explicit task that
reads the registered `rc`. Keep the `or (<reg>.attempts | ...) > <retries>`
escape — it suppresses the misleading `[ERROR]` block and makes the give-up
explicit.
