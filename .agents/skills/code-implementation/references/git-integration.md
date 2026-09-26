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

## Integration variants

- **Pull request (default).** Push the task branch when the user or harness asks,
  open or update the pull request, and keep CI green. Later fixes are new
  commits on the same branch; do not rewrite pushed history.
- **Cloud session.** The harness-designated branch is the task branch and its
  instructions decide when to push.
- **Local, no remote.** Rebase the task branch onto the destination tip, rerun
  the affected gates, and advance the destination by fast-forward only. If the
  destination has unrelated uncommitted changes or cannot move safely, keep the
  tested task branch and report the blocker; never bypass it with a merge.

Amend only a commit you created for this same change that has not been pushed
or shared and that no unrelated commit follows; otherwise make a new commit.
Squash temporary task history to the minimum coherent commits before
integration, and check the range with `git diff --check` and its log.

## Cleanup

After the final commit is integrated and its gates pass there, remove each clean
task-created worktree and then its branch, after confirming the final commit
represents its content. Leave unrelated Git state untouched.
