# Implementation Worktree Skill

Use this skill for every implementation request that changes repo-tracked files.
Work must happen in a temporary branch and worktree, never directly in the primary
`main` worktree.

## Load First

- `/.agents/skills/implementation-validation/SKILL.md` — owns the rebase-first →
  `make check-fast` loop this skill defers to.
- Any task-specific project skills for the requested change.

## Before Editing

- Record whether the primary `main` worktree was dirty at task start:

  ```text
  git status --porcelain --untracked-files=all
  ```

- Create an isolated temporary branch and worktree from local `main`
  (preauthorized — do not ask):

  ```text
  base_sha=$(git rev-parse main)
  branch="work/<task-slug>-<base8>-<timestamp>"
  worktree="/tmp/bootwright-worktrees/<task-slug>-<base8>-<timestamp>"
  git worktree add -b "$branch" "$worktree" main
  ```

- Implement only inside the worktree. Use the primary `main` worktree for
  read-only inspection until integration.
- Never stash, reset, force-update, or commit unrelated user changes.
- A shell's working directory does not reliably persist into the temporary
  worktree between commands; drive tools with explicit `-C` flags
  (`go -C`, `git -C`, `make -C`) or per-command absolute paths, or tests can
  pass vacuously against the wrong tree.
- `/tmp/bootwright-worktrees` does not survive a reboot or session loss:
  commit every coherent edit set on the temporary branch as you go (the
  branch lives in the main repository), and re-verify the worktree path
  exists before resuming work in a later session.

## Parallel Workers

Before executing a task list that fulfills the request, check whether it splits
into independent, disjoint-scope pieces (different files/packages/specs, no
shared edit surface, no ordering dependency). If so, default to parallelizing
rather than working the list serially — the goal is wall-clock time to
completion, not just eventual correctness.

- Give each independent piece its own `work/<scope-slug>-<base8>-<timestamp>`
  branch and matching worktree, and work them concurrently.
- The coordinating agent reviews, combines worker output, resolves conflicts,
  and runs the final validation once for the combined result — do not run
  `make check-fast` per worker branch when a single combined run will do.
- Do not over-split: more branches means more rebase/merge/conflict-resolution
  overhead at integration. If the pieces touch overlapping files, are too small
  to justify a separate branch, or coordination would cost more wall-clock time
  than finishing the same work serially, keep them on one branch (or fold small
  pieces together) instead of forcing parallelism.

## Validate And Commit

- Run focused commands during implementation. After the intended edit set,
  follow `implementation-validation`: immediately before every `make
  check-fast` or user-requested `make check`, check for a `main` advance and
  rebase first when needed.
- Once `make check-fast` passes, repeat the `main` advance check immediately
  before committing task changes. Rebase first when needed; when that changes
  the effective tree, rerun validation before committing. Task commits and
  commits of rebase fixes are preauthorized — do not ask. Author commits as the
  human only — no agent co-author or attribution trailer (see AGENTS.md
  "Handoff Format").
- Once `make check-fast` passes and the branch is rebased current, integration is
  preauthorized — do not ask. Proceed to "Integrate" below. The only reasons to
  leave the change on the branch without merging are a failed safety gate or an
  explicit user request to hold it for review/testing.

## Integrate

When `make check-fast` is green and the branch is current, merging is
preauthorized — the final rebase, fast-forward, worktree removal, and branch
deletion need no separate approval.

- Rebase the temporary branch onto current local `main` if it advanced; rerun
  `make check-fast` when the rebase changes the effective tree.
- Merge only while every safety gate holds: `main` was clean at task start and is
  still clean, `make check-fast` is green, and no rebase or fast-forward conflict
  occurs. When they all hold, fast-forward `main` to the temporary branch, then
  remove the worktree and delete the branch.
- If `main` is dirty at integration time, a rebase or fast-forward conflict occurs,
  or the user asked to hold the change for review, do not touch `main`: keep the
  worktree and report the blocker or held state.
