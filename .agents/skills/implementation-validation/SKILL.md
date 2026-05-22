# Implementation Validation Skill

Use this skill before completing implementation work. For definition-only
changes, also run the checks from `definition-stewardship`.

## Load First

- `/specs/architecture.md` (Testing section)

## Required Checks

- Run `gofmt -w` across all repository Go files before finishing.
- If any `.go` file changed, run `go test -race ./...` before finishing.
- Report any validation command that could not be run, including the reason.
