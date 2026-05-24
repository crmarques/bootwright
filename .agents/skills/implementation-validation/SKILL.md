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
- If `make check` cannot run or fails, report the blocker instead of a
  successful handoff.
- If any `.go` file changed, run `go test -race ./...` before finishing.
- Report any validation command that could not be run, including the reason.
