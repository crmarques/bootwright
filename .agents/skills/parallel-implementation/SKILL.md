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
- Use the primary worktree only for read-only inspection and, when it is clean,
  final fast-forward integration into `main`.
- Do not stash, reset, force-update, or commit unrelated user changes.

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
- The coordinating agent reviews, integrates, resolves conflicts, and runs the
  final validation for the combined result.

## Completion

- Run all task validation from the temporary worktree.
- Commit the task changes on the temporary branch.
- If local `main` advanced after `base_sha`, rebase the temporary branch onto
  current local `main`.
- Rerun required validation when rebase changes the effective final tree.
- If the primary `main` worktree was dirty at task start, stop after validation
  and the temporary branch commit. Report a blocked handoff for manual
  integration.
- If the primary `main` worktree was clean at task start and remains clean at
  integration time, fast-forward `main` to the temporary branch.
- After successful integration, remove the temporary worktree and delete the
  temporary branch.
- If `main` is dirty at integration time, or rebase or fast-forward conflicts
  occur, keep the temporary worktree and report the blocker.
