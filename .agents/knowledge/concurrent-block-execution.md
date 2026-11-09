# Concurrent block execution

What running an operation's blocks together cost to get right, beyond the
contract [state reconciliation](../../specs/state-reconciliation.md#stages-and-the-pause-boundary)
owns. Delivered 2026-09-17; everything below is from the in-tree suites and
the code, except the lazily created directory race, which the first real-host
destroy under it found.

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
- History from before X88: the lifecycle transaction collected the areas its
  `OpenBundle` handed out in a bare slice, the lifecycle opened an execution
  bundle once per operation and `controllerBundleArea.Location` wrote
  `scanned = false` (called once, in a symbol since deleted, before any block
  started). No execution bundle exists now. `OpenBundle` serves only tool
  areas (`controllerBundleArea` in `contextfs/controller_bundles_linux_amd64.go`)
  and the automation is staged per invocation, so none of that bundle code is
  on the block path; guard any collector of opened areas the same way, because
  the opener is a published port.
- The **test fakes** raced before the engine did: the memory areas, the
  capability recorder, the progress recorder, the execution guard's counter and
  both mutating test clocks. A test clock that advances on every reading is the
  least obvious one, and it is the only clock a block stamps its records with.

`ExecutionGuard.WithPython` is safe: every handle is a local and it takes a
shared flock on the runtime's `.lock`, so concurrent holders are expected; see
[controller runtime isolation](controller-runtime-isolation.md).

## A shared directory created lazily is a race too

The operation store makes only the operation's own directories up front; the
log tree's `blocks/` and each block's directory are created by the
first write that needs them, through `operationArea.EnsureDirectory` and
`OpenLog`. With a first wave of several blocks, every one of them races to
`Mkdirat` the same `blocks`, and the context's first operation also races on the
`operations` subtree itself. The losers' `EEXIST` used to be refused as a
foreign entry, so a real-host `destroy` of a 13-block context ended
`state: unknown` with `runtime.log: a required private operation log could not
be maintained`, and the log fault's cancellation then landed inside sibling
blocks' publications as `context.state: published lifecycle record is unsafe`.

The operation areas (lifecycle, bounded runs, SSH trust) and
`Store.ensureDirectory` now treat `EEXIST` as a concurrent creator winning:
they reopen through `openDirectory`, which applies exactly the checks an
existing directory passes, and sync the parent again, since the loser may
proceed before the winner's sync completes. A writer whose plain open lands
between the winner's `Mkdirat` and its sync still syncs nothing; that window
predates the fix. Files already had this shape in `openLog`. The secret,
bundle and media areas are single-writer and still refuse `EEXIST`, as must
any path created exclusively on purpose, such as a run's own directory.

## Admission rules, and why each is not just "start everything ready"

`lifecycle/scheduler.go` admits in a strict precedence, and each level exists
to preserve a safety rule that predates concurrency:

1. An **unproved** effect is observed before anything else, and several may be
   observed together because observation is read-only. Nothing else is admitted
   while any block is unproved.
2. Every selected **failed** block is retried as a first attempt runs, beside
   every startable block, up to the bound and skipping any whose exclusive
   resources are in use (D144). What follows a failed block already waits on
   it through its dependency, so a retry needs no other isolation; before X71
   it ran alone, which serialized independent hosts. A selection that admits
   no failed block refuses before any block is worked; after that, admission
   never depends on which block completes first.
3. Otherwise every **startable** block starts, in frozen plan order, up to the
   bound, skipping any whose exclusive resources are in use.

The `worked` set is what keeps this equal to the old behavior: an invocation
never retries a block it failed itself, never observes an effect it just left
unproved and never starts again a block whose attempt could not start. Without
it the scheduler retries a fresh failure immediately and loops; before X13 the
startable branch skipped the check, so a persistently failing attempt start
re-admitted its block without end.

An attempt that could not start or a cancellation **admits nothing further
and waits** for what is in flight. A failure admits no further first attempt
and waits too, but the selected failed blocks not yet retried are still
retried. Killing an attempt mid-effect converts a provable outcome into an
unproved one, which is the one state nothing can retry, destroy or delete past.

## The bound is not plan intent

`MaxRunningBlocks = 8`, overridable through `lifecycle.Options.Concurrency`
(zero takes the default) so tests pin 1. It is deliberately **not** frozen in
the plan, not in desired state and not a flag: the graph decides what may run
together.
A continuation of an operation frozen by another build runs under this build's
bound, which changes no effect, no order and no evidence.

The existing journey suite runs at `Concurrency: 1`, which reproduces the old
order exactly; that is what keeps those goldens meaningful.

## Wave-major plan order changes every plan digest

`order()` now sorts by (wave, identity) instead of walking a lexicographic
ready queue, so the numbered plan reads as the schedule. The order is inside
`Plan.Digest()`, and `operationstore.ReadPlan` re-derives the plan and compares
digests, so **a build whose order differs refuses an operation frozen by another
build**. Destroy the context with the build that froze it first, exactly as for
`content-digest-bump-strands-destroy`.

`ScheduleOf` measures a frozen plan for `plan` output. Its first version only
recorded a depth for blocks that had dependencies, so roots were missing from
the map and a plan of four roots reported "up to 1 step at once". Every block
gets a wave, including one nothing waits for.

## Exclusive resources: the graph does not order writers

Two blocks the graph never ordered can still write to one place. The first case
was the Anaconda package tree, published per **install profile**: two Machines
on one profile extracted and renamed over the same path. The original fix
put `path:<tree>` in each installation's `Request.ExclusiveKeys`, protecting
publication while serializing the full installer waits too.

X48 gives the shared publication its own durable block. Only it claims that
path; every installation requires its completion and reads the immutable
tree. Inverse dependencies retain it until its consumers are removed. This
separates a short shared effect from long independent machine work.

Watch for the same shape elsewhere: anything derived from something other than
the block's own object is a candidate.

## One Ceph claim serialized a whole cluster (X279)

Until X279 every Ceph block of a cluster claimed `ceph/<cluster>`, so after
the cluster block the owner's three-node run held about twenty startable
children and ran them one at a time, in frozen order, for about sixteen
minutes. `ScheduleOf` measured waves from dependencies alone, so `plan` read
"widest 12 steps" for a wave that could run one block. The claim now names
only the blocks that write cluster-wide orchestrator or manager state
(`exclusiveFor` in `internal/storage/ceph/capability.go`), every Ceph child
names the throttle `ceph/<cluster>` (four at once,
`lifecycle.MaxThrottledBlocks`), and `ScheduleOf` counts only what claims and
throttles admit together. `TestCephChildrenOfOneClusterStartTogetherUnderTheThrottle`
holds the compiled examples' DAG to it and fails under the old claim set.

Releasing a block from a claim needs three proofs, and serial execution had
hidden the lack of each:

- **One writer per native name.** A block adopts a same-name object only on
  its own attempt record, which trusted the claim to admit one writer.
  Admission now refuses a second owner of a native name, including a pool a
  gateway derives (`<zone>.rgw.*`, `.rgw.root`).
- **Manager restarts.** The settings block's `mgr module enable` and the
  monitoring unit's derived `prometheus` module respawn the active manager
  while siblings run. A manager-served command an unclaimed block reaches must
  repeat under `manager_retry` or read as unfinished work in an observation;
  an object bucket's `orch ps` did neither (B1041).
- **Shared private areas.** Markers in `/var/lib/bootwright/ceph/<fsid>` were
  already published without replacement, but the custody proof listed the
  area and then stat'ed each name, so a concurrent writer's `.ownership-*`
  staging name vanished in between (`FileNotFoundError`, B1040), the same
  shape as the `Mkdirat` race above.

The orchestrator families were released the same way (B1043): their
declarations go through `manager_declare`, which repeats until the manager
it was sent to holds and its orchestrator and declaring module answer, and
reads an NFS export or the SSO option back before declaring it again, so a
lost reply never doubles an effect.
What still serializes is named after what it protects: `ceph-mgr/<cluster>`
between the two manager restarters and every block not yet qualified beside
one, `ceph-rgw/<cluster>` between writers of `.rgw.root` and the default
realm.

## Production admission also needs live invocation ownership

The scheduler's bound was eight, but production bound one until X48. Raising
it exposed the runner's same-context sweep: a sibling adapter held a job lock
and was refused as another active invocation. Context and operation strings
cannot prove ownership because another invocation can continue the same
operation.

X48 opens a private lifecycle invocation scope. Runner copies share a live
registry that proves exact lock and holder file identity. Sweep and claim
publication, and unregister and release, are synchronized; process execution
remains concurrent. A sibling's cleanup cannot pass through an unregistered
claim, and a descendant holding a released job is no longer exempt. Foreign,
unknown and ended jobs still refuse. Role scratch uses the same runner-issued
job boundary; durable machine work has a separate context/machine path.

The three-node example's compiled production DAG and controlled real-runner
barriers qualify these boundaries in-tree. Operator overlap remains an M1
closing gate; no live run is implied by those tests.
