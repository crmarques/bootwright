# Implementation Validation Skill

Use this skill before completing implementation work. For definition-only
changes, also run the checks from `definition-stewardship`.

## Load First

- `/specs/architecture.md` (Testing section)

## Required Sequence

- During investigation or iterative fixes, run the smallest direct targeted
  command that answers the current question instead of an aggregate target.
- After completing the intended edit set for any implementation request that
  changes code, run basic targeted validations first. Examples include
  formatting, focused unit tests, syntax checks, and for definition-only
  changes, checks required by `definition-stewardship`.
- Before running `make check`, refresh or rebase the temporary worktree against
  current local `main`. If the refresh changes the effective final tree,
  perform needed fixes and rerun the affected basic validations.
- Run `make check` once as the last validation command before handoff.
- Do not run `make check` repeatedly during the same request unless later edits
  can invalidate the previous result. If post-`make check` fixes are needed,
  repeat the sequence: basic targeted validation, current-`main` refresh when
  needed, then one final `make check`.
- Treat a completed `make check` as covering its member commands for the final
  edit set; do not rerun those member commands unless later edits or failure
  diagnosis require it.
- Current `make check` runs `go vet ./...`, plain `go test ./...`,
  `go test -race ./...`, clean-copy plain `go test ./...`, Python tests,
  Ansible syntax checks, stale-term checks, and CLI file-size checks.
- If `make check` cannot run or fails, report the blocker instead of a
  successful handoff.
- Report any validation command that could not be run, including the reason.
