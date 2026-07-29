# Add-on records: secret hygiene and the Steps clobber trap

**Constraint:** Observed-state add-on records must never hold secret bytes
(the specs/security.md invariant). Everything below exists to keep that true
while still leaving full diagnostics in the sanctioned apply log.

**Failed-apply summaries:** An `oc apply` error carries the raw oc
stdout/stderr, which can echo back user-inlined Secret bytes from the applied
manifest. On failure the record's `LastObserved` gets a non-secret summary —
`applyFailureSummary` names only the failed resource as
`Kind/Namespace/Name` or `Manifest/<path>` and points at the apply log — while
the raw output is preserved only in the returned error, which reaches the
apply log the runner already wrote. `TestApplyDoesNotPersistRawOutputInFailedRecord`
pins both halves: the record must not contain the secret and the error must
still carry the raw output.

**Step/effect failures:** `StepError{Step, Lifecycle, Detail}` and
`EffectError{Effect, Input, Detail}` exist so a step/effect failure is recorded
with a step- or effect-specific summary instead of naming an `oc apply`
target. Their `Detail` field must never hold secret bytes because the summary
is persisted into the observed-state add-on record. The same rule applies to
`StepRecord.LastError`.

**Inline Secrets rejected at validate:** OLM custom resources are applied via
stdin, so an inline `kind: Secret` carrying `data`/`stringData` under
`spec.olm.customResources` would be written verbatim AND logged with zero
diagnostic. Validation rejects it and steers operators to reference a secret
provided at apply time. Non-Secret custom resources with `data`/`stringData`
fields are untouched.

**Record.Steps two-writer protocol:** `Record.Steps` is a map keyed by step
name of `StepRecord{Lifecycle, Status, Digest, RanAt, LastError}`; `Digest` is
the step's content+inputs digest used to skip an unchanged `run: onChange`
step. The add-on engine (Apply/Wait) rebuilds the record from scratch on every
save and never sets `Steps`; the step executor writes per-step state out of
band via `SetStep` (a load-modify-save that is the executor's only writer).
`SaveRecord` therefore preserves any `Steps` already on disk when the incoming
record carries none — without this, an engine save would clobber the
executor's per-step updates.

**Scoped secret materialization:** `ContextStore.MaterializeSelected`
materializes only the named materials into a target dir and is the
scoped-secrets primitive for ClusterAddon step runs: a step receives only its
declared `secretRefs` (and, for its connection dir, only the target machines'
SSH key material), never the whole store. Names not present in the store are
silently skipped — a missing secret is preflight's job to report, not
materialization's.
