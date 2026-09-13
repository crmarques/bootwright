# Bastion setup resolution cost

Observed on 2026-09-12 on a ready Fedora 43 bastion: `bootwright bastion setup`
took about two minutes to report `unchanged`, while `preflight bastion` on the
same host takes seconds. [Controller](../../specs/controller.md#supported-host-and-dependency-selection)
owns the rule that setup reuses a serving retained resolution and never checks
for newer releases; this page records why resolution is expensive, which is
the reason that rule matters.

- Setup used to resolve dependencies before checking readiness, so every run
  paid the full resolution cost even with nothing to do. Resolution now runs
  only when no retained resolution serves the intent or a selected native root
  is missing. Code: `internal/controller/prerequisites/service.go` (`Setup`,
  `resolutionRequired`), `internal/controller/prerequisites/resolution.go`
  (`bootstrapClosure`, `supersededLatest`).
- Bootstrap resolution is priced as acquisition, not as a metadata round-trip:
  it fetches the uv Python metadata, downloads the complete standalone Python
  archive, runs the pip resolver inside that projection, then downloads every
  resolved wheel to compute the projection identity. Code:
  `internal/controller/bundlelocal/bootstrap.go` (`Resolve`).
- Native resolution stages the `primary`, `filelists` and related members of
  every approved repository and copies the RPM database before the DNF helper
  solves. Code: `internal/controller/nativelocal/resolver_linux_amd64.go`
  (`Resolve`, `stageRepository`).
- Native readiness (`Check`) also snapshots the RPM database and runs the
  helper, but reads no repository; it is the per-run floor whenever a container
  runtime is selected.
- Evidence: `internal/controller/prerequisites` tests asserting that a ready
  bastion, a retry after a terminal failure and a missing native root contact
  no bootstrap or tool publisher
  (`TestLatestSetupResolvesOnceAndReusesRetainedNoop`,
  `TestTargetToolsResolveBeforeConfirmationAndInstallEvenWithReadyRuntime`,
  `TestDefiniteRefusalRetriesFromRetainedClosure`).

Applies to the Linux/amd64 `bundlelocal` and `nativelocal` adapters. Revisit if
a publisher offers a projection identity without payload acquisition.
