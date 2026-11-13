# Git Integration

[code-implementation](../SKILL.md) owns when to commit and what to verify. This
reference covers the mechanics.

## Worktrees and parallel work

- Create one task branch and worktree from the inspected destination and reuse it
  for the whole task, including specs, tests and skill edits. Confirm before the
  first edit that the tree you change is that worktree. An urgent fix, a one-line
  correction and a follow-up after delivery are tasks like any other.
- Parallel workers may share the task worktree when their files and mutable
  resources are disjoint; the coordinator alone stages, commits, rebases, merges
  or changes branches there. Give a worker its own worktree only when overlapping
  edits or mutable build or test resources require isolation, and accept its
  single handoff commit or patch serially.
- Worktrees share one check cache through `scripts/cache-dir`, so a new worktree
  does not rebuild the Ansible tool environment.
- Agent parallelism never authorizes concurrent Bootwright lifecycle execution.

## Integration reservation

All local sessions use `./scripts/integration-guard status` before a long gate.
It reports the destination tip and whether a rebase is needed. A busy guard
reports the active session's task branch, PID and start time, giving independent
sessions a shared local coordination point without a chat service. Exit 75
means busy, 3 means rebase needed, 1 means blocked, and 0 means ready. Options
precede the action: `--destination BRANCH` selects a destination other than main.
Status is advisory; readiness is checked again while acquiring the reservation.

When busy, continue focused checks or review and retry after the owner finishes;
do not rebase while another session is about to land. When stale, rebase only
your task branch, inspect incoming changes and resolutions, and run `make
focus`. Then reserve again; a disjoint path diff never proves a rebase is
unnecessary. The ancestry check under the reservation decides that. Never
rebase, terminate, clean or remove another session's work to get a turn.

Use `./scripts/integration-guard run -- /bin/sh /tmp/task-land.sh` for the
complete foreground gate-and-integration sequence. Prepare and review that
task-specific script first; the guard executes its argv without shell parsing
and forwards the command's exit status. It runs no gate and changes no Git ref
itself. The script must fail on any failed gate and keep the reservation until
the fast-forward finishes. It performs, in order:

1. `make check` and the safety additions when applicable, on the final rebased
   inputs. No worker edits the shared task worktree during this window.
2. Review the complete prospective diff, tree and message, explicitly stage
   only the task's files, check the staged diff, then create the coherent
   commit(s) recording the actual results. The committed tree equals the
   checked tree; no automatic fix or generated change escapes review.
3. Run `scripts/check-commits "$BOOTWRIGHT_INTEGRATION_BASE" HEAD`; confirm
   the destination still names that exact base and its worktree is still clean;
   then fast-forward that checkout to the task's `git rev-parse HEAD`.

The guard supplies `BOOTWRIGHT_INTEGRATION_BASE` (the reserved destination tip),
`BOOTWRIGHT_INTEGRATION_DESTINATION` (the branch name), and
`BOOTWRIGHT_INTEGRATION_WORKTREE` (its checkout). Compare the base using
`git rev-parse "refs/heads/$BOOTWRIGHT_INTEGRATION_DESTINATION"`, and use
`git -C "$BOOTWRIGHT_INTEGRATION_WORKTREE" merge --ff-only "$task_tip"` for
the final step, where `task_tip` was read in the task worktree. Do not use
`HEAD` as that merge's argument: it resolves in the destination checkout.

The reservation is one kernel lock in the clone's common Git directory, shared
across worktrees and independent of cache overrides. Its owner record stays
local there; the lock, never a PID's age or leftover record, decides busy.
Never unlink a live lock. Interrupting the guard stops the command group before
releasing it, and the foreground command inherits the lock so killing only the
guard cannot free it while that command survives. Keep the script in the
foreground; detached commands and separate clones are outside this protocol.

Git does not enforce this advisory lock. A session using older guidance or an
outside Git writer can still move the destination; the final base and clean-tree
checks remain mandatory. If it moved, keep the branch, rebase and reserve a new
gate run; report the discarded run. A blocked destination retains its changes
and the tested task branch. Pull-request CI keeps its normal full gates.

## Contract line

`scripts/check-commits` refuses a commit whose diff touches a contract path
unless its body carries exactly one `Contract:` line, the author's answer to
[whether the change alters what an effect is](go.md#contract-versions-and-content-digests):
`Contract: unchanged`, or `Contract: <implementation>, ...` naming each
implementation whose contract it moved, whose
`cmd/bootwright/testdata/contracts/<implementation>.golden` the same commit
changes. It also refuses `Contract: unchanged` on a commit that changes a
`content-digest:` line of a contract golden. The contract paths, listed in the
script as `contract_paths`, are the collection's `meta/`, `playbooks/`,
`plugins/` and `roles/` under
`ansible/collections/ansible_collections/bootwright/core/`, and the packages
that define a contract version's implementation, request or evidence type, or
a value a content digest hashes: `internal/addons/instance/`,
`internal/addons/clusterobjects/`, `internal/containercluster/agentinstall/`,
`internal/containercluster/networking/`, `internal/controller/clients/`,
`internal/infrastructureservices/` `artifactserver/`, `managedservice/`,
`mirrorfill/`, `proxy/`, `dnsserver/`, `ntpserver/` and `registry/`, `internal/machine/power/`,
`internal/managedos/installation/`, `internal/storage/ceph/` and
`internal/substrate/`, each with its subdirectories.
`TestCheckCommitsContractPathsCoverEveryImplementation` holds the list to the
implementations the contract goldens pin.

## Breaking changes

A commit that names a moved contract in its `Contract:` line, or changes the
value of a Go store format version (a non-test `...Version = <integer>` line
under `internal/`), makes this build refuse something an earlier build left: an
incomplete operation, a frozen request or a whole store. `scripts/check-commits`
refuses it unless its subject carries the Conventional Commit `!` and its body
a `BREAKING CHANGE: ` footer naming what it refuses and the remedy, such as
finishing or destroying the operation with the earlier build, or a fresh
`controller install` ([pre-1.0 policy](../../../../specs/project.md#design-priorities-and-non-goals)).
`git log --grep '^BREAKING CHANGE:'` lists them for release notes.

## Integration variants

- **Local fast-forward (default).** Under
  [D168](../../../../specs/milestones/backlog.md#d168) a slice that passed
  its checkpoint and is feature complete integrates into local main as soon as
  it is done, without asking. Follow the [integration reservation](#integration-reservation)
  from preflight through fast-forward, keeping
  [D223](../../../../specs/milestones/backlog.md#d223)'s full gate on the
  combined tree. If the destination has unrelated uncommitted changes or
  cannot move safely, keep the tested task branch and report the blocker.
  This applies
  with or without a remote; pushing still waits for the owner.
- **Pull request.** When the owner or harness asks for one, push the task
  branch, open or update the pull request, and keep CI green. Later fixes are
  new commits on the same branch; do not rewrite pushed history.
- **Cloud session.** The harness-designated branch is the task branch and its
  instructions decide when to push.

Amend only a commit you created for this same change that has not been pushed
or shared and that no unrelated commit follows; otherwise make a new commit.
Squash temporary task history to the minimum coherent commits before
integration, and check the range with `git diff --check` and its log. A
squashed commit carries one `Contract:` line merging those of the commits it
replaces: the implementations any of them named, or `unchanged` when none
named one.

## Cleanup

After the final commit is integrated and its gates pass there, remove each clean
task-created worktree and then its branch, after confirming the final commit
represents its content. Leave unrelated Git state untouched.
