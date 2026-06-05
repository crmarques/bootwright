# Implementation Worktree Skill

Use this skill for every implementation request that changes repo-tracked
files. Implementation work must happen in a temporary branch and worktree,
never directly in the primary `main` worktree.

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
  before editing. This is preauthorized; do not ask the user before creating
  the branch or worktree.
- Load the repo information, specs, skills, code, examples, and current
  worktree state needed to complete the request before editing.
- Use the primary worktree only for read-only inspection until the user
  explicitly approves merge after review/testing.
- Do not stash, reset, force-update, or commit unrelated user changes.
- Do not push, merge, or fast-forward `main` immediately after implementing a
  fix. Task commits on the temporary branch are preauthorized after the
  intended edit set is complete; do not ask before creating them. Leave `main`
  integration pending explicit merge approval.

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

## Pre-Handoff Validation And Readiness

- During implementation, run only focused commands that directly answer the
  current question or fix cycle.
- After the intended edit set is complete, run `make check-fast` from the
  temporary worktree. Do not run `make check` by yourself; run it only when the
  user explicitly requests that full gate.
- Commit the task changes on the temporary branch after `make check-fast`
  passes. This is preauthorized; do not ask the user before committing in the
  temporary branch.
- After `make check-fast`, check whether the temporary branch is ready to merge
  into current local `main`.
- If local `main` has advanced or the temporary branch is not ready, rebase the
  temporary branch onto current local `main`. This is preauthorized; do not ask
  the user before rebasing.
- If rebase changes the effective final tree or exposes conflicts, perform
  needed fixes or adjustments, rerun `make check-fast`, and commit those
  adjustments on the temporary branch without asking.
- Repeat the readiness, rebase, fix, `make check-fast`, and temp-branch commit
  loop until the branch is ready to merge or a real blocker remains.
- Leave changes committed on the temporary branch for user review/testing
  unless the user has explicitly approved merge after review.
- If the primary `main` worktree has uncommitted changes when integration is
  considered, keep the temporary worktree and report that `main` is not ready
  instead of touching unrelated user changes.
- Once the branch is ready for `main`, ask the user whether merge can proceed.

## After Merge Approval

- A response such as "go" authorizes final rebase when local `main` advanced,
  merge into `main`, temporary worktree removal, and temporary branch deletion.
  Do not ask separately for those steps.
- If local `main` advanced after the last refresh, rebase the temporary branch
  onto current local `main`.
- Rerun `make check-fast` when rebase changes the effective final tree.
- If the primary `main` worktree was clean at task start and remains clean at
  integration time, fast-forward `main` to the temporary branch.
- After successful integration, remove the temporary worktree and delete the
  temporary branch.
- If `main` is dirty at integration time, or rebase or fast-forward conflicts
  occur, keep the temporary worktree and report the blocker.
