# Implementation Validation Skill

Use this skill before completing implementation work. For definition-only
changes, also run the checks from `definition-stewardship`.

## Load First

- `/specs/architecture.md` (Testing section)

## Required Checks

- After completing the intended edit set for any implementation request that
  changes repo-tracked files, run `make check` once before handoff.
- Do not run `make check` repeatedly during the same request unless later edits
  can invalidate the previous result.
- During investigation or iterative fixes, run the smallest direct targeted
  command that answers the current question instead of an aggregate target.
- Treat a completed `make check` as covering its member commands for the final
  edit set; do not rerun those member commands unless later edits or failure
  diagnosis require it.
- Current `make check` runs `go vet ./...`, plain `go test ./...`,
  `go test -race ./...`, clean-copy plain `go test ./...`, Python tests,
  Ansible syntax checks, stale-term checks, and CLI file-size checks.
- If `make check` cannot run or fails, report the blocker instead of a
  successful handoff.
- Report any validation command that could not be run, including the reason.
