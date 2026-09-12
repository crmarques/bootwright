# Development

The CLI uses [Cobra v1.10.2](https://github.com/spf13/cobra/releases/tag/v1.10.2)
for command and flag definitions, parsing, and help. Cobra was explicitly
selected for this skeleton. Its pflag dependency supplies established Boolean,
scalar, and repeated-flag parsing; the standard library handles encoding and
context-independent value validation.

Desired-state parsing selects unmodified
[go.yaml.in/yaml/v3 v3.0.5](https://github.com/yaml/go-yaml/releases/tag/v3.0.5)
as its stable v3 baseline. The node representation exposes scalar tags, styles,
mapping entries and source coordinates needed by Bootwright's strict decoder.
Bootwright owns schema and lexical validation instead of relying on automatic
Go-value unmarshalling. This choice adds no local parser patch or fork and
does not select a prerelease parser API.

The adapter verifies bounded source bytes before parsing, then checks each
composed document's representation budget before decoding or retaining it.
Its one-document lookahead, parser-error precedence and cooperative
cancellation limits are defined by the
[API parser boundary](../specs/api.md#parser-boundary). The isolated 1 GiB RSS
and 120-second watchdog qualification is a required M1b check; this dependency
choice alone is not evidence that the check passed.

Completion candidates derive from the same Cobra tree. The private adapter
implements the [completion boundary](../specs/cli/commands.md#completion).
Its framework constraints and shell harness findings are indexed in
[CLI adapter knowledge](../.agents/knowledge/cli-adapter-constraints.md).

Build and verification tools are development dependencies; running the
Bootwright executable never installs them. Dependency acquisition during a
build may need network access. See [build knowledge](../.agents/knowledge/build-toolchain.md)
for the exact toolchain selection and release metadata injection points.
`scripts/tools` separately locks `govulncheck` v1.4.0 and its
dependencies, keeping check tooling out of the application module graph.

| Command | Result |
| --- | --- |
| `make build` | Build `bin/bootwright`. |
| `make test` | Run all package tests. |
| `make vet` | Run Go static analysis. |
| `make fmt-check` | Check Go formatting. |
| `make modules-check` | Verify both module locks. |
| `make completion-test` | Source and exercise all four shell integrations. |
| `make vulncheck` | Scan for known reachable vulnerabilities. |
| `make ansible-check` | Run pinned Ansible syntax, lint, collection sanity, unit and safe integration checks. |
| `make check` | Run all verification gates, including completion. |

Completion tests require `bash` on the test runner's `PATH`, or
`BOOTWRIGHT_TEST_BASH` set to an absolute executable path. The other generated
integrations still ship and are covered by the same cases; select them with
`BOOTWRIGHT_TEST_ALL_SHELLS=1`, which additionally requires `zsh`, `fish` and
`pwsh` or their `BOOTWRIGHT_TEST_ZSH`, `BOOTWRIGHT_TEST_FISH` and
`BOOTWRIGHT_TEST_POWERSHELL` overrides. These variables belong only to the test
harness; the Bootwright CLI does not read them. The gate fails if a selected
runtime is missing.

The M1a shell checks use Bash `5.3.0`, Zsh `5.9`, Fish `4.0.2`, and PowerShell
`7.7.0-preview.2` on Linux/amd64. PowerShell completion requires that preview
release or later because stable `7.6` lacks the
[empty-result fix](https://github.com/PowerShell/PowerShell/pull/27398). The
generated script rejects older versions; it does not enable filename fallback.
A broader release-platform matrix remains part of release qualification.

M1d qualifies Bash only. The other shells keep their M1a versions above and are
verified with `BOOTWRIGHT_TEST_ALL_SHELLS=1` when a runner provides them.

Package tests exercise command dispatch, parsing and help precedence, output,
cancellation, and effect boundaries. Completion verification additionally
sources and exercises the generated Bash script, and the other shells when they
are selected. An unavailable selected runtime is missing verification evidence,
not a pass.

The current [M1d delivery](../specs/milestones.md#m1d--bastion-setup) adds
bastion dependency preparation and preflight to the existing admission,
context, rendering and Secret journeys. Go selects and freezes dependencies,
owns confirmation/recovery and orchestrates the embedded Ansible collection.
Ansible installs the selected host packages and target CLIs. The private
Python/Ansible bootstrap is materialized by Go so the playbooks can run.
Setup resolves latest stable dependencies by default; Environment
`spec.dependencyVersions` overrides individual roots, including Python and
Ansible. Public resolver downloads and maintained pip/DNF resolution use
disposable unprivileged staging before the installation plan is confirmed.
Retries use the frozen result. Development checks retain their separate pinned
tool versions for reproducible verification.
The [Ansible check tool lock and setup](../scripts/tools/ansible-check.md)
is separate from the product execution bundle and builds its own pinned
interpreter on first use, so the gate does not depend on the host's Python. `make check` includes its
unprivileged collection gate; it never installs bastion packages.

Bastion tests are unitary and host-independent, following the
[M1d verification model](../specs/milestones.md#m1d--bastion-setup): no package
manager, network, privilege or second operating system, and no virtual machines.
End-to-end acceptance against a real bastion is operator-run. Deferred
lifecycle commands keep their unavailable result until their owning milestone.

Operator-run bastion harnesses are carried in the tree but excluded from
`make test`, so they never report a silent skip as acceptance. They resolve
real publisher metadata or touch the running host and are run by hand as the
code evolves:

| Harness | Selected by |
| --- | --- |
| Current-OS native metadata resolution and frozen file verification | `BOOTWRIGHT_NATIVE_RESOLVE_QUALIFY=1`, `BOOTWRIGHT_NATIVE_INSPECT_PLAN` |
| Bundle acquisition, projection and readiness against real sources | `-tags controllerqualification`, `BOOTWRIGHT_BUNDLE_FIXTURES` |
| Bootstrap publisher resolution and root-invocation isolation | `-tags controllerqualification`, `BOOTWRIGHT_QUALIFY_DYNAMIC_BOOTSTRAP`, `BOOTWRIGHT_QUALIFY_ROOT_RESOLVER` |
| Ansible `controller_native` target, which builds an RPM and drives the host package manager | `BOOTWRIGHT_ANSIBLE_NATIVE_TARGET=1` with `make ansible-check` |
| Ansible `controller_prerequisites` target, which runs the shipped setup playbook and role over the real runner protocol against the host inventory | `BOOTWRIGHT_ANSIBLE_NATIVE_TARGET=1` with `make ansible-check` |

Executed native installation is not covered by any of these; it is a manual
`bastion setup` on a prepared host.

## M1e lifecycle and managed artifact serving

The current [M1e delivery](../specs/milestones.md#m1e--lifecycle-engine-and-managed-artifact-serving)
adds the lifecycle engine and one capability behind it, so `plan`, `status`,
`apply` and `destroy` act on an Environment whose only lifecycle objects are
persistent managed artifact servers. Go owns plans, operation records, leases,
continuation and authorization; the embedded collection installs and removes
the service. The verification model is M1d's: every in-tree test is unitary and
host-independent, creates no container and needs no second host, and
real-system acceptance is operator-run.

The managed artifact server runs `registry.access.redhat.com/ubi9/nginx-124`,
pinned by content digest in
`internal/infrastructureservices/artifactserver/catalog.go`. The compiled
default was resolved from the publisher's `latest` tag on 2026-09-11 and
qualified with podman 5.8.4 on Fedora 43; its runtime constraints are recorded
in [artifact-server knowledge](../.agents/knowledge/artifact-server-nginx-runtime.md).
An `ArtifactServer` that pins `spec.image` uses that reference instead, and any
reference must resolve to an immutable digest.

The embedded collection participates in the dependency-bundle identity, so a
build that changes `ansible/` changes the bundle a context is bound to. Run
`bastion setup --context <name>` again after such a build; `preflight bastion`
reports the incompatible retained bundle and `apply` refuses rather than
executing automation the receipt does not cover.

`make ansible-check` covers the new collection content with the same pinned
syntax, lint, sanity and unit gates as the controller entrypoints. Executed
service effects are not covered by any in-tree gate; these remain operator-run
and are not selectable from a test runner:

| Acceptance | How it is run |
| --- | --- |
| The complete journey in [`examples/lab-artifacts`](../examples/lab-artifacts/README.md): apply, replay, interrupt, continue and destroy against a real container runtime | by hand as root on a prepared bastion |
| SSH placement against a second OS-ready host | by hand, with that host's authored access and bound host key |
