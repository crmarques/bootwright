# Implementation Worktree Skill

Use this skill for every implementation request that changes repo-tracked files.
Work must happen in a temporary branch and worktree, never directly in the primary
`main` worktree.

## Load First

- `/.agents/skills/implementation-validation/SKILL.md` — owns the `make
  check-fast` → readiness → rebase loop this skill defers to.
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
  read-only inspection until the user explicitly approves merge.
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

When splitting work across workers, give each a disjoint write scope and its own
`work/<scope-slug>-<base8>-<timestamp>` branch and matching worktree. The
coordinating agent reviews, combines worker output, resolves conflicts, and runs
the final validation for the combined result.

## Validate And Commit

- Run focused commands during implementation. After the intended edit set, follow
  `implementation-validation`: `make check-fast` (never `make check` unless the
  user requests that gate) and the readiness/rebase loop.
- Commit task changes on the temporary branch once `make check-fast` passes, and
  commit any rebase fixes the same way (preauthorized — do not ask). Author commits
  as the human only — no agent co-author or attribution trailer (see AGENTS.md
  "Handoff Format").
- Do not push, merge, or fast-forward `main`. Leave changes committed on the
  temporary branch for review/testing; `main` integration stays pending explicit
  merge approval.
- If the primary `main` worktree is dirty when integration is considered, keep the
  worktree and report that `main` is not ready instead of touching unrelated
  changes.
- Once the branch is ready, ask the user whether merge can proceed.

## After Merge Approval

A response such as "go" authorizes the final rebase, merge, worktree removal, and
branch deletion — do not ask separately for those steps.

- Rebase the temporary branch onto current local `main` if it advanced; rerun
  `make check-fast` when the rebase changes the effective tree.
- If `main` was clean at task start and remains clean at integration, fast-forward
  `main` to the temporary branch, then remove the worktree and delete the branch.
- If `main` is dirty at integration time, or a rebase or fast-forward conflict
  occurs, keep the worktree and report the blocker.
