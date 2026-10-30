# A fix to a failed or unknown step continues without a destroy

Observed 2026-10-13 in the owner's M2b Ceph run, where a destroy of the whole
Ceph fleet followed an automation-only fix that the code did not require.
Revised for setup v2 (X88): no execution bundle exists any more, so the
automation never needs `controller install` to reach a run.

## Symptom

An apply left a step `failed` or `unknown`, the fix was to `ansible/` only, and
the operator destroyed and rebuilt the whole context (35 minutes for the Ceph
fleet) because the milestone status said a moved automation digest needs
retirement. Builds before X88 also stopped the continuation at the first step
with `controller.identity`, "the embedded automation does not match the
approved execution bundle", unless `bootwright controller install` had projected the new
automation first.

## Cause

A block's content digest is computed from the implementation's declared
contract: its identity, its request and evidence versions and its effect pins,
the Go-owned values outside the frozen request that fix what an effect is (an
artifact, a package set, a host path, a name prefix, an ownership mark); a
value the request carries needs no pin. Today one implementation hashes such
values directly and another a version tag moved by hand, so a pin change moves
the latter's digest only when its author moves the tag. No capability package
reads the automation, so a fix confined to automation moves the automation
digest and no content digest, request byte or version. Another build continues
an operation exactly when it freezes the same blocks
([dependency safety](../../specs/state-reconciliation.md#dependency-safety-during-recovery),
D142). Since X88 each invocation stages the running executable's embedded
automation itself, and the execution closure (`execution-closure-v2`) is only
the runtime lock digest and the interpreter ABI, so a rebuild that keeps the
lock continues with no `controller install`.

## Rule

- Fix, `make build`, then `bootwright apply` with the same `--stage`. The
  operation, its done blocks and their evidence stay; each attempt record names
  the build that ran it (`build`), and `plan` and `status` say the operation was
  registered by another build.
- `bootwright controller install` is needed only when the fix moves the runtime lock (another
  `ansible-core`, Python dependency or interpreter ABI). Before that setup the
  apply refuses with `run bootwright controller install`; after it, a live operation whose
  closure differs refuses naming the build that registered it, and offers the
  removal when the operation can be superseded.
- An unknown block is observed first. When the observation leaves it `failed`,
  the same apply retries it and runs its dependents (D146, X82). Before X82 that
  apply stopped and the next one retried it, so recovery took two applies; a
  build from before X82 still behaves that way.
- A new [version specialization](version-specializations.md) is an automation
  fix: its identity enters no intent or digest, so it continues a live
  operation at every step not yet attempted and at every step whose failure
  left no attempt record. A step that wrote an attempt record before failing
  natively (bootstrap's `bootstrap-attempt.json`, a tunnel setting's attempt
  marker) still refuses `partial-cluster`, so such a specialization must land
  before the run that needs it.
- The licensing and Call Home finalization of 5dbf06288 (the
  `tunnel-config-attempt-call_home_initialize.json` and
  `tunnel-config-complete-call_home_initialize.json` markers, the enroll-time
  `orch accept-license` and the persisted-acceptance readiness read) touched
  no Go file: its markers enter no intent, request or digest, so a build with
  it continues an earlier build's live operation without a version move.
- An exhausted image pull continues too: each host preparation run makes up
  to three bounded pull attempts of its own, and the retained `pull-attempt-N`
  records serve crash diagnosis and recovery only (B648, X102). A build from
  before X102 refuses `image-pull` on every apply once a host's three retained
  attempts are spent; the build with this fix continues that operation.
- A destroy is needed when the fix moves a version or a pin, which moves the
  content digest: the continuation then refuses naming the first block that
  differs. The contract guard in `cmd/bootwright` fails the build when a request
  or evidence schema or a persisted automation format changes with the digest
  unmoved; it cannot see a change of meaning under an unmoved version, so a fix
  that changes what an effect is must move the implementation's version.
- An add-on package's rendering change, such as another object, field or
  approval rule in what a package's program renders, changes that instance's
  request, so another build does not continue the operation: the continuation
  refuses naming the first block that differs, and the way out is a destroy. A
  fix confined to the cluster objects role or its modules (planned by
  [B751](../../specs/milestones/delivered.md#b751)) moves no content digest and no
  request byte, so it continues a live operation like any other automation fix
  here: rebuild, then `apply`. The approval-rule tags of
  [B750](../../specs/milestones/delivered.md#b750) were in the content digest from its
  first delivery. [B759](../../specs/milestones/delivered.md#b759) also adds bounded
  namespace plan/CSV evidence absent from the original evidence schema, so its
  delivery moves the add-on and network evidence to v2 and their content
  digests with it. An old operation therefore keeps its original executable;
  this change is not an automation-only continuation fix.
- After continuing an operation with a build that writes `build`, do not go
  back to an earlier build for it: a build from before X74 decodes records
  closed and refuses one that carries `build` as "lifecycle record is malformed
  or unsupported", so it can neither continue nor remove the operation. The
  removal under the build in hand is the way out (D145).
