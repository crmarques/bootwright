# ADR 0010: CLI Gate and Flag Conventions

## Status

Accepted

## Context

The Bootwright context — rendered state, secrets, run history, live
state, and the media and add-ons stores — lives under the root-owned
`/var/lib/bootwright` tree, so most commands re-execute themselves under
sudo. Without a single policy, escalation, flag naming, destructive-
command gating, and human output each drifted per command: a typo could
cost a sudo password prompt before failing, overwrite acknowledgment
varied between `--force` and `--yes` dances, the `--stage` vocabulary
was restated in error text, help, and completion, and frame output was
written from many files. This ADR records the conventions that now
govern all four.

## Decision

### Root-gate argument handling

The root gate classifies the raw argv (`argsNeedLocalRoot`) before cobra
runs, and re-execs under sudo only when the invocation will actually
touch root-owned state:

- Even read-only inspectors escalate — `plan`, `diff`, `status`,
  `secret generate/list/check/encryption`, `machine list/trust`,
  `cluster list/info/kubeconfig`, `media list`, `add-ons list` — because
  the context and the media/add-ons stores live under
  `/var/lib/bootwright`.
- Any invocation cobra would reject stays rootless so the user sees
  cobra's error (including its "Did you mean" suggestion) as themselves,
  never after a doomed sudo password prompt: unknown top-level commands
  and subcommands, a bogus `apply`/`destroy` `--stage`/`--through` value
  (validated against `converge.ApplyStageNames`), a `validate`
  invocation with an unknown flag or stray positional, and any
  `--name`-targeted command missing a non-empty `--name` value
  (`argsHaveNameValue`).
- `destroy` with an omitted `--stage` is a whole-context full destroy
  (or, with `--clusters`, the inferred clusters stage) and must escalate
  BEFORE context resolution — a non-root run would fail to `lstat` the
  root-owned context directory. A bogus explicit `--stage` deliberately
  stays rootless so it fails stage validation before any context read.
- Carve-outs: a context-free `render --input-dir <dir> --output-dir
  <dir>` reads and writes user-owned directories with `{{ secret }}`
  placeholders and must never escalate — the `--input-dir` carve-out
  wins over the `--output-dir` execution-target match, mirroring
  `validate -f`. `secret set` and `context init`/`update` self-escalate
  inside the command (`shouldRunContextRootChild`), so the generic gate
  must not double-escalate them.
- Only `context init`/`use` (and `context delete --purge`) may mutate
  the per-user registry (`argsMayMutateRegistry`); only `apply`, rootful
  destroy targets, and `bastion setup` may use a become password
  (`argsMayUseBecome`).
- `machine rsh`/`exec` and `cluster rsh`/`exec` exec the ssh client as
  root specifically to read the root-owned SSH private key, and escalate
  only when a non-empty `--name` is present.
- A leading global `--context` is stripped for classification only; the
  original args are forwarded verbatim to the sudo child.

### Flag naming coherence

- Shared flag help strings and registrars live in one file
  (`internal/cli/flags.go`); commands reuse them so a flag never carries
  different wording on different commands.
- Command-vocabulary flags derive from single accessors: the
  `--stage`/`--through` values come from `internal/converge`
  (`FamilyStageNames()` = `infra`, `clusters`; `SubPhaseStageNames()` =
  `fabric`, `machines`, `deps`, `base`, `add-ons`) in all three surfaces
  — validation/error text, help, and shell completion. `destroy`
  accepts families only, because a sub-phase has no single destroy
  playbook.
- Targeted resource commands take `--name` rather than positionals
  (`secret delete/show`, `media add/remove`, `add-ons add/delete`,
  `machine`/`cluster` `rsh`/`exec`).

### Destructive-command gating and override remedies

- Destructive paths are fail-closed by default; a marker or drift
  mismatch refuses rather than proceeding.
- Overwriting stored material is acknowledged by a single gate — one
  `--yes` or one interactive `y`, never a separate `--force` plus
  confirmation dance (`media add` replace matches `secret set`).
- `--override` is the uniform remedy that authorizes Bootwright-owned
  destructive re-convergence (managed-OS reinstall of a drifted node,
  Ceph wipe-and-rebuild), and its help text must name that scope.
  Failure summaries preserve the exact remedy command (middle-ellipsis
  shortening keeps the `--override` tail).
- Destructive selection stays unambiguous by reservation: the cluster
  name `artifact-server` is reserved so `destroy --stage infra
  --clusters artifact-server` can only mean the generated artifact
  publication service.

### Output routing

- All human-facing frame output flows through `internal/cli/output`;
  `RenderFrame` is the only frame writer, enforced by
  `TestHumanOutputUsesOutputPackage`.
- One style gate governs color: `NO_COLOR`, `TERM=dumb`, non-TTY, and
  piped output strip it entirely, so piped diff output is a plain
  unified diff.
- Interactive terminals get in-place redraw; piped/CI output gets
  append-only transition lines with no ANSI cursor control.
- Exit codes are contract: 0 success, 1 run/load failure, 2 usage
  error, and `diff` exits 3 when out of sync while still printing a
  parsable report.
- Raw ansible output routes to per-run/per-task logs by default;
  `--stream-ansible` tees it to the terminal.

## Consequences

- A typo or malformed invocation never costs a sudo password prompt;
  the classification is pinned by tests
  (`TestGateStaysRootlessForTyposAndBogusStages`,
  `TestRenderInputDirStaysRootless`, `TestStripLeadingGlobalFlags`).
- New commands must be classified in the root gate and must reuse the
  shared flag help registrars; new stage-like vocabularies must derive
  from one accessor so validation, help, and completion cannot drift.
- New destructive behavior routes through the existing `--yes` single
  gate and the `--override` remedy vocabulary instead of inventing
  per-command force flags.
- Frame byte output stays testable and consistent because it is
  confined to the output package; automation can gate on the exit-code
  contract and on color-free piped output.
- Operational gotchas behind these rules are cataloged in
  `.agents/knowledge/cli-root-gate-sudo-reexec.md`,
  `cli-flag-and-parsing-conventions.md`, and
  `cli-terminal-rendering-contract.md`.
