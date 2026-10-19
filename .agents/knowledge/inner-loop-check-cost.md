# Inner-loop check cost

The [verification tiers](../skills/code-implementation/references/verification.md)
own which check runs when. This page records why the gate is slow, why the
inner loop is shaped as it is, and the traps found while measuring it on
2026-10-18 (16 cores, warm caches).

## Where the gate's time goes

- Go is not the cost. The whole Go suite takes about 43 s uncached, vet on both
  platforms 14 s, and docs-check 3 s. The slowest packages are
  `internal/workspace/contextfs` (32 s, half of it one crash-injection test),
  `cmd/bootwright` (24 s) and `internal/reconciliation/lifecycle` (15 s).
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
  with `os.kill(os.getpid(), signal.SIGALRM)`, as
  `test_control_startup_alarm_cannot_escape_registration_cleanup` in
  `tests/unit/plugins/module_utils/test_ceph_host_route_probe.py` does, then
  sees its handler run late and fails in parallel only, intermittently.

The narrowed units suite therefore runs one file serially and reruns every
parallel failure serially; the gate stays serial.

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
