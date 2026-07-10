# Add-on records: secret hygiene and the Hooks clobber trap

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

**Hook/effect failures:** `HookError{Hook, Lifecycle, Detail}` and
`EffectError{Effect, Input, Detail}` exist so a hook/effect failure is recorded
with a hook- or effect-specific summary instead of naming an `oc apply`
target. Their `Detail` field must never hold secret bytes because the summary
is persisted into the observed-state add-on record. The same rule applies to
`HookRecord.LastError`.

**Inline Secrets rejected at validate:** OLM custom resources are applied via
stdin, so an inline `kind: Secret` carrying `data`/`stringData` under
`spec.olm.customResources` would be written verbatim AND logged with zero
diagnostic. Validation rejects it and steers operators to reference a secret
provided at apply time. Non-Secret custom resources with `data`/`stringData`
fields are untouched.

**Record.Hooks two-writer protocol:** `Record.Hooks` is a map keyed by hook
name of `HookRecord{Lifecycle, Status, Digest, RanAt, LastError}`; `Digest` is
the hook's content+inputs digest used to skip an unchanged `run: onChange`
hook. The add-on engine (Apply/Wait) rebuilds the record from scratch on every
save and never sets `Hooks`; the hook executor writes per-hook state out of
band via `SetHook` (a load-modify-save that is the executor's only writer).
`SaveRecord` therefore preserves any `Hooks` already on disk when the incoming
record carries none — without this, an engine save would clobber the
executor's per-hook updates.

**Scoped secret materialization:** `ContextStore.MaterializeSelected`
materializes only the named materials into a target dir and is the
scoped-secrets primitive for ClusterAddon hook runs: a hook receives only its
declared `secretRefs` (and, for its connection dir, only the target machines'
SSH key material), never the whole store. Names not present in the store are
silently skipped — a missing secret is preflight's job to report, not
materialization's.
