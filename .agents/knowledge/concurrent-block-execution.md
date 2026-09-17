# Concurrent block execution

What running an operation's blocks together cost to get right, beyond the
contract [state reconciliation](../../specs/state-reconciliation.md#stages-and-the-pause-boundary)
owns. Delivered 2026-09-17; **no real-host apply or destroy has run under it
yet**, so everything below is from the in-tree suites and the code.

## The plan was already a graph; only the executor was not

`BlockDefinition.Requires` resolved into `Dependencies` from the first
lifecycle delivery, and `reconciliation.Ready`/`Startable` already answered
which blocks could start. The sequential part was one `candidate` call per
iteration in `lifecycle.run`. Replacing it needed no change to how a capability
declares anything.

## Four latent races the sequential executor hid

Each of these is a data race the moment two blocks run, and none of them is
visible while one block runs at a time:

- `operationstore.Store.expected` is a map every block writes as it publishes
  its own records. Each block writes only its own paths, so the map is guarded
  rather than the writes serialized.
- The lifecycle transaction collected the bundle areas its `OpenBundle` handed
  out in a bare slice. The lifecycle now opens the approved bundle **once per
  operation** in `run` and passes it down, which removes the contention, and
  the collector is guarded anyway because the opener is a published port.
- `controllerBundleArea.Location` writes `scanned = false`. That is safe only
  because `Location` is called once, inside `approvedBundle`, before any block
  starts. `Read`, which every block calls through `verifyAutomation`, touches
  no shared field.
- The **test fakes** raced before the engine did: the memory areas, the
  capability recorder, the progress recorder, the execution guard's counter and
  both mutating test clocks. A test clock that advances on every reading is the
  least obvious one, and it is the only clock a block stamps its records with.

`ExecutionGuard.WithPython` was already safe: every handle is a local, and the
lock it takes on the native package file is a read lock, so concurrent holders
are expected.

## Admission rules, and why each is not just "start everything ready"

`lifecycle/scheduler.go` admits in a strict precedence, and each level exists
to preserve a safety rule that predates concurrency:

1. An **unproved** effect is observed before anything else, and several may be
   observed together because observation is read-only. Nothing else is admitted
   while any block is unproved.
2. A **failed** block is retried alone. What follows it depends on it
   succeeding, so running siblings beside the retry would start work the retry
   may invalidate.
3. Otherwise every **startable** block starts, in frozen plan order, up to the
   bound, skipping any whose exclusive resources are in use.

The `worked` set is what keeps this equal to the old behavior: an invocation
never retries a block it failed itself and never observes an effect it just
left unproved. Without it the scheduler retries a fresh failure immediately and
loops.

A failure or a cancellation **admits nothing further and waits** for what is in
flight. Killing an attempt mid-effect converts a provable outcome into an
unproved one, which is the one state nothing can retry, destroy or delete past.

## The bound is not plan intent

`MaxRunningBlocks = 8`, overridable through `lifecycle.Options.Concurrency`
(zero takes the default) so tests pin 1. It is deliberately **not** frozen in
the plan, not in desired state and not a flag: ADR 0057 on the superseded
`work/apply-destroy-rework-…` branch already settled that the graph decides
what may run together, and commit `33c3db5f` removed the parallelism flags.
A continuation of an operation frozen by another build runs under this build's
bound, which changes no effect, no order and no evidence.

The existing journey suite runs at `Concurrency: 1`, which reproduces the old
order exactly; that is what keeps those goldens meaningful.

## Wave-major plan order changes every plan digest

`order()` now sorts by (wave, identity) instead of walking a lexicographic
ready queue, so the numbered plan reads as the schedule. The order is inside
`Plan.Digest()`, and `operationstore.ReadPlan` re-derives the plan and compares
digests, so **an operation frozen by an earlier build refuses**. Live contexts
must be destroyed with a pre-change binary first, exactly as for
`content-digest-bump-strands-destroy`.

`ScheduleOf` measures a frozen plan for `plan` output. Its first version only
recorded a depth for blocks that had dependencies, so roots were missing from
the map and a plan of four roots reported "up to 1 step at once". Every block
gets a wave, including one nothing waits for.

## Exclusive resources: the graph does not order writers

Two blocks the graph never ordered can still write to one place. The first case
is the Anaconda package tree, published per **install profile**: two Machines
on one profile extract and rename over the same path, and the role guards it
only by observing first. `Request.ExclusiveKeys` names `path:<tree>` and the
scheduler never runs two holders together. The key is frozen with the plan and
kept verbatim by a removal, because what must not run together to create
something must not run together to remove it.

Watch for the same shape elsewhere: anything derived from something other than
the block's own object is a candidate.
