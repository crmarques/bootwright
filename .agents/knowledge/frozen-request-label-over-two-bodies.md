# A frozen request label that covers two bodies

Observed on 2026-09-17. Required behavior stays in
[state reconciliation](../../specs/state-reconciliation.md#continuation-and-removal).

## Symptom

`destroy` on a context whose apply had completed refused before registering
anything:

```text
[FAIL] lifecycle.state: the request the block substrate-host-lab-libvirt froze
cannot be read by this executable: the frozen provider host request is
malformed; next: remove it with the executable its operation.json records
```

The executable named was the one that had just been built, and the prior
version reader the capability carries was in place.

## Mechanism

`4d5bdf9f` replaced the libvirt host request's `service` string with a
`services` list and left the label at `substrate-host-libvirt-v1`. `70ea5379`
bumped the label to `-v2` and added a reader for `-v1` shaped as the label was
declared, with `service`. Every build between the two froze this build's body
under the prior label, so a context applied by one of them holds bytes that
declare `-v1` and carry `services`. The prior reader decodes with
`DisallowUnknownFields` and refuses them as malformed, and the current reader
never sees them because the label chose the prior one.

The blocks beside it were unaffected because their labels moved with their
bodies before that window: `os-install-anaconda-v3` in `93d8d6ba`, and
`machine-libvirt-v1` and `controller-clients-v1` kept the body the window build
wrote.

## Decisions

- [`decodePriorHostRequest`](../../internal/substrate/libvirt/requests.go)
  reads both bodies the prior label was frozen over. The key that changed
  tells them apart: `services` selects this build's shape with the label
  rewritten on read, anything else selects the declared prior shape. Each body
  is still proved canonical against the shape that wrote it, so the refusal
  for bytes that are neither is unchanged.
- The label is rewritten to the current version because a removal runs the
  bundle the current controller receipt approves, whose role accepts only the
  version this build writes. The prior reader already upgrades the same way.
- Proved by `TestAFrozenHostRequestOfThePriorLabelOverThisBodyReadsAsThisOne`
  and `TestAFrozenHostRequestOfThePriorLabelOverAnUnknownBodyRefuses` in
  [`requests_test.go`](../../internal/substrate/libvirt/requests_test.go).

## How to apply

When a version bump follows a shape change that shipped without one, the prior
reader must accept every body that label was frozen with, not only the shape
the label was declared for. Find the window with `git log -S` on the label
constant and on the field that changed, and cover each shape a build in that
range encoded. A context applied by a window build is what the lab holds
between the two commits, so the reader that omits it strands exactly the
context an operator is about to remove.
