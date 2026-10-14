# Native root already installed: libdnf5's reason change

Observed 2026-10-14 on the owner's Fedora 44 controller (libdnf5 5.4.6.0, rpm
6.0.2) running the first resolve against the Fedora 44 repositories after
[X84](../../specs/milestones/delivered.md#x84--fedora-44-controllers) admitted
the release, unprivileged through `BOOTWRIGHT_NATIVE_RESOLVE_QUALIFY=1`.
[Controller](../../specs/controller.md#supported-host-and-dependency-selection)
owns the native resolution contract; this page records the refusal, its cause
and how to read the helper's own error.

- Symptom: setup's native resolve refuses `controller.setup: the provided
  native package solver refused its isolated dependency operation` with the
  generic remedy, after the metadata stage (about 40 s). The Go side maps only
  the first line of the helper's stderr, `refused <class>`, and the class here
  is `internal`, so the exception itself never reaches the operator. Code:
  `internal/controller/nativelocal/resolver_linux_amd64.go` (`run`).
- Cause: every baseline root (podman, openssh-clients, nmstate) was already
  installed; podman and openssh-clients carried the reason `Group` after `dnf
  system-upgrade`, nmstate `User`. libdnf5 5.4 answers `goal.add_rpm_install`
  of an installed package with a transaction entry whose action prints as
  `Reason Change` (reason `User`), for every such package, whatever its
  current reason. `solve5` admitted only install, upgrade, downgrade, replaced
  and remove and raised `ValueError("unrequested native transaction action")`.
  The apply-side re-solve from the plan's downloaded RPMs
  (`add_cmdline_packages`) reports the same entry for an installed build, so
  one rule covers both: a reason change moves no file and is neither inbound
  nor outbound. Code:
  `ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/native_resolution.py`
  (`solve5`); guard `Dnf5Transaction` in
  `tests/unit/plugins/module_utils/test_native_resolution.py`.
- Not seen on Fedora 43: its resolves (libdnf5 5.2) passed with
  openssh-clients already installed. Whether 5.2 omitted the entry or those
  hosts never asked for an installed root again is unverified.
- Reading the helper's refusal: stage each repository's `repomd.xml` and the
  members `stageRepository` selects under `<work>/repositories/<id>/repodata/`,
  then run `/usr/bin/python3 -I -B native_resolution.py <work>` with the
  request JSON (`operation`, `platform`, `requirements`, `versions`, `egress`,
  `repositories` with `id`, `baseURL`, `metadataSHA256`, `localPath`,
  `signer`) on stdin and `HOME=<work>/home`, `LC_ALL=C`; omit `snapshot` and
  the helper copies the RPM database itself. Its stderr holds the class and the
  exception. The end-to-end check is `TestNativeCurrentOSResolution` with
  `BOOTWRIGHT_NATIVE_RESOLVE_PLAN` set, then `TestNativeCurrentOSInspection`
  over that plan.
- Evidence: the resolve passing after the fix with 3 roots, 3 sources and 0
  actions, and the inspection finding every root by name.

Applies to the dnf5 path (Fedora). Revisit when libdnf5 adds another action
that moves no file, or when native installation moves to the host's own `dnf`.
