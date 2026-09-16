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
- An automation revision still names a new bundle area, because `Digest()`
  hashes every embedded file into `BootstrapDefinition.AutomationDigest`, which
  enters the bootstrap digest and through it `CatalogDigest`. It no longer costs
  a resolution. `validateDefinition` checks the provided execution foundation
  first and reports an automation-only mismatch as `ErrAutomationSuperseded`
  beside `ErrBootstrapIncompatible`; `Setup` then carries that resolution
  forward instead of replacing it. `Manager.Rebase` reads the retained sources
  out of the sealed area, projects them under the embedded automation and
  returns the same resolution under its new projection identity, and `Prepare`
  reads each source from that area before contacting a publisher. Nothing is
  downloaded and nothing is solved when the native roots are still installed.
  The superseded area still remains. Code: `ansible/assets.go` (`Digest`),
  `internal/controller/prerequisites/definition.go` (`resolvedContentDigest`),
  `internal/controller/bundlelocal/manager.go` (`validateDefinition`,
  `qualifiedFoundation`, `Rebase`, `retainedSource`),
  `internal/controller/prerequisites/resolution.go` (`carryForward`, `rebind`).
  Retirement of the replaced area is deferred to milestone candidate C24.
- Staging acquires only the repository members the provided solver opens:
  `primary` and `filelists` for DNF5, plus `updateinfo` and `modules` for DNF4,
  whose sack fill reads them. The publisher's own `repomd.xml` is still staged
  whole and still names the authenticated snapshot. Code:
  `internal/controller/nativelocal/resolver_linux_amd64.go` (`solverMetadata`,
  `stageRepository`).
- The inventory snapshot is taken once per invocation. `Resolver` retains the
  copy and reuses it while the live database still has the identity it was
  copied from, so a setup that proves presence three or four times copies the
  database once; `Close`, returned through `localControllerDependencies` and
  deferred by `run`, releases it. Code:
  `internal/controller/nativelocal/resolver_linux_amd64.go` (`stageDatabase`,
  `databaseState`, `retainedDatabase.serves`, `Close`).
- Evidence: `internal/controller/prerequisites` tests asserting that a ready
  controller, a retry after a terminal failure and a missing native root contact
  no bootstrap or tool publisher
  (`TestLatestSetupResolvesOnceAndReusesRetainedNoop`,
  `TestTargetToolsResolveBeforeConfirmationAndInstallEvenWithReadyRuntime`,
  `TestDefiniteRefusalRetriesFromRetainedClosure`), the sealed-bundle presence
  test in `internal/controller/bundlelocal` (`TestSealedBundleReadinessIsPresenceOnly`),
  the presence evidence test in `internal/controller/nativelocal`, and the
  helper unit test `test_present_reports_roots_by_name_without_verifying_files`.
  For carrying a resolution forward:
  `TestSupersededAutomationCarriesTheRetainedResolutionForward` and
  `TestCarryForwardRefusesWithoutTheRetainedBundleItReadsFrom` in
  `internal/controller/prerequisites`, and
  `TestRebaseKeepsEveryRetainedIdentityAndOnlyMovesTheAutomation`,
  `TestRebaseRefusesRetainedSourcesThatAreNotTheirApprovedBytes` and
  `TestPreparationRecoversRetainedSourcesInsteadOfAcquiringThem` in
  `internal/controller/bundlelocal`. For staging and the snapshot:
  `TestSolverMetadataNamesOnlyWhatTheProvidedSolverLoads`,
  `TestStagingSkipsAdvertisedMembersTheSolverNeverOpens` and
  `TestRetainedDatabaseServesOnlyTheStateItWasCopiedFrom`.

Applies to the Linux/amd64 `bundlelocal` and `nativelocal` adapters. Revisit if
a publisher offers a projection identity without payload acquisition, or if a
presence-only readiness ever admits a broken dependency in practice.

Not yet qualified on a real host: that DNF5 with `optional_metadata_types` set
to `filelists`, and DNF4's `fill_sack`, open no repository member outside the
staged set. A solver that does opens a member the staging no longer wrote, and
the resolve fails loudly rather than solving against partial metadata.
