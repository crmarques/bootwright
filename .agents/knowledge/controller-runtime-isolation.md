# Controller runtime isolation

Observed while implementing controller setup; [Controller](../../specs/controller.md#supported-host-and-dependency-selection)
owns the requirement that the private Python/Ansible runtime runs isolated from
ambient Python, loader and Ansible configuration and that readiness rejects
additions and substitutions. This page records how that isolation is achieved
and why each mechanism is necessary.

- The private interpreter runs in isolated mode with bytecode and `site`
  initialization disabled and imports only its four explicit private roots.
  Its publication probe, run when the bundle is published rather than on every
  readiness check, verifies every locked distribution, the required
  standard-library modules and the cryptographic extension, so system Python,
  pip configuration, user site, Ansible configuration and environment search
  paths cannot alter the closure. Code:
  `internal/controller/bundlelocal/execution_linux_amd64.go`,
  `internal/controller/bundlelocal/probe_linux_amd64.go`.
- The provided host supplies glibc and libgcc; setup never replaces those
  shared libraries. Their package ownership is frozen with the execution
  profile, and a native transaction that would change them is refused before
  the plan is presented. A dependency release that needs a different foundation
  needs a separately qualified profile. Code:
  `internal/controller/bundlelocal/catalog.go`, `internal/controller/nativelocal/resolver_linux_amd64.go`.
- Execution verifies the catalogued host loader, ELF library files and aliases
  under the native package read lock, then invokes that loader directly with the
  cache disabled, hardware-capability selection disabled, a fixed library path
  and an explicit dependency-ordered preload list. A nonempty system loader
  preload configuration refuses. The baseline probe holds the read lock until it
  has exited; the Ansible runner acknowledges its loaded state before Go
  releases that lock, and package installation then acquires native transaction
  coordination and revalidates the selected foundation. No ambient loader cache,
  library search variable or preload configuration can select executable code.
  Code: `internal/controller/bundlelocal/execution_linux_amd64.go`,
  `internal/controller/ansiblelocal/runner_linux_amd64.go`.
- Evidence: the `internal/controller/bundlelocal` package tests, and the
  operator-run `-tags controllerqualification` harnesses listed in
  [development](../../docs/development.md).

Applies to RHEL 9 and Fedora on Linux/amd64. Revisit when a dependency release
changes its foundation requirements or when another host family is qualified.
