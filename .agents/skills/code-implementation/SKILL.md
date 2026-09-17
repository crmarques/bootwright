---
name: code-implementation
description: Implement and deliver any authorized Bootwright tracked change, including specs, skills, guidance, code, and tests, using minimal temporary Git state, dependency-aware parallel work, verification, and consolidated final commits.
---

# Code Implementation

This is the shared delivery workflow for all tracked changes. Unless the user
requests an uncommitted result or integration is unsafe, finish with one
verified final commit per coherent change on the current destination branch
(normally main). Prefer one final commit for the task; split only independently
reviewable outcomes and keep the total to at most three unless the user requests
a different history. Commit after implementation and verification are complete,
then remove task-created branches and worktrees. This does not authorize a push
or release.

## Prepare and divide work

1. Identify the authorized outcome using the owning [specs](../../../specs/index.md),
   [milestone](../../../specs/milestones.md), and relevant
   [knowledge](../../knowledge/index.md). A spec or backlog entry is not
   implementation authorization. Record unrelated discoveries in the earliest
   fitting future milestone.
2. Inspect status, destination history, publication/share evidence, and
   relevant branches and worktrees. Preserve unrelated changes; never stash,
   reset, rewrite, or remove them to make integration convenient.
3. For a new or changed dependency, follow the
   [dependency selection rule](../../../specs/architecture.md#dependency-selection-and-reuse).
4. Load applicable [domain skills](../index.md). For authoritative spec or
   guidance edits, include architecture-best-practices. Use
   [source formatting](references/formatting.md) for Go, YAML, Ansible, front
   matter, and corresponding inline examples; [Go](references/go.md) for Go
   code/modules; [Ansible](references/ansible.md) for Ansible content. Load both
   language references for a Go/Ansible port or shared contract change.
5. Identify useful tasks, their dependencies, ownership, and checks before
   editing. Run independent ready tasks concurrently whenever this reduces
   total execution time; start newly unblocked tasks as soon as their
   prerequisites are integrated. Account for coordination and integration cost.
   Assign one owner to overlapping files, interfaces, and mutable test resources;
   revise ownership or sequence work when overlap prevents safe parallelism.
   Record why work must remain sequential when dependencies, shared resources,
   or coordination cost prevent a time saving.

Use one task-created temporary branch and worktree by default, including for
related code, specs, tests, and skill edits. Reuse it throughout the task rather
than creating branches for phases or follow-up fixes. Start from the inspected
destination; never implement directly on that branch.

Confirm this before the first edit: the tree you are about to change is that
task worktree, not the destination checkout. Delivery removes the worktree it
used, so a session that has just delivered holds no worktree and the next task
begins by creating one. Each of these is such a task: an urgent fix, a one-line
correction, a regression this session introduced, and a follow-up the user asks
for after a delivery. None of them is an exemption. Urgency is the weakest
reason of all, because an unverified edit made directly on the destination is
the one that cannot be reviewed, set aside, or abandoned if it turns out wrong.

Parallel workers may share the task worktree when their assigned files and
mutable resources are disjoint. The coordinator alone stages, commits, rebases,
merges, or changes branches there. Give a worker a separate temporary worktree
only when overlapping edits, incompatible checkouts, or mutable build/test
resources require isolation. Read-only reviewers share an existing checkout.
Record each necessary worktree's base and ownership with non-sensitive names.

Worker handoffs report changed files, checks and concerns. Shared-worktree
workers leave changes uncommitted. For an isolated worker, prefer one complete
handoff patch or commit over prerequisite and follow-up commit chains. Create
an intermediate commit only when it materially enables dependent isolated work
or protects substantial progress; it is temporary history to consolidate at
delivery. Agent parallelism does not authorize concurrent Bootwright lifecycle
execution.

## Implement and verify

- Apply the [repository layout contract](../../../specs/architecture.md#repository-layout)
  when adding or changing code and scripts; review names and placement before
  delivery.
- Keep implementation, specs, examples, and tests for one observable change
  together. Add meaningful automated coverage for changed behavior and
  affected safety invariants; include a regression for a defect when practical.
  Test public contracts and effect boundaries, not incidental implementation.
- Cover relevant failures, limits, determinism, cancellation, replay, and
  partial progress. Close coverage gaps in behavior the change relies on;
  do not expand into unauthorized features to obtain evidence.
- For definition-only changes, check links, consistency, examples, and skill
  metadata. Record future executable evidence in the owning milestone gate;
  do not build deferred behavior or wording-matching tests for a document edit.
- Run focused checks during work and the affected repository/language gates
  before delivery. After rebase or conflict resolution, rerun affected checks;
  run full relevant gates on the final integrated tree. Record exact commands
  and results. Failed, skipped, flaky, unavailable, and unrun gates are not passes.
- Before staging and committing, review the complete prospective diff, tree,
  status, and message for unrelated or sensitive content. Inspect the staged
  result and run `git diff --cached --check` before creating the commit.

## Commit and integrate

After implementation and verification, inspect the destination tip and
relevant range. Record the amend-versus-new decision in the task plan or a
progress update, including same-change, rewrite authorization,
publication/share, and intervening-commit evidence. A handoff, task boundary,
user turn, or prior fast-forward alone does not make related work a new change.

For existing delivery history, amend the latest related commit only when all hold: the work is the same
coherent change; the agent created that commit for this work or the user
authorized rewriting it; it has not been pushed or shared as a dependency; and
no unrelated commit follows it. Otherwise make a new coherent fix commit.
Never rewrite unrelated or published history without authorization. Temporary
task history may be consolidated once dependent workers have finished; their
internal handoffs do not require preserving intermediate commits.

Accept isolated worker patches or commits serially through the coordinator;
rebase only when necessary to integrate them safely. Review the incoming diff
and messages, then run affected checks. Complete fixes and final integrated
verification before creating delivery commits. Squash any temporary history to
the minimum coherent non-merge commits; do not split by worker, package, phase,
or fixes discovered during verification. Honor the user's commit limit and
check the explicit incoming range with
`git diff --check` and inspect its log: fast-forward alone does not prove that
the range has no merge, WIP, or fixup commits.

If the destination moved, rebase onto its current tip and rerun relevant gates.
Advance it by fast-forward, or amend under the rule above. If it has unrelated
uncommitted changes or cannot be updated safely, preserve tested task branches
and report the blocker; never overwrite work or bypass the check with a merge.

Once the final commit is on the destination and checks pass there, remove each
clean task-created worktree, then its branch. Verify its content is represented
by the final commit before deletion; force-delete a task branch only when
consolidation changed ancestry and ordinary deletion refuses for that reason.
Leave unrelated Git state untouched.

Report the outcome, final SHA, checks and limitations, cleanup status, and the
amend-versus-new decision with its evidence. Explain dependency choices and
parallel-work considerations when they materially affect review.
