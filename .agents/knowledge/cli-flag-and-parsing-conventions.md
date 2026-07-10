# CLI flag help and argument parsing conventions

**Constraint: shared flag help strings have one source.**
`internal/cli/flags.go` owns every flag help string shared across
commands (`flagOutputUsage`, `flagDryRunUsage`, `flagVerboseUsage`, …)
and the `add*Flag` registrars; per-command flags must reuse these so the
same flag never drifts to different wording on different commands. The
`--verbose` (`-v`) help must explicitly warn that it reveals values
normally censored by `no_log` (secrets, BMC/registry/RHSM/proxy
credentials, tokens, generated Ceph keys) and that they are written to
the terminal AND the run log.

**Constraint: the `--stage`/`--through` vocabulary derives from
internal/converge accessors.** `FamilyStageNames()` = `{infra,
clusters}`, `SubPhaseStageNames()` = `{fabric, machines, deps, base,
add-ons}`, composed by `ApplyStageNames`/`DestroyStageNames`. All three
surfaces — flag validation/error text, flag help, and shell completion —
must derive from them so they can never drift (each accessor returns a
fresh slice so callers may append). `cli_test` locks the exact `--stage`
error wording; `TestStageFlagCompletionOffersCanonicalValues` pins that
apply/plan/diff complete the full vocabulary while destroy completes
families only. A drift here means one surface fell behind.

**Constraint: `--override` help must name the destructive scope it
authorizes.** Managed-OS VM reinstall and Ceph wipe-and-rebuild — never
understated as "install-mismatch checks". The guarding test matches
single tokens so the assertion survives cobra's help line wrapping, and a
companion assertion matches the removed `--scope` flag at its token
boundary (trailing space) so a resurrected `--scope` is still rejected.

**Gotcha: dispatcher wiring rules (`requireSubcommand`).**
(1) `FParseErrWhitelist.UnknownFlags` is required, or pflag bails on a
child's flag (e.g. `cluster acces --cluster X`) before Cobra ever sees
the typo'd subcommand. (2) Do NOT set `ValidArgs` mirroring subcommand
names — Cobra's `getCompletions` then lists every subcommand twice (once
described from the subcommand walk, once bare from `ValidArgs`); the CLI
reproduces the "Did you mean" suggestion via `unknownSubcommandError`
instead. (3) `RunE` must be set (to Help + exit 2) because Cobra skips
`ValidateArgs` on commands with no `Run*`. (4) A bare dispatcher exits 2
so a CI gate that forgets the target (e.g. `bootwright preflight`) fails
instead of passing green. Guarded by
`TestDispatcherCompletionListsSubcommandsOnce` and
`TestDispatcherUsageErrorsExitTwo`.

**Gotcha: JSON error-output selection honors the `--` terminator.**
`argsRequestJSON` (which selects JSON-formatted error output for the
top-level error handler) must treat everything after `--` as positional:
a bare `--output json` appearing there (e.g. as a `machine exec` command
tail) must NOT switch error output to JSON. Pinned by
`TestArgsRequestJSONHonorsFlagTerminator`.

**Gotcha: `confirm()` must not buffer past its own line.** It reads
exactly one line from the shared stdin one byte at a time
(`readSingleLine`) instead of wrapping stdin in a fresh `bufio.Reader`: a
per-call buffered reader would swallow every byte past that line, so a
later prompt over the same piped stdin would see a spurious EOF and
silently reject a correct answer. If the caller already passes a
`*bufio.Reader` it is used directly. Guarded by
`TestConfirmPreservesBufferedStdinAcrossPrompts` (two piped answers over
one reader).
