# ADR 0010: CLI Gate and Flag Conventions

## Status

Accepted

The destructive-gating clause is revised by
[ADR 0030](0030-one-intent-flag-and-named-authorizations.md), which replaces the
per-risk boolean flags with one intent flag and one named-authorization
vocabulary. The rest of this ADR is unchanged.

## Context

The Bootwright context — rendered state, secrets, run history, live
state, and the media and add-ons stores — lives under the root-owned
`/var/lib/bootwright` tree, so most commands re-execute themselves under
sudo. Without a single policy, escalation, flag naming, destructive-
command gating, and human output each drifted per command: a typo could
cost a sudo password prompt before failing, overwrite acknowledgment
varied between a separate force flag and `--yes` dances, the `--stage` vocabulary
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
  `cluster list/info/kubeconfig`, `context list/current`, `media list`,
  `add-ons list`, and a `validate` that reads the context rather than a
  `-f` file — because the context and the media/add-ons stores live under
  `/var/lib/bootwright`.
- Any invocation cobra would reject stays rootless so the user sees
  cobra's error (including its "Did you mean" suggestion) as themselves,
  never after a doomed sudo password prompt: unknown top-level commands
  and subcommands, a bogus `apply`/`destroy` `--stage`/`--through` value
  (validated against `converge.ApplyStageNames`, plus the `end` sentinel
  for `--through` via `converge.ApplyThroughNames`), a `validate`
  invocation with an unknown flag or stray positional, any
  `--name`-targeted command missing a non-empty `--name` value
  (`argsHaveNameValue`), and any invocation whose global `--ssh-user`
  value is missing or is not a valid POSIX user name
  (`argsHaveUnusableSSHUser`).
- `destroy` with an omitted `--stage` is a full-lifecycle destroy of the whole
  context or of the roots selected by `--clusters`, and must escalate
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
  the per-user registry (`argsMayMutateRegistry`); of those, `context use`
  also escalates, while `init`/`update`/`delete` do not. Only `apply`, rootful
  destroy targets, and `bastion setup` may use a become password
  (`argsMayUseBecome`).
- `machine rsh`/`exec` and `cluster rsh`/`exec` run and wait for the SSH
  client as root specifically to decrypt root-owned SSH private material.
  The client reads that material through a parent-held anonymous descriptor;
  Bootwright forwards cancellation, preserves the child exit status, closes
  the descriptor after SSH exits, and escalates only when a non-empty `--name`
  is present. `container-cluster oc`/`kubectl` follow the same shape for the same reason
  — the cluster kubeconfig is root-owned — and likewise preserve the child
  exit status.
- A leading global `--context` is stripped for classification only; the
  original args are forwarded verbatim to the sudo child.

Alternative, considered but not validated: carry the classification on the
cobra commands themselves (`Annotations["bootwright.io/root"]` =
`required` | `forbidden` | `name-gated`), read it from the built command tree,
and assert coverage with a fitness test — removing the parallel argv parser and
the drift between it and the command set. It is recorded as an option, not a
decision: the gate must classify before cobra parses, so the tree would have to
be built and discarded ahead of execution, and that has not been tried.

### Flag naming coherence

- Shared flag help strings and registrars live in one file
  (`internal/cli/flags.go`); commands reuse them so a flag never carries
  different wording on different commands.
- Command-vocabulary flags derive from single accessors: the
  `--stage`/`--through` values come from `internal/converge`
  (`FamilyStageNames()` = `infra`, `clusters`; `SubPhaseStageNames()` =
  `fabric`, `machines`, `deps`, `base`, `add-ons`) in all three surfaces
  — validation/error text, help, and shell completion. `--through` adds the
  `end` sentinel (`ApplyThroughNames()`), which resolves to the final stage;
  `--stage` and `--through` compose into an inclusive stage range. `destroy`
  accepts families only, because a sub-phase has no single destroy
  playbook.
- Targeted resource commands take `--name` rather than positionals
  (`secret delete/show`, `media add/delete`, `add-ons add/delete`,
  `machine`/`cluster` `rsh`/`exec`, `container-cluster oc`/`kubectl`).

### Destructive-command gating and override remedies

- Destructive paths are fail-closed by default; a marker or drift
  mismatch refuses rather than proceeding.
- Overwriting stored material is acknowledged by a single gate — one
  `--yes` or one interactive `y`, never a second boolean plus a
  confirmation dance (`media add` replace matches `secret set`).
- The destructive surface is exactly two axes, per
  [ADR 0030](0030-one-intent-flag-and-named-authorizations.md): `--mode
  create|reconcile|rebuild` states intent, and a repeatable `--authorize
  <token>` states which named risk the operator accepts. `--mode rebuild` is
  the uniform remedy that authorizes Bootwright-owned destructive
  re-convergence (managed-OS reinstall of a drifted node, Ceph
  wipe-and-rebuild), and `--authorize data-loss` authorizes the disk wipe such
  a rebuild performs. Both help texts must name that scope, and failure
  summaries preserve the exact remedy command (middle-ellipsis shortening keeps
  the `--mode rebuild --authorize data-loss` tail).
- A destroy-time Ceph marker recovery carries the identity being confirmed in
  `--recover-ceph-ownership <StorageCluster>=<fsid>` rather than a boolean
  bypass: naming the exact identity is an attestation only an operator can
  make, where a boolean would authorize whatever happens to be on the host.
  The agreement checks it performs, and the gates it does not relax, are
  specified in [`state-model.md`](../state-model.md) ("CLI Contract"). It
  authorizes no named risk of its own: a teardown that destroys data still
  needs `--authorize data-loss` or the interactive data-loss confirmation.
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
- Exit codes are contract, including the carve-out for the commands that
  hand control to a child process; specified in
  [`state-model.md`](../state-model.md) ("CLI Contract").
- Raw ansible output routes to per-run/per-task logs by default; `-v` /
  `--verbose` tees the full Ansible task output to the terminal AND the run
  log and un-censors values normally hidden as "censored due to no_log", so a
  verbose run leaves credential plaintext in its persisted log. The scope of
  that escape hatch is specified in [`security.md`](../security.md)
  ("Redaction escape hatch").

## Consequences

- A typo or malformed invocation never costs a sudo password prompt;
  the classification is pinned by tests
  (`TestGateStaysRootlessForTyposAndBogusStages`,
  `TestRenderInputDirStaysRootless`, `TestStripLeadingGlobalFlags`).
- New commands must be classified in the root gate and must reuse the
  shared flag help registrars; new stage-like vocabularies must derive
  from one accessor so validation, help, and completion cannot drift.
- New destructive behavior names its risk as an `--authorize` token in the one
  vocabulary; it never adds a flag. A new flag is justified only when it
  carries an identity, a value, or a scope a token cannot express — as
  `--reclaim-devices` and `--recover-ceph-ownership` do — never as a second
  spelling of `--yes`, `--mode`, or an existing token.
- Frame byte output stays testable and consistent because it is
  confined to the output package; automation can gate on the exit-code
  contract and on color-free piped output.
- Operational gotchas behind these rules are cataloged in
  `.agents/knowledge/cli-root-gate-sudo-reexec.md`,
  `cli-flag-and-parsing-conventions.md`, and
  `cli-terminal-rendering-contract.md`.
