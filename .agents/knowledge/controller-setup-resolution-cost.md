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
- File verification belongs to the transaction, over exactly what it installed;
  readiness beside it is presence. Observed on 2026-09-17: the first apply on a
  controller that already carried the whole closure failed at
  `controller-prerequisites` with `controller.setup: Ansible did not complete
  the authorized dependency operation`, and the next apply resolved the same
  block as completed. The frozen transaction held no actions, so nothing was
  installed and the transaction task was skipped; the completion phase still
  refused with `native postcondition`, because `inspect` ran `rpm --verify` over
  the whole frozen closure on every run and reported `......G..  g` for
  `libvirt-daemon-driver-qemu` — which declares `/run/libvirt/qemu/swtpm` as a
  `%ghost` owned by `qemu:qemu` while libvirt's own daemon creates that runtime
  directory as `qemu:tss`. Excluding `%ghost` entries with `--noghost` removed
  that instance; it did not remove the class, because any verify divergence over
  any closure package reproduced the same symptom. `inspect` now reports
  `rootsReady` from presence at the frozen identities alone, and the integrity
  proof moved to `verified_files`, called once by the native transaction over
  its own action targets. Code:
  `ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/native_resolution.py`
  (`inspect`, `verified_files`),
  `ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/native_apply.py`
  (`apply`),
  `ansible/collections/ansible_collections/bootwright/core/plugins/action/controller_protocol.py`
  (`completed`).
- What made that defect expensive to see is worth stating on its own: the block
  had two definitions of satisfied. `Apply` reached the adapter only when a root
  was missing — every later apply took the `settled` fast path, whose condition
  is exactly `Observe`'s — so the strict completion gate ran on the first apply
  and never again. A gate no repeat run can reach cannot be trusted to hold, and
  it fails where it is least expected: on a host that already has everything.
  Presence is now the one bar all three share. Code:
  `internal/controller/clients/capability.go` (`Apply`, `settled`, `Observe`).
- An automation revision still names a new bundle area, because `Digest()`
  hashes every embedded automation file into
  `BootstrapDefinition.AutomationDigest`, which enters the bootstrap digest and
  through it `CatalogDigest`. The collection's root README and CHANGELOG are
  the exception (B16): `Automation()` leaves them out, and so do the digest,
  every comparison against an approved bundle and the projection identity, file
  count and byte total, so a documentation-only build keeps the bundle it
  names. Inspection requires them present as regular non-executable files and
  never compares their bytes, and preparation writes only the ones an area
  lacks. The narrowed digest and identity took new domain versions, so the
  first build carrying them supersedes every retained definition once. An
  automation revision no longer costs a resolution. `validateDefinition`
  checks the provided execution foundation first and reports an
  automation-only mismatch as `ErrAutomationSuperseded` beside
  `ErrBootstrapIncompatible`; `Setup` then carries that resolution forward
  instead of replacing it. `Manager.Rebase` reads the retained sources out of
  the sealed area, projects them under the embedded automation and returns the
  same resolution under its new projection identity, and `Prepare` reads each
  source from that area before contacting a publisher. Nothing is downloaded
  and nothing is solved when the native roots are still installed. The
  superseded area still remains. Code: `ansible/assets.go` (`Digest`,
  `Automation`, `Documentation`),
  `internal/controller/prerequisites/definition.go` (`resolvedContentDigest`),
  `internal/controller/bundlelocal/projection.go` (`embed`, `describe`),
  `internal/controller/bundlelocal/manager.go` (`validateDefinition`,
  `qualifiedFoundation`, `Rebase`, `retainedSource`,
  `attributableDocumentation`, `publishDocumentation`),
  `internal/controller/prerequisites/resolution.go` (`carryForward`, `rebind`).
  Retiring the replaced area is `setup --purge-old-bundles` (X7); a foundation
  shared across automation-only revisions is
  [B44](../../specs/milestones/m1.md#b44).
- Staging acquires every repository member the solver may open, and narrowing
  that set by what a solver is configured to load does not work. Staging only
  `primary` and `filelists` for DNF5, on the reasoning that
  `optional_metadata_types` names everything else it loads, made `load_repos()`
  fail the whole `updates` repository with `RepoDownloadError: Cannot download,
  all mirrors were already tried without success`, because a member it does ask
  for and cannot find fails the repository rather than that member. Measured on
  Fedora 43, the staged set is about 104 MB and `filelists` alone is 72 MB of
  it, so the upside was never large: dropping `other`, the only member proved
  droppable, saves 8 MB. Code:
  `internal/controller/nativelocal/resolver_linux_amd64.go` (`stageRepository`).
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
  `TestDefiniteRefusalRetriesFromRetainedClosure`), the sealed-bundle presence
  test in `internal/controller/bundlelocal` (`TestSealedBundleReadinessIsPresenceOnly`),
  the presence evidence test in `internal/controller/nativelocal`, and the
  helper unit test `test_present_reports_roots_by_name_without_verifying_files`.
  For readiness by presence and the integrity proof beside it:
  `test_inspect_reports_readiness_by_presence_without_verifying_files`,
  `test_verified_files_covers_installed_files_and_no_host_owned_path` and
  `test_integrity_is_proved_over_exactly_what_the_transaction_installed`.
  For carrying a resolution forward:
  `TestSupersededAutomationCarriesTheRetainedResolutionForward` and
  `TestCarryForwardRefusesWithoutTheRetainedBundleItReadsFrom` in
  `internal/controller/prerequisites`, and
  `TestRebaseKeepsEveryRetainedIdentityAndOnlyMovesTheAutomation`,
  `TestRebaseRefusesRetainedSourcesThatAreNotTheirApprovedBytes` and
  `TestPreparationRecoversRetainedSourcesInsteadOfAcquiringThem` in
  `internal/controller/bundlelocal`. For staging and the snapshot:
  `TestStagingRequiresTheMembersEveryPlanDependsOn` in
  `internal/controller/nativelocal` and
  `TestRetainedDatabaseServesOnlyTheStateItWasCopiedFrom`.

Applies to the Linux/amd64 `bundlelocal` and `nativelocal` adapters. Revisit if
a publisher offers a projection identity without payload acquisition, or if a
presence-only readiness ever admits a broken dependency in practice.

Not yet qualified on a real host: that DNF5 with `optional_metadata_types` set
to `filelists`, and DNF4's `fill_sack`, open no repository member outside the
staged set. A solver that does opens a member the staging no longer wrote, and
the resolve fails loudly rather than solving against partial metadata.
