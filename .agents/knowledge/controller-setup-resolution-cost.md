# Controller setup cost

[Controller](../../specs/controller.md#host-packages) owns the rules that
setup installs only what is missing, never checks for updates, and checks
readiness by presence. This page records what setup costs since setup v2 (X88,
2026-10-14, D152) and what it cost before, which is why those rules exist.

## Since X88

Setup resolves nothing. The build embeds a lock that names every wheel, and
the host's dnf owns every package, so setup only asks what is missing:

- **Ready host.** `/etc/os-release`, `rpm -q --whatprovides` for the baseline
  packages run as the invoking user, the setup record, and an ownership walk of
  the selected interpreter and the published runtime. No network request, no
  dnf call, no confirmation and no write. Code: `Missing` in
  `internal/controller/hostdnf/packages_linux_amd64.go`, `Inspect` in
  `internal/controller/venvlocal/guard_linux_amd64.go`. Evidence:
  `TestAReadyHostMakesNoRequestAndNoWrite`,
  `TestPresenceIsRpmWhatprovidesWithoutVersions`.
- **A new build.** The runtime's identity is the lock digest, the Python ABI
  and the interpreter path, and automation is no part of it, so a build with
  the same lock, an `ansible/`-only fix included, needs no setup. A build whose
  lock moved builds a new venv and downloads only the wheels its cache lacks,
  often one file. Evidence: `TestACacheHitMakesNoRequest`.
- **Fresh runtime.** Ten wheels of about 8.7 MB and the 1.8 MB pip wheel
  (the lock of 2026-10-14), at most 4 fetches at a time, a
  `venv --without-pip`, pip from local files with hashes, `compileall`, and
  one `syncfs` before the publishing rename, in place of an `fsync` per file.
  Code: `internal/controller/venvlocal/build_linux_amd64.go`, and the
  publication in `internal/workspace/contextfs/setup_linux_amd64.go`.
- **Missing packages** cost dnf's own time, as `dnf install` would, plus one
  `--assumeno` preview before confirmation.
- **An OS upgrade** that keeps `/usr/bin/python3.X` costs nothing; a Python
  ABI that leaves the lock adds one `python(abi)` install and a new venv.

D152's targets, which the owner's fresh-install run measures and records as
[ledger rows](../../docs/acceptance.md#setup-v2-fresh-install-row) (B555):
fresh setup within 60 s excluding dnf, the ready path and a same-lock build
within 2 s, and a runtime of at most 60 MB and 3,000 files. None is measured
on a host yet.

One lesson carries over unchanged: readiness and completion use the same bar.
The controller stage's `Apply`, `settled` and `Observe` all judge host packages
by presence, so the first apply on a host that already holds everything
succeeds as later ones do. Code: `internal/controller/clients/capability.go`.

## Before X88 (history)

Observed on 2026-09-12 and 2026-09-13 on a ready Fedora 43 controller,
`bootwright setup` took about two minutes, then still almost a minute, to
report `unchanged`; the owner's D152 request of 2026-10-14 still measured more
than three minutes after an earlier build's setup. The causes, each removed
in turn and then deleted with the design:

- Resolution ran before readiness. Bootstrap resolution fetched the uv Python
  metadata, downloaded the whole standalone Python archive, ran pip's resolver
  inside it and downloaded every wheel to compute the projection identity.
  Native resolution staged about 104 MB of repository metadata per Fedora
  release (`filelists` alone 72 MB; staging fewer members made libdnf5 fail
  the whole repository) and copied the RPM database before solving. From
  2026-09-13 a retained resolution was reused when it still served.
- Readiness re-read every retained source, re-extracted the archive and wheels,
  byte-compared the roughly 190 MB per-build bundle area and ran
  `rpm --verify` over every planned package. From 2026-09-13 it was a presence
  walk and `rpm -q` per root.
- Readiness and completion differed. On 2026-09-17 a first apply on a
  controller that already held the closure failed with `native postcondition`,
  because the completion phase ran `rpm --verify` over the frozen closure and
  libvirt's daemon owns a `%ghost` directory `qemu:tss` that the package
  declares `qemu:qemu`; the next apply took the settled path and passed. The
  fix made presence the one bar, which the section above keeps.
- The automation digest entered the bundle identity, so every `ansible/` edit
  named a new bundle area: first a full re-resolution, later a 30 to 60 s
  carry-forward (`Manager.Rebase`), and the replaced area stayed until
  `setup --purge-old-bundles` under a bound of 16 areas.
- Any glibc or libgcc update, and any new OS release, refused until setup ran
  again or a build catalogued the release
  ([runtime isolation](controller-runtime-isolation.md)).

The code went with X88 and X89: internal/controller/bundlelocal's bootstrap,
manager and projection, internal/controller/nativelocal and the Fedora dnf5
solver in the collection's native_resolution.py.

Applies to setup on Linux/amd64. Revisit if a ready-path step starts reading
the network or the RPM database beyond presence, or if the owner's measurement
misses a target.
