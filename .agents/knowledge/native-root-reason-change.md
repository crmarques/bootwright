# Native root already installed: libdnf5's reason change (history)

**History of a deleted path.** This page describes the controller's own dnf5
solve, which setup v2 replaced with the host's `dnf install` (X88, D152) and
X89 deleted with `solve5`, the controller bridge and
internal/controller/nativelocal. Nothing in this build runs that solve; the
Ceph node's dnf4 core in the collection's native_resolution.py is a separate
path. Keep the page for the libdnf5 behaviour it records, which any future
code that drives libdnf5 directly meets again.
[Controller](../../specs/controller.md#host-packages) owns today's host
package contract.

Observed 2026-10-14 on the owner's Fedora 44 controller (libdnf5 5.4.6.0, rpm
6.0.2) running the first resolve against the Fedora 44 repositories after
[X84](../../specs/milestones/delivered.md#x84--fedora-44-controllers) admitted
the release, unprivileged through the BOOTWRIGHT_NATIVE_RESOLVE_QUALIFY
harness X89 deleted.

- Symptom: setup's native resolve refused `controller.setup: the provided
  native package solver refused its isolated dependency operation` with the
  generic remedy, after the metadata stage (about 40 s). The Go side mapped
  only the first line of the helper's stderr, `refused <class>`, and the class
  here was `internal`, so the exception itself never reached the operator.
- Cause: every baseline root (podman, openssh-clients, nmstate) was already
  installed; podman and openssh-clients carried the reason `Group` after `dnf
  system-upgrade`, nmstate `User`. libdnf5 5.4 answers `goal.add_rpm_install`
  of an installed package with a transaction entry whose action prints as
  `Reason Change` (reason `User`), for every such package, whatever its
  current reason. `solve5` admitted only install, upgrade, downgrade, replaced
  and remove and raised `ValueError("unrequested native transaction action")`.
  The apply-side re-solve from the plan's downloaded RPMs
  (`add_cmdline_packages`) reported the same entry for an installed build, so
  one rule covered both: a reason change moves no file and is neither inbound
  nor outbound.
- Not seen on Fedora 43: its resolves (libdnf5 5.2) passed with
  openssh-clients already installed. Whether 5.2 omitted the entry or those
  hosts never asked for an installed root again was never verified.
- Evidence at the time: the resolve passing after the fix with 3 roots, 3
  sources and 0 actions, and the inspection finding every root by name.

The lesson that outlives the path: a solver that asks libdnf5 to install an
installed package must admit `Reason Change` as an action that moves no file,
and a helper's refusal class alone hides the exception an operator needs.
Revisit only if Bootwright drives libdnf5 directly again.
