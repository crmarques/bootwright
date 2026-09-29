# Development

This guide is for changing Bootwright: the pinned toolchain, the verification
tiers and the test harnesses. Preparing a host, running the lab journeys and
recording a run are in the [operator guide](operator-guide.md);
[milestones](../specs/milestones.md#completion-and-verification) owns which
gate a slice needs.

## Toolchain

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
[API parser boundary](../specs/api/input.md#parser-boundary).
`TestParserQualification` runs each budget case in an isolated child process
against the 1 GiB RSS and 120-second limits; it runs with the package tests on
Linux and is excluded from race builds.

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
Development checks keep their own pinned tool versions for reproducible
verification: the [Ansible check tool lock and setup](../scripts/tools/ansible-check.md)
is separate from the product execution bundle and builds its own pinned
interpreter on first use, so the collection gate does not depend on the host's
Python.

The embedded collection participates in the dependency-bundle identity, so a
build that changes `ansible/` changes the bundle a live context is bound to;
[changing the build between runs](operator-guide.md#changing-the-build-between-runs)
says what that requires.

## Verification tiers

| Command | Result |
| --- | --- |
| `make build` | Build `bin/bootwright`, stamped with the version, commit and source state of the checkout it was built from. |
| `make quick` | Inner loop: formatting, vet, the architecture suite, and the packages this branch changed with their dependents. |
| `make docs-check` | Check guidance links, anchors, cited paths and tests, documented command lines, skill frontmatter and byte budgets, and that each milestone's Status Delivery agrees with its page and every item has one row, one detail section and no delivered record (`TestDocsMilestonePagesAgreeWithTheirStatus`). |
| `make test` | Run all package tests. |
| `make vet` | Run Go static analysis. |
| `make fmt-check` | Check Go formatting. |
| `make modules-check` | Verify both module locks. |
| `make tidy-check` | Check that both module files are tidy. |
| `make completion-test` | Source and exercise the Bash integration, and the Zsh, Fish and PowerShell integrations with `BOOTWRIGHT_TEST_ALL_SHELLS=1`. |
| `make race` | Run the race detector over the concurrent lifecycle, privilege and composition packages. |
| `make vulncheck` | Scan for known reachable vulnerabilities. |
| `make ansible-check` | Run pinned Ansible syntax, lint, collection sanity, unit and safe integration checks; `./scripts/ansible-check --suite <name>` runs one suite locally. |
| `make check-offline` | Run every gate that needs no network once caches are warm, and name the gates it left unrun. |
| `make check` | Run all verification gates, including completion. CI runs it on every push and pull request, and `make race` nightly. |

Reusable check caches live in the directory `scripts/cache-dir` prints: the
primary checkout's `.cache`, shared by every worktree of the clone.

In-tree tests are unitary and host-independent, as the
[completion and verification](../specs/milestones.md#completion-and-verification)
rule requires: no package manager, network, privilege, second operating system
or virtual machine. Package tests exercise command dispatch, parsing and help
precedence, output, cancellation, and effect boundaries. `make check` includes
the unprivileged collection gate and never installs controller packages. An
executed native installer, container or guest is qualified only by the
operator-run harnesses below and the journeys in the
[operator guide](operator-guide.md#run-a-lab-journey).

## Test harnesses

### Shell completion

Completion tests require `bash` on the test runner's `PATH`, or
`BOOTWRIGHT_TEST_BASH` set to an absolute executable path. The other generated
integrations still ship and are covered by the same cases; select them with
`BOOTWRIGHT_TEST_ALL_SHELLS=1`, which additionally requires `zsh`, `fish` and
`pwsh` or their `BOOTWRIGHT_TEST_ZSH`, `BOOTWRIGHT_TEST_FISH` and
`BOOTWRIGHT_TEST_POWERSHELL` overrides. These variables belong only to the test
harness; the Bootwright CLI does not read them. The gate fails if a selected
runtime is missing.

The shell integrations were qualified against Bash `5.3.0`, Zsh `5.9`, Fish
`4.0.2`, and PowerShell `7.7.0-preview.2` on Linux/amd64 when M1a delivered
them. `make check` exercises Bash only; the other shells are verified when a
runner provides them. PowerShell completion requires that preview release or
later because stable `7.6` lacks the
[empty-result fix](https://github.com/PowerShell/PowerShell/pull/27398). The
generated script rejects older versions; it does not enable filename fallback.
A broader release-platform matrix remains part of release qualification.

Completion verification sources and exercises the generated Bash script, and
the other shells when they are selected. An unavailable selected runtime is
missing verification evidence, not a pass.

### Operator-run controller harnesses

Operator-run controller harnesses are carried in the tree but excluded from
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
`setup` on a prepared host, as the
[operator guide](operator-guide.md#prepare-a-host) describes.

## Qualified hosts and images

`setup` qualifies Fedora 43 and RHEL 9.8 on Linux/amd64: the compiled catalog
`internal/controller/bundlelocal/catalog.json` records exact dependency
artifacts for those releases only, and any other release refuses.

Setup's private runtime is qualified for `ansible-core` 2.21 at its latest
stable patch, on controller CPython 3.12, 3.13 or 3.14 at the latest patch of
the newest. Managed hosts run Python 3.9 through 3.14, the target versions in
ansible-core 2.21's
[test matrix](https://github.com/ansible/ansible/blob/stable-2.21/test/lib/ansible_test/_util/target/common/constants.py),
so the collection's modules and module utilities keep to 3.9 grammar. The
minor is one constant, in `internal/controller/prerequisites/qualified.go`.
`TestQualifiedAnsibleMinorAgreesEverywhere` holds every statement of it
together: the collection's `requires_ansible`, both `ansible-core` pins, the
ansible-lint supported list, the sanity ignore file, the compiled catalog and
this paragraph. Qualifying another minor changes all of them in one commit.

Each managed service runs one container image pinned by content digest in its
capability's `catalog.go`. All four were resolved from their publisher's
current stable tag and qualified by hand against podman 5.8.4 on Fedora 43 on
the date below, before any role was written:

| Kind | Image | Qualified |
| --- | --- | --- |
| `ArtifactServer` | `registry.access.redhat.com/ubi9/nginx-124` | 2026-09-11 |
| `Proxy` | `docker.io/ubuntu/squid` (24.04) | 2026-09-13 |
| `DNSServer` | `docker.io/4km3/dnsmasq` | 2026-09-13 |
| `NTPServer` | `docker.io/dockurr/chrony` (chrony 4.9) | 2026-09-13 |

Their runtime constraints are recorded in
[artifact-server knowledge](../.agents/knowledge/artifact-server-nginx-runtime.md)
and [network-service knowledge](../.agents/knowledge/managed-network-service-runtime.md).
The [API](../specs/api/infrastructure-services.md) owns the `spec.image`
override.

A realized Machine's emulated management controller runs the same way, pinned
in `internal/substrate/libvirt/catalog.go`:

| Role | Image | Qualified |
| --- | --- | --- |
| Emulated BMC | `quay.io/metal3-io/sushy-tools` (sushy-tools 2.2.1.dev14) | 2026-09-15 |

The image was resolved, pulled and started against session libvirt, which is
what [emulated-BMC knowledge](../.agents/knowledge/sushy-tools-emulated-bmc.md)
records, including the stock command the unit must not use. The Redfish surface
this contract drives — the `Systems` collection, virtual media insert and eject,
boot override, power and basic authentication — and `mkksiso` against RHEL 9.8
boot media were qualified by the first real `lab-rhel` install rather than
ahead of it, and the guest that install produced booted and served SSH. One part
of that surface is still unproved against this image: the
`EthernetInterfaces` collection the
[physical target proof](../specs/substrates.md#physical-machine-realization)
reads, which only the
[lab-baremetal rehearsal](../examples/lab-baremetal/README.md#rehearsing-it-without-hardware)
exercises.
