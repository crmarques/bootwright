# Implementation Worktree Skill

Use this skill for every implementation request that changes repo-tracked
files. Implementation work must happen in a temporary worktree, never directly
in the primary `main` worktree.

## Load First

- `/.agents/skills/implementation-validation/SKILL.md`
- Any task-specific project skills that apply to the requested code change

## Before Editing

- Check the primary `main` worktree with:

  ```text
  git status --porcelain --untracked-files=all
  ```

- Record whether the primary `main` worktree was dirty at task start.
- Always create an isolated temporary branch and worktree from local `main`
  before editing.
- Use the primary worktree only for read-only inspection until the user
  explicitly approves commit or integration after review/testing.
- Do not stash, reset, force-update, or commit unrelated user changes.
- Do not commit task changes, push, or fast-forward `main` immediately after
  implementing a fix. Leave the temporary worktree available for user
  review/testing until the user explicitly approves the next step.

## Isolated Worktree Workflow

- Record the starting point:

  ```text
  base_sha=$(git rev-parse main)
  ```

- Create an agent-neutral branch and worktree:

  ```text
  branch="work/<task-slug>-<base8>-<timestamp>"
  worktree="/tmp/bootwright-worktrees/<task-slug>-<base8>-<timestamp>"
  git worktree add -b "$branch" "$worktree" main
  ```

- Implement only inside the isolated worktree.
- For multiple parallel workers, split disjoint write scopes and assign each
  worker a separate `work/<scope-slug>-<base8>-<timestamp>` branch and matching
  `/tmp/bootwright-worktrees/<scope-slug>-<base8>-<timestamp>` worktree.
- The coordinating agent reviews, combines worker output, resolves conflicts,
  and runs the final validation sequence for the combined result.

## Pre-Handoff Validation

- Run basic targeted validation from the temporary worktree first.
- Before running `make check`, refresh the temporary change set against current
  local `main`.
  - If the task changes are still uncommitted, use a non-committing replay
    flow, such as applying the temporary worktree diff onto a fresh worktree
    created from current `main`.
  - If the user has already approved a commit or the task branch already
    contains task commits, rebase the temporary branch onto current local
    `main`.
- If refresh or rebase changes the effective final tree, perform needed fixes
  and rerun the affected basic validations.
- Run `make check` once as the last validation command before handoff. Do not
  run it earlier in the request.
- Leave changes uncommitted in the temporary worktree for user review/testing
  unless the user has explicitly approved commit or integration after review.
- If the primary `main` worktree was dirty at task start, stop after validation
  and report a blocked handoff for manual integration.

## After User Approval

- Commit the reviewed task changes on the temporary branch.
- If local `main` advanced after the last refresh, rebase the temporary branch
  onto current local `main`.
- Rerun required validation when rebase changes the effective final tree.
- If the primary `main` worktree was clean at task start and remains clean at
  integration time, fast-forward `main` to the temporary branch.
- After successful integration, remove the temporary worktree and delete the
  temporary branch.
- If `main` is dirty at integration time, or rebase or fast-forward conflicts
  occur, keep the temporary worktree and report the blocker.
