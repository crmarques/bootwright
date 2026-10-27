# Verification Tiers

This page owns the verification tiers: their names, when each runs, what each
proves and what it costs, so a change pays for the full gate once, on the
commit that integrates into main, and not on every edit. Other guidance names
the commands and links here.

| Tier | Command | Run it | Typical cost |
| --- | --- | --- | --- |
| Probe | the one check that shows the change, below | after every edit | seconds |
| Focus | `make focus` | before each intermediate commit or handoff | 3 to 40 s |
| Quick | `make quick` | a Go change many packages import, before the gate | 40 to 110 s |
| Gate | `make check`, plus `make race` and a review for a [safety change](../SKILL.md#safety-class) | once, before integrating into main | about 6 min |

Costs were measured on a 16-core host with warm caches: focus took 4.8 s for a
leaf Go package against 40 s for quick, 15 s for `api/v1alpha1` against 110 s,
4 s for a spec edit against 60 s, 34 s for a Ceph module utility and its test
against 207 s for the Ansible units and lint suites, and 40 s for a role task
file. The gate's Ansible suites take about 4.7 minutes of its 6, and the units
suite alone about 3.

## Probe

The probe is the empirical check: the narrowest command that exercises the
changed behavior. For a defect, write the regression test first and watch it
fail before the fix; a defect probe that never failed proves nothing.

- One Go test: `./scripts/go test -count=1 -run '^TestName$' ./internal/<package>`.
- One collection test file:
  `./scripts/ansible-check --suite units <path to a test_*.py file>`.
- One role or playbook: `./scripts/ansible-check --suite lint <path>` or
  `--suite syntax <path to a playbook>`.
- The command itself: `make build`, then the changed command on an example it
  affects, such as `bin/bootwright validate --file
  examples/ceph-clusters/libvirt-3-nodes-ceph`, which needs no context.

## Focus

`make focus` derives every check from the branch's diff from main, runs the
Go, Ansible units, other Ansible checks and text lanes at once, stops each lane
at its first failure, and prints the command that reproduces it. It selects as
[architecture verification](../../../../specs/architecture.md#architecture-verification)
describes: Go tests of the changed packages, vet over every affected package so
a broken dependent still fails, the collection tests that import or name a
changed plugin or list the plugins tree, every Ansible check for a change to
the Ansible configuration or locks, and docs-check for guidance.

It never proves what only the gate runs: the dependents' tests, the
architecture suite unless the change selects it, the second vet platform,
completion, vulnerability and module checks, the ansible-test units, the unit
run under the managed-host floor, full sanity and syntax, and the integration
targets. A collection test that reads a role or a plugin as data without
importing or naming it is reached only through the whole-tree run a non-Python
change triggers. Its parallel unit runs settle each failure by a serial rerun;
the gate runs every test serially. A failure that passes only on the serial
rerun is flaky, not a pass, and is recorded as flaky. Each parallel lane is
bounded in time and prints its reproduce command when the bound expires.

Not yet met: the focus driver reports a failure the serial rerun recovered as
passing, and a parallel lane has no timeout, so a worker that deadlocks in a
fork hangs the lane until interrupted; tracked as
[B822](../../../../specs/milestones/backlog.md#b822).

## End-of-turn hook

The Stop hooks of Claude Code (`.claude/settings.json`) and Codex
(`.codex/hooks.json`, the only tracked file under `.codex/`) run
`scripts/hooks/stop-check` with a 300 s timeout. It is advisory: it does nothing
in the primary checkout, in a clean worktree, or while the runtime reports
`stop_hook_active`; otherwise it runs `make focus` at the worktree's top level
and asks for one more turn (exit 2, reason on stderr, stdout empty as Codex
requires) only when that fails. A pass records a key over `HEAD`, the tracked
diff and the untracked files' names and contents in `stop-check.ok` in the
worktree's Git directory, so an unchanged tree is not checked twice; a failure
records its key in `stop-check.failed`, so the same content never blocks twice
even when the runtime omits `stop_hook_active`. A failure the task did not
cause is reported, not fixed. Codex runs project hooks only once the project's
`.codex/` layer and the hook are trusted (`/hooks`), and skips the hook again
after it changes until it is re-trusted.

## Recording

A commit body names the tier commands that ran and their results. An
intermediate commit on a task branch may rest on `make focus`; the commit that
integrates into main rests on `make check`, plus the safety class's additions
when it applies.
