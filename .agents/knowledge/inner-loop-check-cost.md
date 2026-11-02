# Inner-loop check cost

The [verification tiers](../skills/code-implementation/references/verification.md)
own which check runs when. This page records why the gate is slow, why the
inner loop is shaped as it is, and the traps found while measuring it on
2026-10-18 (16 cores, warm caches).

## Where the gate's time goes

- The whole Go suite took about 43 s uncached on 2026-10-18 and 112 s on
  2026-11-02, then 90 s after X286; vet on both platforms 14 s, docs-check
  3 s. The suite waits for its longest package, and the slowest ones run their
  tests one after another: on 2026-11-02 the wall time of `cmd/bootwright`
  (76 s), `internal/workspace/contextfs` (71 s),
  `internal/reconciliation/lifecycle` (78 s) and `test/architecture` (40 s)
  each equalled the sum of their tests (B1053).
- Profile a slow test before shortening it:
  `./scripts/go test -count=1 -run '^TestName$' -cpuprofile cpu.prof -o pkg.test ./<package>`,
  then `./scripts/go tool pprof -top -cum pkg.test cpu.prof`. In memory doubles
  over a flat path map, a scan per call is quadratic once a test fills the area
  to its bound; that cost, not the code under test, made the lifecycle reserve
  test take 52 s. contextfs time is real: its `directory.verify` walk opens
  each entry, and the crash harness repeats it hundreds of times.
- `t.Parallel` on one heavy test gains nothing: Go starts a top-level parallel
  test only after every serial test of its package has finished.
- Ansible is about 4.7 of the gate's 6 minutes: syntax 15 s, lint 22 s,
  sanity 54 s and units 185 s. Units runs the 3818 collection tests twice
  serially, under ansible-test and as the plain loop, then the module tests
  again under the 3.9 floor. Sanity's cost is mostly building its tool
  environments; over two target files all 25 tests take 17 s.
- Narrowing is where the savings are: lint over one role takes 2 s, syntax over
  one playbook directory 3 s, and units over one test file 1.5 s.

## pytest-xdist is no gate

pytest-xdist is in the reviewed lock, and the plain loop over the whole tree
takes 21 s across workers instead of 86 s serially. It is not equivalent:

- execnet gives every worker an I/O thread, so each `os.fork()` in a test warns
  `This process ... is multi-threaded, use of fork() may lead to deadlocks in
  the child`.
- A process-directed signal can reach that thread. A test that signals itself
  with `os.kill(os.getpid(), signal.SIGALRM)` then sees its handler run early
  or late and fails in parallel only, intermittently. Signal the calling
  thread with `signal.pthread_kill(threading.get_ident(), ...)`, as
  `test_control_startup_alarm_cannot_escape_registration_cleanup` in
  `tests/unit/plugins/module_utils/test_ceph_host_route_probe.py` does (B866):
  a blocked signal then stays pending on that thread alone.

The narrowed units suite therefore runs one file serially and reruns every
parallel failure serially. The full gate's ansible-test unit phases run on
pytest-xdist workers by design: the locked ansible-test turns `--num-workers 0`
into `-n auto` and has no in-process run, and its nearest serial run, one
worker, took 1054 s instead of 189 s across the five unit phases of a
2026-11-02 `make check` while still carrying execnet's I/O thread. The gate
therefore selects the mode explicitly (`units_arguments` and
`UNITS_WORKERS_AUTO` in `scripts/tools/ansible_check.py`), and the plain pytest
loop after it is the in-process serial run that settles every test with no
worker thread. `UnitsWorkersTest` reads the `-n` value from the command
`ansible-test units --explain` generates
([B798](../../specs/milestones/delivered.md#x278--the-owners-choices-for-b508-and-b798)).

## A no-op pause over the real clock

On 2026-11-02 one unit-test worker grew to 26 GB and the kernel OOM killer
froze the laptop, twice. `Worker.manager_declare` repeats a declaration
until `MANAGER_WAIT` passes on `worker.clock`. A test fake had replaced
`pause` with a no-op but left the real monotonic clock. A lost reply that never
cleared then spun for 300 real seconds, and every pass appended the spec to
the fake monitor's logs. The same fake's `mgr dump` lacked the `active_gid`
real Ceph reports, so the same-manager exit never fired.

A fake that replaces `pause` must advance the clock it bounds, as `worker` in
`tests/unit/plugins/module_utils/test_ceph_service_units.py` now does: a
retry that never settles then fails in milliseconds at its bound. A fake
monitor answers each field the code under test branches on. Reproduce a
suspected runaway under a cap, such as
`systemd-run --user --scope -p MemoryMax=2G -p MemorySwapMax=0`, one file at
a time.

## Selection traps

- quick-test once took its base from `origin/main` first. With local `main`
  ahead of an unpushed remote, every commit since the last push counted as a
  change; one task branch selected 288 paths. It now takes the nearer of the
  two merge bases (`TestQuickTestComparesWithTheNearerMain`).
- The reader rule matches a quoted first path element anywhere in a test, so a
  table row naming the `ansible` field selects `cmd/bootwright` (24 s) for any
  Ansible change. The focused loop runs its Go lane beside the Ansible lanes
  rather than narrowing that rule.
- `./...` in the primary checkout fails with `permission denied` when an
  ignored directory the user cannot read, such as root-owned evidence under
  `examples/wip/`, sits in the tree; vet, quick-test and the request-field
  architecture test then fail there and pass in a task worktree.
- Go caches a test result while the test binary and the files it opened are
  unchanged. A constant nothing reads leaves the binary unchanged, so a timing
  probe needs a change the linker keeps, such as an `init` function.

## A setup lease that survives its callback

Before X247, `Store.mutateSetup` closed its locked file without explicitly
unlocking it. Linux flock locks belong to the open file description: an
unrelated fork can retain that description until exec even when the parent
descriptor has close-on-exec set. Closing the parent's descriptor then leaves
the setup lease held, so an immediate retry can report `lifecycle.lease`.
This reproduces a mechanism for the setup lease failures recorded as B673;
an isolated pass alone did not diagnose it.

The existing `unlockAndClose` helper now releases the setup lock when the
transaction returns, following
[the lock lifetime contract](../../specs/contexts.md#storage-locking-and-publication).
`TestACompletedSetupUnlocksBeforeClosingItsSharedDescription` in
[setup_linux_amd64_test.go](../../internal/workspace/contextfs/setup_linux_amd64_test.go)
keeps a duplicated descriptor open against the same file description through
success, refusal and cancellation. It proves exclusion during the callback and immediate retry
afterward without a process-timing assumption. The whole interruption harness
and concurrent gates remain necessary checks; do not hide a lease failure by
serializing the tests or adding retries.
