# Implementation Validation Skill

Use this skill before completing implementation work. For definition-only
changes, also run the checks from `definition-stewardship`.

## Load First

- `/specs/architecture.md` (Testing section)
- `/docs/contributing/building-and-testing.md` — what each gate runs, the
  guard-test regime, and the `/tmp`-tmpfs `GOTMPDIR` trap

## Required Sequence

- During investigation or iterative fixes, run the smallest direct targeted
  command that answers the current question instead of an aggregate target.
- Immediately before executing every `make check-fast` or user-requested `make
  check`, check whether local `main` has advanced since the temporary branch
  was created or last rebased. If it has, rebase the temporary branch onto
  current local `main` and make any needed fixes or adjustments first. Perform
  this check for every aggregate validation run, even when a previous run
  already found the branch current.
- Run `make check-fast` from the temporary worktree once the branch is current,
  in the background, and spend the wait on the tail work below rather than
  idling. Do not run `make check` by yourself; run it only when the user
  explicitly requests that full gate.
- Immediately before every task commit on the temporary branch, check again
  whether local `main` advanced and rebase first if needed. If that rebase
  changes the effective tree after validation, rerun the required validation
  sequence before committing.
- If local `main` advances again before merge (including while `make
  check-fast` was running), repeat the rebase-first sequence — rebase and fix,
  then rerun `make check-fast` — until the branch is both current and green, or
  a real blocker remains.
- What each gate runs is tabulated in
  `docs/contributing/building-and-testing.md` ("Check gates"); the full `go test
  ./...` suite dominates `check-fast`'s cost and is its main verification signal.
- If `make check-fast` cannot run or fails, report the blocker instead of a
  successful handoff.
- Report any validation command that could not be run, including the reason.
- When validating behavior by running `bootwright`, use the repo-built binary
  (`make build`, then `./bin/bootwright`), not whatever is on `PATH`: the
  installed binary lags `main`, and its strict loader rejects newer schema
  (a stale binary fails on inputs that use fields it does not yet know, such
  as a `required` marker), producing a false negative that is not a real
  defect.

## Overlap The Gate With The Tail Work

`make check-fast` costs minutes. Serializing the knowledge, ADR, and docs
writing after it doubles the tail of every task for no added confidence, and
restarting the whole gate for each late prose edit is worse. Overlap them:

- **Freeze the compiled surface first.** Launch only once every edit to Go,
  Ansible, `api/`, `examples/`, `scripts/`, and the `Containerfile` is final —
  those are what the gate builds, renders, and lints.
- **Launch it in the background** (`make -C <worktree> check-fast`, backgrounded)
  after the rebase check above, and keep working while it runs.
- **Spend the wait on the tail work**: `.agents/knowledge/` entries, ADRs under
  `specs/adr/`, spec and `docs/` prose, and drafting the handoff. Write each
  gated artifact together with its index in the same step — knowledge file plus
  its `KNOWLEDGE.md` row, ADR plus its `specs/adr/README.md` row, docs page plus
  its `mkdocs.yml` nav entry — so a guard test never reads a half-written pair.
- **Leave the compiled surface alone while the run is live.** An edit that lands
  mid-run is outside that run's verdict and can fail a guard test against a tree
  that no longer exists. Such a failure is inconclusive, not a finding: settle it
  with the re-verification below before chasing it.
- **Re-verify only what landed after the launch**, scoped to what it touched:

  ```text
  .agents/ prose only:            go test ./internal/repo/checks/...

  plus docs/, specs/, README.md:  go test ./internal/repo/checks/... \
                                    ./internal/state/desired/... ./internal/cli/...
                                  make stale-term-check

  anything else:                  make check-fast again, in full
  ```

  Those three packages hold every test that reads authored prose — the repo
  guard tests, the docs-snippet validator, and the authorization-contract test —
  and `stale-term-check` is the only non-Go stage that scans it; nothing else in
  `go test ./...` can have changed. Extend that list in the same change whenever
  a task introduces another test that reads prose from the repo tree.
- Report the change verified only once both the background run and the scoped
  re-verification are green. A green background run does not cover the prose
  written while it was running.

## Inherited Failures

A red `check-fast` is not automatically your change's fault, and an unrelated
failure must neither be fixed on the task branch nor park the finished task.
Classify it before acting:

- **Prove it on the merge base.** Rerun the exact failing target or test package
  in the primary `main` worktree — a read-only use, since the tests do not mutate
  the tree. A failure that reproduces there, on a file the change never touched,
  is inherited.
- **Everything else is yours.** A failure that passes on `main`, or that names a
  file in the change's own edit set, was caused by the change: fix it on the
  branch before integrating. Classify by reproduction, never by plausibility — a
  guard test naming an unfamiliar package is not evidence of inheritance.
- **Recover the coverage the abort skipped.** `make` stops at the first failing
  prerequisite, so an inherited failure in an early stage (`shellcheck-check`,
  say) means the `go test ./...` stage never ran at all. Run the skipped stages
  directly so the change's own surface is still verified; when that cannot be
  done, the change is unverified and the handoff reports a blocker instead.
- **Record it, then leave it alone.** Add a `.agents/knowledge/BACKLOG.md` row
  naming the failing target, the symptom, and the commit it reproduces on.
  Fixing it on the task branch widens the diff, couples two unrelated reviews,
  and re-opens validation for both.
- **Integrate the task** once every other safety gate holds, then **open a fresh
  cycle for the inherited failure** in the same turn: new branch and worktree off
  the updated `main`, the failure as its own task, its own `check-fast`, its own
  integration and handoff line.
