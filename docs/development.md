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

The CLI presentation adapter uses
[Charm ANSI v0.11.7](https://github.com/charmbracelet/x/tree/ansi/v0.11.7/ansi)
for ANSI-aware cell measurement, wrapping and truncation. Styling and semantic
blocks stay in `internal/cli`; terminal and environment facts are injected by
the composition root. Color never enters domain results or raw exports.

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
`scripts/tools` separately locks `govulncheck` v1.8.0, `goimports` from
`golang.org/x/tools` v0.51.0, and their dependencies, keeping check tooling out
of the application module graph. Run import formatting through
`./scripts/goimports`; the architecture suite also rejects duplicate imports
and mixed or misordered standard-library and module groups.
Development checks keep their own pinned tool versions for reproducible
verification: the [Ansible check tool lock and setup](../scripts/tools/ansible-check.md)
is separate from the product's [runtime lock](#lock-refresh-and-tested-hosts)
and builds its own pinned interpreter on first use, so the collection gate does
not depend on the host's Python.

Human-authored YAML follows the
[source formatting standard](../.agents/skills/code-implementation/references/formatting.md#yaml):
community indentation and scalar conventions, plus one empty line between
logical blocks. The examples [.yamllint](../examples/.yamllint) checks the mechanical rules;
review the block grouping and preserve parsed values when formatting. Once
the [Ansible check environment](../scripts/tools/ansible-check.md) is prepared,
check tracked examples with its pinned yamllint (ignored local inputs are
excluded):

```sh
yaml_python="$(./scripts/cache-dir)/ansible-check/venv/bin/python"
git ls-files -z 'examples/**/*.yaml' | xargs -0 "$yaml_python" -I -m yamllint --strict -c examples/.yamllint
```

The embedded collection is no part of any setup identity: each command that
runs Ansible unpacks the build's own automation, so a build that changes only
`ansible/` needs a build and an `apply`, never a `setup`;
[changing the build between runs](operator-guide.md#changing-the-build-between-runs)
says what a lock change requires.

## Verification tiers

| Command | Result |
| --- | --- |
| `make build` | Build `bin/bootwright`, stamped with the version, commit and source state of the checkout it was built from. |
| `make focus` | Probe tier: the narrowest checks of the branch's diff in concurrent lanes, stopping each at its first failure: gofmt of the changed Go files, vet of every affected package, the changed packages' tests, the collection tests that import or name a changed plugin, narrowed lint, syntax and sanity, and docs-check for guidance. It is no gate; [the tiers](../.agents/skills/code-implementation/references/verification.md) say what it leaves to `make check`. |
| `make quick` | Inner loop: formatting, vet, the architecture suite, and the packages a change on this branch reaches, whether to Go source, an embedded asset or test data, as [architecture verification](../specs/architecture.md#architecture-verification) selects them. |
| `make docs-check` | Check guidance links, anchors, cited paths and tests, documented command lines, skill frontmatter and byte budgets over the files Git tracks, every capability's refusal table and the engine's table of unclaimed objects against the code that refuses, beside the working-tree walks [architecture verification](../specs/architecture.md#architecture-verification) names, and hold the pages to the [milestone rules](../specs/milestones.md) (`TestDocsMilestonePagesAgreeWithTheirStatus`): each milestone's Status Delivery agrees with its page and its Next names only open work; a done milestone keeps no item or planned slice and requires only done milestones; every item has one row of a known Kind, one detail section and no delivered record, and leaves its page when it completes; each planned slice has one row of a known Kind naming only its page's items; and an item's Delivery names a planned slice exactly when that slice's row lists the item, with no empty slice suffix. |
| `make test` | Run all package tests. |
| `make vet` | Run Go static analysis on linux/amd64 and on darwin/arm64, where each `*_unsupported.go` refusal stands in for its Linux adapter. |
| `make fmt-check` | Check Go formatting. |
| `make modules-check` | Verify both module locks. |
| `make tidy-check` | Check that both module files are tidy. |
| `make completion-test` | Source and exercise the Bash integration, and the Zsh, Fish and PowerShell integrations with `BOOTWRIGHT_TEST_ALL_SHELLS=1`. |
| `make race` | Run the race detector over the concurrent lifecycle, privilege and composition packages. |
| `make vulncheck` | Scan for known reachable vulnerabilities. |
| `make ansible-check` | Run pinned Ansible syntax, lint, collection sanity, unit and safe integration checks; `./scripts/ansible-check --suite <name>` runs one suite locally, and `--suite syntax\|lint\|sanity\|units PATH...` narrows one to collection paths. |
| `make check-offline` | Run every gate that needs no network once caches are warm, and name the gates it left unrun. |
| `make check` | Run all verification gates, including completion. CI runs it, and `make race` beside it as its own job, on every push and pull request. |

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
`BOOTWRIGHT_TEST_BASH` set to an absolute executable path. Set
`BOOTWRIGHT_TEST_ZSH` to an absolute Zsh executable to additionally exercise
Zsh, including the single-directory no-space behavior. The other generated
integrations use the same cases with `BOOTWRIGHT_TEST_ALL_SHELLS=1`, which
requires `zsh`, `fish` and `pwsh` or their `BOOTWRIGHT_TEST_ZSH`,
`BOOTWRIGHT_TEST_FISH` and `BOOTWRIGHT_TEST_POWERSHELL` overrides. These variables belong only to the test
harness; the Bootwright CLI does not read them. The gate fails if a selected
runtime is missing.

The shell integrations were qualified against Bash `5.3.0`, Zsh `5.9`, Fish
`4.0.2`, and PowerShell `7.7.0-preview.2` on Linux/amd64 when M1a delivered
them. `make check` exercises Bash only; the other shells are verified when a
runner provides them. PowerShell completion requires that preview release or
later because stable `7.6` lacks the
[empty-result fix](https://github.com/PowerShell/PowerShell/pull/27398). The
generated script rejects older versions; it does not enable filename fallback.
X44 additionally qualified the Bash and Zsh directory no-space cases on Zsh
`5.9.2`, built from the [official release](https://www.zsh.org/pub/zsh-5.9.2.tar.xz)
with its [published checksum](https://www.zsh.org/mla/announce/msg00136.html).
A broader release-platform matrix remains part of release qualification.

Completion verification sources and exercises the generated Bash script, and
the other shells when they are selected. An unavailable selected runtime is
missing verification evidence, not a pass.

### Operator-run controller harnesses

Operator-run controller harnesses are carried in the tree but excluded from
`make test`, so they never report a silent skip as acceptance. They touch the
running host and are run by hand as the code evolves:

| Harness | Selected by |
| --- | --- |
| Ansible `native_transaction` target, which builds an RPM and drives a RHEL 9 host's package manager into an isolated installroot, the Ceph node's package path | `BOOTWRIGHT_ANSIBLE_NATIVE_TARGET=1` with `make ansible-check` |
| Ansible `controller_prerequisites` target, which runs the shipped controller playbook and role over the real runner protocol with one `controller-prerequisites-v6` request, publishing a synthetic client from a source already retained in a temporary tool area and replaying it | `BOOTWRIGHT_ANSIBLE_CONTROLLER_TARGET=1` with `./scripts/ansible-check --suite integration` |

`scripts/runtime-lock verify` checks the lock against its publisher with
network, as [below](#lock-refresh-and-tested-hosts). A controller's host
package installation and runtime build are covered by none of these; they are
a manual `setup` on a host missing something, as the
[operator guide](operator-guide.md#prepare-a-host) describes, recorded as the
[setup row](acceptance.md#setup-v2-fresh-install-row).

## Lock refresh and tested hosts

`setup` admits any Fedora or RHEL host on Linux/amd64, a derivative whose
`/etc/os-release` `ID_LIKE` names one included, that has a qualified `rpm` and
`dnf`, a locked Python ABI installed or offered and a glibc at or above the
lock's floor ([controller](../specs/controller.md#supported-hosts)). The lock's
`tested` list names the releases a build was tested on, Fedora 43 and 44 and
RHEL 9 and 10 today; a release outside it gets the informational
`controller.untested-release` line and is never refused. The
[acceptance ledger](acceptance.md#ledger) records each release setup and the
controller stage actually ran on.

The controller runtime is ansible-core 2.21 at the patch
`internal/controller/runtimelock/lock.json` pins, on controller CPython 3.12,
3.13 or 3.14, the host's own interpreter at `/usr/bin/python3.X`. Managed
hosts run Python 3.9 through 3.14, the target versions in ansible-core 2.21's
[test matrix](https://github.com/ansible/ansible/blob/stable-2.21/test/lib/ansible_test/_util/target/common/constants.py),
so the collection's modules and module utilities keep to 3.9 grammar, the
sanity suite imports each of them under the CPython 3.9 that
`scripts/tools/ansible-check-floor-interpreter.json` pins, and the units suite
runs their unit tests under it. The lock's ansible-core is the one version:
`TestTheLockCarriesOneAnsibleCoreEverywhere` holds it to
`scripts/tools/ansible-requirements.txt` and to the collection's
`requires_ansible`, `TestTheLintAndSanityConfigurationsNameTheLockedMinor`
holds the ansible-lint supported list and the sanity ignore file to its minor,
and `TestTheDevelopmentGuideStatesTheLockedMinorAndABIs` holds this paragraph
to the minor and the locked interpreter ABIs. Qualifying another minor
changes all of them in one commit.

To refresh the lock, run, with network:

```sh
scripts/runtime-lock generate --ansible-core 2.21 --python 3.12,3.13,3.14 --installer pip==<version> --output internal/controller/runtimelock/lock.json
scripts/runtime-lock verify
```

`generate` resolves each ABI's binary wheels with the pinned installer,
downloads every file to confirm its size and SHA-256, computes the glibc floor
from the highest manylinux tag and carries the `tested` list forward; add a
release to it with `--tested fedora:43,44 --tested rhel:9,10` once it ran.
Move `scripts/tools/ansible-requirements.txt` to the same ansible-core patch in
the same commit. `verify` downloads every locked URL and compares it, and
`outdated` prints newer 2.21 patches and dependency releases without gating;
`.github/workflows/runtime-lock.yml` runs `verify` when the lock or the tool
changes and weekly, with `outdated` as an advisory summary.

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
[lab-baremetal rehearsal](../examples/machines/lab-baremetal/README.md#rehearsing-it-without-hardware)
exercises.
