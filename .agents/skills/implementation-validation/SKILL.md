# Implementation Validation Skill

Use this skill before completing implementation work. For definition-only
changes, also run the checks from `definition-stewardship`.

## Load First

- `/specs/architecture.md` (Testing section)

## Required Sequence

- During investigation or iterative fixes, run the smallest direct targeted
  command that answers the current question instead of an aggregate target.
- Immediately before executing every `make check-fast` or user-requested `make
  check`, check whether local `main` has advanced since the temporary branch
  was created or last rebased. If it has, rebase the temporary branch onto
  current local `main` and make any needed fixes or adjustments first. Perform
  this check for every aggregate validation run, even when a previous run
  already found the branch current.
- Run `make check-fast` from the temporary worktree once the branch is current.
  Do not run `make check` by yourself; run it only when the user explicitly
  requests that full gate.
- Immediately before every task commit on the temporary branch, check again
  whether local `main` advanced and rebase first if needed. If that rebase
  changes the effective tree after validation, rerun the required validation
  sequence before committing.
- If local `main` advances again before merge (including while `make
  check-fast` was running), repeat the rebase-first sequence — rebase and fix,
  then rerun `make check-fast` — until the branch is both current and green, or
  a real blocker remains.
- Current `make check-fast` syncs the embedded ansible bundle (needs
  `ansible-playbook`), runs the cheap local guardrails — CLI file-size, Go source
  visibility, gofmt, stale-term, Containerfile pinning, shellcheck, and E2E
  dependency checks — and then runs the full `go test ./...` unit-test suite,
  which is the dominant cost and the main verification signal.
- If `make check-fast` cannot run or fails, report the blocker instead of a
  successful handoff.
- Report any validation command that could not be run, including the reason.
- When validating behavior by running `bootwright`, use the repo-built binary
  (`make build`, then `./bin/bootwright`), not whatever is on `PATH`: the
  installed binary lags `main`, and its strict loader rejects newer schema
  (a stale binary fails on inputs that use fields it does not yet know, such
  as a `required` marker), producing a false negative that is not a real
  defect.
