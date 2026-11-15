# A fix to a failed or unknown step continues without a destroy

Observed 2026-10-13 in the owner's M2b Ceph run, where a destroy of the whole
Ceph fleet followed an automation-only fix that the code did not require.
No execution bundle exists, so the automation never needs
`controller install` to reach a run.

## Symptom

An apply left a step `failed` or `unknown`, the fix was to `ansible/` only, and
the operator destroyed and rebuilt the whole context (35 minutes for the Ceph
fleet) because the milestone status said a moved automation digest needs
retirement.

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
an operation when its current execution contracts match the saved blocks
and their producers
([dependency safety](../../specs/state-reconciliation.md#dependency-safety-during-recovery),
D142). Each invocation stages the running executable's embedded
automation itself, and the execution closure (`execution-closure-v1`) is only
the runtime lock digest and the interpreter ABI, so a rebuild that keeps the
lock continues with no `controller install`.

## Rule

- Fix, `make build`, then the operator runs `bootwright apply` with the same
  `--stage`; an agent proposes it and runs it only when the owner asks
  ([AGENTS.md](../../AGENTS.md)). The operation, its done blocks and their
  evidence stay; each attempt record names the build that ran it (`build`), and
  `plan` and `status` say the operation was registered by another build.
- `bootwright controller install` is needed only when the fix moves the runtime lock (another
  `ansible-core`, Python dependency or interpreter ABI). Before that setup the
  apply refuses with `run bootwright controller install`; after it, a live operation whose
  closure differs refuses naming the build that registered it, and offers the
  removal when the operation can be superseded.
- An unknown block is observed first. When the observation leaves it `failed`,
  the same apply retries it and runs its dependents (D146, X82).
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
  it continues another build's live operation without a version move.
- An exhausted image pull continues too: each host preparation run makes up
  to three bounded pull attempts of its own, and the retained `pull-attempt-N`
  records serve crash diagnosis and recovery only (B648, X102).
- A planner-only default or ordering change does not prevent continuation: the
  saved plan remains authoritative. A changed execution contract or runtime
  requires finishing or removing with its registering build. Operations written
  before the required contract metadata also use that build. The contract goldens
  in `cmd/bootwright` pin the current shape and are regenerated after review.
  Versions remain fixed under the
  [development policy](../../specs/project.md#design-priorities-and-non-goals);
  they do not establish compatibility with existing development effects.
- A changed add-on package render affects newly planned requests; an existing
  operation keeps its saved program while its execution contract remains
  supported. A changed execution or evidence contract refuses as above. The
  current runtime manifest is checked by the same contract guard before a
  supported build ships; it carries no earlier manifest or historical reader.
