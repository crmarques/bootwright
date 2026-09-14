# Controller setup resolution and readiness cost

Observed on 2026-09-12 and 2026-09-13 on a ready Fedora 43 controller:
`bootwright setup` took about two minutes, then still almost a minute,
to report `unchanged`. [Controller](../../specs/controller.md#supported-host-and-dependency-selection)
owns the rules that setup reuses a serving retained resolution, never checks
for newer releases, and checks readiness by presence only; this page records
why each removed step was expensive, which is the reason those rules matter.

- Setup used to resolve dependencies before checking readiness, so every run
  paid the full resolution cost even with nothing to do. Resolution now runs
  only when no retained resolution serves the intent or a selected native root
  is missing. Code: `internal/controller/prerequisites/service.go` (`Setup`,
  `resolutionRequired`), `internal/controller/prerequisites/resolution.go`
  (`resolveDependencies`, `supersededLatest`).
- Bootstrap resolution is priced as acquisition, not as a metadata round-trip:
  it fetches the uv Python metadata, downloads the complete standalone Python
  archive, runs the pip resolver inside that projection, then downloads every
  resolved wheel to compute the projection identity. Code:
  `internal/controller/bundlelocal/bootstrap.go` (`Resolve`).
- Native resolution stages the `primary`, `filelists` and related members of
  every approved repository and copies the RPM database before the DNF helper
  solves. Code: `internal/controller/nativelocal/resolver_linux_amd64.go`
  (`Resolve`, `stageRepository`).
- Bundle readiness used to re-read every retained source, re-extract the Python
  archive and every wheel in memory, byte-compare all published files against
  that projection, then launch the private interpreter for the import probe.
  A sealed bundle now reports presence from one directory walk: retained
  sources by size, published files by count and total bytes, the interpreter,
  and tool files. Full verification and the probe still run when the bundle is
  published. Code: `internal/controller/bundlelocal/manager.go`
  (`Inspect`, `presentFiles`, `inspectFiles`).
- Native readiness used to load the full installed inventory through DNF and
  run `rpm --verify` over every package of the frozen plan, checking each
  installed file. It now snapshots the RPM database and asks `rpm -q` for each
  selected root by name through the helper's `present` operation; the installed
  identity is shown as the observed release and never compared to the frozen
  one. The helper's `inspect` operation keeps the full inventory for the native
  transaction's own proof. Code:
  `internal/controller/nativelocal/resolver_linux_amd64.go` (`Check`,
  `decodePresence`), `ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/native_resolution.py`
  (`present`).
- Evidence: `internal/controller/prerequisites` tests asserting that a ready
  controller, a retry after a terminal failure and a missing native root contact
  no bootstrap or tool publisher
  (`TestLatestSetupResolvesOnceAndReusesRetainedNoop`,
  `TestTargetToolsResolveBeforeConfirmationAndInstallEvenWithReadyRuntime`,
  `TestDefiniteRefusalRetriesFromRetainedClosure`), the sealed-bundle presence
  test in `internal/controller/bundlelocal` (`TestSealedBundleReadinessIsPresenceOnly`),
  the presence evidence test in `internal/controller/nativelocal`, and the
  helper unit test `test_present_reports_roots_by_name_without_verifying_files`.

Applies to the Linux/amd64 `bundlelocal` and `nativelocal` adapters. Revisit if
a publisher offers a projection identity without payload acquisition, or if a
presence-only readiness ever admits a broken dependency in practice.
