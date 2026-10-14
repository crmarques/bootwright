# Controller runtime isolation

[Controller](../../specs/controller.md#the-controller-runtime) owns how the
controller runtime is built and launched, and
[security](../../specs/security.md#the-controller-hosts-attacker-model) owns
the attacker model it answers. This page records what the isolation is since
setup v2 (X88, 2026-10-14, D152), why each mechanism is there, and what the
earlier design did and why it went.

## Since X88

The attacker is an unprivileged local account. Root, and anything only root
can write, is trusted, because dnf already runs the host's
`/usr/bin/python3` as root for every package install. So the isolation proves
that nothing another account can write reaches the interpreter, and that no
ambient configuration selects code; it does not pin bytes.

- **Ownership, not digests.** The interpreter, its resolved target, its
  standard library and `lib-dynload`, and the runtime directory chain must be
  owned by root with no group or other write on them or any directory above.
  Setup walks them when it selects the interpreter; every launch walks them
  again. Code: `ownedExecutable` and `ownedDirectory` in
  `internal/controller/venvlocal/walk_linux_amd64.go`, `qualify` in
  `internal/controller/venvlocal/select_linux_amd64.go`.
- **No ambient search path.** Every launch is `bin/python -I -B` with only
  `LC_ALL`, `LANG`, `PATH`, `HOME` and `TMPDIR` set. `-I` ignores `PYTHON*`
  variables and the user site; the venv's `pyvenv.cfg` excludes the system
  site-packages; the build refuses a `*.pth`, `sitecustomize.py` or
  `usercustomize.py` in site-packages, the startup hooks `site` still
  honours. `-B` writes no bytecode, and the build compiles unchecked-hash
  bytecode once, so a launch reads root-owned `.pyc` files and no timestamp
  decides whether they are used. Code:
  `internal/controller/venvlocal/build_linux_amd64.go`.
- **The supervisor checks it is that runtime.** `controller_runtime` in
  `ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/controller_supervisor.py`
  requires `sys.flags.isolated`, `sys.flags.dont_write_bytecode` and a venv
  (`sys.prefix != sys.base_prefix`), so a launch that lost a flag refuses
  instead of running with ambient paths.
- **A shared lock per block, not the RPM lock.** `WithPython` holds
  `flock(LOCK_SH)` on the runtime's `.lock` for the whole Ansible block, so a
  retirement (X90) never removes a runtime in use, and the operator's own dnf
  is never blocked. A Python update during a run can replace standard-library
  files under it; that risk is accepted, and setup's import probe rebuilds a
  runtime an update broke. Code: `WithPython` in
  `internal/controller/venvlocal/guard_linux_amd64.go`.
- **The host's TLS policy applies.** No `OPENSSL_CONF` override and no loader
  invocation: Python's `ssl` follows the host's OpenSSL and crypto-policy (W4).
- **Automation is staged per invocation.** The embedded tree is written into
  root 0700 staging and read back against the automation digest, and every
  adapter process inherits an exclusive lock on it, so the sweep never removes
  a tree a descendant still runs from. Code:
  `internal/controller/stagelocal/automation_linux_amd64.go`.
- Evidence: `TestAnInterpreterBelowAWritableDirectoryIsSkipped`,
  `TestTheStdlibDirectoriesAreWalked`, `TestTheVenvHoldsNoPthOrStartupHook`,
  `TestBytecodeIsUncheckedHash`, `TestTheGuardHoldsTheRuntimeSharedForTheBlock`,
  `TestTheGuardRefusesARuntimeThatNoLongerVerifies`,
  `TestADescendantKeepsTheTreeLocked` and the collection unit test
  `test_only_the_isolated_controller_venv_runs_the_supervisor`.

## Before X88 (history)

From M1d (2026-09) until X88, setup projected a private python-build-standalone
interpreter and the locked wheels into a per-build execution bundle of about
190 MB, and pinned the host's foundation byte for byte:

- The interpreter ran with `-I -B -S` and an explicit list of four private
  import roots, and a publication probe imported every locked distribution.
- glibc and libgcc were catalogued per Fedora and RHEL release, by file digest,
  link target and single-link rule, and a native transaction that would change
  them was refused. The launch invoked the catalogued `ld-linux` directly with
  the cache and hardware-capability selection disabled, a fixed library path
  and an explicit preload list, and refused a non-empty /etc/ld.so.preload.
- The RPM database's read lock was held from the probe until the runner
  acknowledged its loaded state, so no package transaction could replace the
  foundation under a launch.
- `OPENSSL_CONF=/dev/null` kept the host's OpenSSL configuration out.

The pin defended nothing the attacker model admits: only root can replace those
files, and root is trusted. It cost a code change per OS release (Fedora 44
refused until X84 added it on 2026-10-14), a full re-setup after any glibc
update, and an RPM lock that blocked the operator's dnf during a run. D152
dropped it (clause 1) and kept root ownership with no group or other write,
extended to the standard library and to the re-executed binary (B548). The
code went with X88 and X89: internal/controller/bundlelocal's execution,
bootstrap, catalog and probe files, internal/controller/nativelocal and
scripts/foundation-catalog.

Applies to Linux/amd64 hosts of the Fedora and RHEL families. Revisit if the
attacker model admits a root-owned path an unprivileged account can influence,
or if a FIPS-qualified runtime (B331) needs a pinned crypto module.
