# A fix to a failed or unknown step continues without a destroy

Observed 2026-10-13 in the owner's M2b Ceph run, where a destroy of the whole
Ceph fleet followed an automation-only fix that the code did not require.
Revised for setup v2 (X88): no execution bundle exists any more, so the
automation never needs `setup` to reach a run.

## Symptom

An apply left a step `failed` or `unknown`, the fix was to `ansible/` only, and
the operator destroyed and rebuilt the whole context (35 minutes for the Ceph
fleet) because the milestone status said a moved automation digest needs
retirement. Builds before X88 also stopped the continuation at the first step
with `controller.identity`, "the embedded automation does not match the
approved execution bundle", unless `bootwright setup` had projected the new
automation first.

## Cause

A block's content digest is computed from the implementation's declared
contract: its identity, its request and evidence versions and its effect pins
(images, the cephadm binary, package sets, host paths). No capability package
reads the automation, so a fix confined to automation moves the automation
digest and no content digest, request byte or version. Another build continues
an operation exactly when it freezes the same blocks
([dependency safety](../../specs/state-reconciliation.md#dependency-safety-during-recovery),
D142). Since X88 each invocation stages the running executable's embedded
automation itself, and the execution closure (`execution-closure-v2`) is only
the runtime lock digest and the interpreter ABI, so a rebuild that keeps the
lock continues with no `setup`.

## Rule

- Fix, `make build`, then `bootwright apply` with the same `--stage`. The
  operation, its done blocks and their evidence stay; each attempt record names
  the build that ran it (`build`), and `plan` and `status` say the operation was
  registered by another build.
- `bootwright setup` is needed only when the fix moves the runtime lock (another
  `ansible-core`, Python dependency or interpreter ABI). Before that setup the
  apply refuses with `run bootwright setup`; after it, a live operation whose
  closure differs refuses naming the build that registered it, and offers the
  removal when the operation can be superseded.
- An unknown block is observed first. When the observation leaves it `failed`,
  the same apply retries it and runs its dependents (D146, X82). Before X82 that
  apply stopped and the next one retried it, so recovery took two applies; a
  build from before X82 still behaves that way.
- A destroy is needed when the fix moves a version or a pin, which moves the
  content digest: the continuation then refuses naming the first block that
  differs. The contract guard in `cmd/bootwright` fails the build when a request
  or evidence schema or a persisted automation format changes with the digest
  unmoved; it cannot see a change of meaning under an unmoved version, so a fix
  that changes what an effect is must move the implementation's version.
- After continuing an operation with a build that writes `build`, do not go
  back to an earlier build for it: a build from before X74 decodes records
  closed and refuses one that carries `build` as "lifecycle record is malformed
  or unsupported", so it can neither continue nor remove the operation. The
  removal under the build in hand is the way out (D145).
