# Implementation Validation Skill

Use this skill before completing implementation work. For definition-only
changes, also run the checks from `definition-stewardship`.

## Load First

- `/specs/architecture.md` (Testing section)

## Required Sequence

- During investigation or iterative fixes, run the smallest direct targeted
  command that answers the current question instead of an aggregate target.
- After completing the intended edit set for any implementation request, run
  `make check-fast` from the temporary worktree. Do not run `make check` by
  yourself; run it only when the user explicitly requests that full gate.
- After `make check-fast`, check whether the temporary branch is ready to merge
  into current local `main`.
- If local `main` has advanced or the temporary branch is not ready, rebase the
  temporary branch onto current local `main`, perform needed fixes or
  adjustments, rerun `make check-fast`, and repeat until the branch is ready or
  a real blocker remains.
- Current `make check-fast` runs the cheap local guardrails: CLI file-size,
  Go source visibility, gofmt, stale-term, Containerfile pinning, and E2E
  dependency checks.
- If `make check-fast` cannot run or fails, report the blocker instead of a
  successful handoff.
- Report any validation command that could not be run, including the reason.
