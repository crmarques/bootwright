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
`ansible/` needs a build and an `apply`, never a `controller install`;
[changing the build between runs](operator-guide.md#changing-the-build-between-runs)
says what a lock change requires.

## Building in a container

A host with only Docker or Podman builds the executable from the
[Dockerfile](../Dockerfile), which runs `make build` in a Go image pinned by
tag and digest, the tag naming the `go` directive of [go.mod](../go.mod):

```sh
make container-build
make container-build CONTAINER_OUT=dist CONTAINER_ENGINE=docker
```

It writes `bootwright`, its bill of materials `bootwright.cdx.json` and its
third-party notices `bootwright.notices.txt` into `CONTAINER_OUT`, `bin` by
default, with Podman when both engines are installed. The engine exports into a fresh private directory
inside `CONTAINER_OUT`, and the recipe then moves `bootwright` into place, so a
shared directory such as `/tmp`, whose other entries the engine's exporter
would try to read or replace, works as well; a `bootwright`, bill of materials
or notices file there that is a directory or another user's file is refused
before the build starts, and an export that lacks the notices fails the recipe. The result is the same static executable a host
build makes, for the building host's architecture, stamped with the identity
`make build` would give the checkout. A host without `make` passes the version
itself: `docker build --output type=local,dest=bin .`;
from a checkout whose `.git` is a directory it then stamps itself in the build.

The build runs `make wheels` while the proxy settings below are set, into a
cache mount of its own, so a warm cache fetches no wheel, and its vulnerability
gate keeps the clients' release files in a second cache mount. Go's module and
build caches are cache mounts the engine keeps between builds:
on the reference host a cold build took 31 seconds, a rebuild after a source
change 8, and a build that leaves go.mod unchanged downloads nothing. The
compile runs with `GOPROXY=off`; its vulnerability gate still reaches
`vuln.go.dev` and `api.osv.dev` through the same proxy settings and CA, and
`make container-build VULNCHECK=skip` builds without them, stamped
`unchecked`. `podman image prune --build-cache` or
`docker builder prune` empties the caches.

Behind a proxy, set it in the environment or on the command line:

```sh
HTTPS_PROXY=http://proxy.example:3128 NO_PROXY=.example make container-build
make container-build HTTPS_PROXY=http://proxy.example:3128 PROXY_CA=corp-ca.pem
```

`HTTP_PROXY`, `HTTPS_PROXY` and `NO_PROXY`, in either case, and `GOPROXY`,
`GOSUMDB`, `GOPRIVATE`, `GONOSUMDB` and `GONOPROXY` reach the build whenever
they are set, so a host `GOPROXY` reaches it as well. Any of them may carry a
credential, such as `http://user:password@proxy.example:3128`, and an engine
records build arguments: Podman in the build stage's image metadata, Docker
in the build record that `docker buildx history inspect` shows, values
included. The recipe therefore writes them to a file only its user can read,
passes it as the `build-env` build secret and removes it, and echoes only
their names; the build reads them for the module and wheel downloads and the
vulnerability gate, and no image,
cache or build record keeps them. `PROXY_CA` names the CA certificate of a
proxy that intercepts TLS, passed the same way as the `proxy-ca` secret.
`TestContainerBuildPassesSettingsOnlyAsASecret` keeps every other value out
of the build arguments. A build without `make` passes its own settings file,
one `NAME=value` per line: `--secret id=build-env,src=<file>`.

The engine pulls the base image through the proxy of its caller's
environment; a Docker daemon also uses its own proxy configuration.
The default image is `docker.io/library/golang:<go version>-trixie` followed
by `@sha256:` and the digest the registry serves for that tag, so the build
trusts the content a reviewer read and not a tag the publisher rebuilds.
`GO_IMAGE` replaces the image, for example with
`<mirror>/golang:<tag>@sha256:<digest>` from a registry mirror pinned by
digest; it is a build argument and so must carry no credential, which an
engine login supplies instead.

`TestTheContainerBuildPinsTheGoImageOfGoMod` holds the `go` directive to the
toolchain [scripts/go](../scripts/go) selects and to the version in the
Dockerfile's image tag, and refuses a default that is not pinned by digest, so
the image and host builds use the same Go. To move Go, change the directive and
`scripts/go` together, then replace the tag and digest in the Dockerfile with
those of `golang:<new version>-trixie`: the digest is the
`Docker-Content-Digest` of the tag's manifest list, which
`HEAD /v2/library/golang/manifests/<tag>` on `registry-1.docker.io` returns
with an anonymous pull token, and which `skopeo inspect --format '{{.Digest}}'`
or `docker buildx imagetools inspect` print as well.

## Verification tiers

| Command | Result |
| --- | --- |
| `make build` | Run `make wheels`, then build `bin/bootwright`, stamped with the version, commit and source state of the checkout it was built from, and write beside it its bill of materials `bin/bootwright.cdx.json` and its third-party notices `bin/bootwright.notices.txt`; the build fails when a component names no license or has no text to reproduce. |
| `make licenses` | Read each Go module's license file from the module cache and the toolchain's own, classify it against known texts and write `cmd/bootwright/go-licenses.json`; a text it does not know fails; [license sources](#license-sources-and-notices). |
| `make licenses-check` | Recompute that table from the module cache and fail when `cmd/bootwright/go-licenses.json` differs; `make check` runs it. |
| `make wheels` | Place the runtime lock's wheels where the executable embeds them, each checked against the lock, from the shared cache or, on a cold cache, `files.pythonhosted.org`; [carried wheels](../specs/controller.md#the-runtime-lock). |
| `make container-build` | Build the same executable into `CONTAINER_OUT` with Docker or Podman only; [building in a container](#building-in-a-container). |
| `make focus` | Run the inner loop: the narrowest checks of the branch's diff, in concurrent lanes. It is no gate; [the tiers](../.agents/skills/code-implementation/references/verification.md#focus) say what it selects and what it leaves to `make check`. |
| `make quick` | Run formatting, vet, the architecture suite and the packages a change on this branch reaches; [the tiers](../.agents/skills/code-implementation/references/verification.md) say when to run it and [architecture verification](../specs/architecture.md#architecture-verification) how it selects them. |
| `make docs-check` | Check guidance links, anchors, cited paths and tests, documented command lines, skill frontmatter and byte budgets over the files Git tracks, every capability's refusal table and the engine's table of unclaimed objects against the code that refuses, beside the working-tree walks [architecture verification](../specs/architecture.md#architecture-verification) names, and hold the pages to the [milestone rules](../specs/milestones.md) (`TestDocsMilestonePagesAgreeWithTheirStatus`): each milestone's Status Delivery agrees with its page and its Next names only open work; a done milestone keeps no item or planned slice and requires only done milestones; every item has one row of a known Kind, one detail section and no delivered record, and leaves its page when it completes; each planned slice has one row of a known Kind naming only its page's items; and an item's Delivery names a planned slice exactly when that slice's row lists the item, with no empty slice suffix. |
| `make test` | Run all package tests. |
| `make vet` | Run Go static analysis on linux/amd64 and on darwin/arm64, where each `*_unsupported.go` refusal stands in for its Linux adapter. |
| `make fmt-check` | Check Go formatting. |
| `make modules-check` | Verify both module locks. |
| `make tidy-check` | Check that both module files are tidy. |
| `make completion-test` | Source and exercise the Bash integration, and the Zsh, Fish and PowerShell integrations with `BOOTWRIGHT_TEST_ALL_SHELLS=1`. |
| `make race` | Run the race detector over the concurrent lifecycle, privilege, trust enrollment and composition packages. |
| `make vulncheck` | Scan for known reachable vulnerabilities. |
| `make ansible-check` | Run pinned Ansible syntax, lint, collection sanity, unit and safe integration checks; `./scripts/ansible-check --suite <name>` runs one suite locally, and `--suite syntax\|lint\|sanity\|units PATH...` narrows one to collection paths; a narrowed units run whose parallel failures pass on the serial rerun exits 75 (FLAKY). |
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
| Ansible `controller_prerequisites` target, which runs the shipped controller playbook and role over the real runner protocol with one `controller-prerequisites-v1` request, publishing a synthetic client from a source already retained in a temporary tool area and replaying it | `BOOTWRIGHT_ANSIBLE_CONTROLLER_TARGET=1` with `./scripts/ansible-check --suite integration` |

`scripts/runtime-lock verify` checks the lock against its publisher with
network, as [below](#lock-refresh-and-tested-hosts). A controller's host
package installation and runtime build are covered by none of these; they are
a manual `controller install` on a host missing something, as the
[operator guide](operator-guide.md#prepare-a-host) describes, recorded as the
[setup row](acceptance.md#setup-fresh-install-row).

### Built-in add-on packages

A new package joins the registry, the render and request goldens and the
coverage parity test by the
[recipe](../.agents/skills/extend-implementation/SKILL.md#adding-a-built-in-add-on-package),
which touches no generic add-on code.

## Lock refresh and tested hosts

`controller install` admits any Fedora or RHEL host on Linux/amd64, a derivative whose
`/etc/os-release` `ID_LIKE` names one included, that has a qualified `rpm` and
`dnf`, a locked Python ABI installed or offered and a glibc at or above the
lock's floor ([controller](../specs/controller.md#supported-hosts)). The lock's
`tested` list names the releases a build was tested on, Fedora 43 and 44 and
RHEL 9 and 10 today; a release outside it gets the informational
`controller.untested-release` line and is never refused. The
[acceptance ledger](acceptance.md#ledger) records each release setup and the
controller stage actually ran on.

The controller runtime is ansible-core 2.21 at the patch
`internal/controller/runtimelock/lock.json` pins, on controller CPython 3.12
or 3.14, the host's own interpreter at `/usr/bin/python3.X`: RHEL 9 installs
AppStream's 3.12, RHEL 10 ships it, and Fedora 43 and 44 ship 3.14. Managed
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
scripts/runtime-lock generate --ansible-core 2.21 --python 3.12,3.14 --installer pip==<version> --output internal/controller/runtimelock/lock.json
scripts/runtime-lock verify
```

`generate` resolves each ABI's binary wheels with the pinned installer, leaves
out the distributions its `OMITTED` list names (ansible-core declares them and
no controller path imports them), downloads every file to confirm its size and
SHA-256, reads each wheel's license into `licenses.json` beside the lock,
computes the glibc floor
from the highest manylinux tag and carries the `tested` list forward; add a
release to it with `--tested fedora:43,44 --tested rhel:9,10` once it ran.
Move `scripts/tools/ansible-requirements.txt` to the same ansible-core patch in
the same commit, and run `make wheels`, which replaces the files the build
carries with the new lock's. `verify` downloads every locked URL, compares it and holds `licenses.json` to
what each wheel declares, and
`outdated` prints newer 2.21 patches and dependency releases without gating;
`.github/workflows/runtime-lock.yml` runs `verify` when the lock or the tool
changes and weekly, with `outdated` as an advisory summary.

`make build` also writes `bin/bootwright.cdx.json`, the CycloneDX bill of
materials the new executable prints with `bootwright sbom --format
cyclonedx-json` ([bill of materials](../specs/cli.md#bill-of-materials)), and
`bin/bootwright.notices.txt`, its [third-party notices](#license-sources-and-notices);
`make container-build` writes both beside its executable. A scanner reads the
pinned components from the first.

### License sources and notices

Each component of the bill of materials names its license from a table the
build holds, so a build offline knows it; the tool that refreshes each table
reads the license from its owner and a check holds the table to it.

| Component | Table | Read from | Refreshed and checked by |
| --- | --- | --- | --- |
| Go modules and the standard library | `cmd/bootwright/go-licenses.json` | Each module's license file in the module cache, matched against the MIT, BSD-2-Clause, BSD-3-Clause, ISC, Apache-2.0 and MPL-2.0 texts; a text that matches none, or two license files, fails until a person reviews it into `reviewedTexts` of `scripts/tools/licenses/classify.go` | `make licenses`, `make licenses-check` |
| Runtime wheels | `internal/controller/runtimelock/licenses.json` | The wheel's own `License-Expression`, else an unambiguous license classifier; a wheel that declares neither needs an entry a person recorded with a `reviewed:` source | `scripts/runtime-lock generate` and `verify` |
| The four default clients | `license` of each `internal/controller/tooltable.json` entry | The publisher's repository, which declares its license through the GitHub API | `make tool-table` |
| Service and emulated BMC images | `imageLicenses` in `cmd/bootwright/billofmaterials_licenses.go` | The license of the project each image runs, with the page that declares it | `TestEveryPinnedImageAndToolNamesItsPublishersLicense` |

`TestEveryThirdPartyComponentNamesAReviewedSPDXLicense` fails when any
component but the project's own collection names no license, or one outside the
reviewed list. After a Go dependency moves, run `make licenses` and commit the
table; `make check` fails until then.

`make build` composes `bin/bootwright.notices.txt` from the bill of materials it
just wrote: for each component marked `required`, the license text its authors
published, taken from the Go table or from the `licenses` directory of the wheel
the executable carries, with a module's `NOTICE`. The tool
(`scripts/tools/licenses`) fails, and so does the build, when such a component
names no license, has no text, or is licensed differently in the table than in
the document. The components a build only names, the clients and images it
acquires at use, are not redistributed by it and carry no text.

The controller stage's clients follow the tool table
`internal/controller/tooltable.json` ([controller](../specs/controller.md#the-tool-table)).
To move a default, edit its version, size and SHA-256 in the table and run,
with network:

```sh
make tool-table
make tool-table-outdated
```

`make tool-table` reads each default's checksum and size from its publisher
and fails on any difference, so a hand-edited entry is proved before it is
committed, and each repository's declared license, and the `go.mod` of
`virtctl`'s release tag against its pin in
`scripts/tools/clientassets/buildmodules.json` (when moving `virtctl`, set that
pin's version, size and SHA-256 to the new tag's `go.mod`, which the
[vulnerability gate](../specs/security.md#the-vulnerability-gate) judges the
release's dependencies by); it sends `GITHUB_TOKEN`, when set, to `api.github.com` alone, which lifts GitHub's anonymous rate limit. `make tool-table-outdated` names each client whose publisher has a
newer stable release, without gating. Raise a minimum only for an advisory
that reaches the releases below it, and never above its default.
`.github/workflows/tool-table.yml` runs `make tool-table` when the table or its
tool changes and weekly, with `make tool-table-outdated` as an advisory
summary.

`make build` first runs `make vulngate`, the
[vulnerability gate](../specs/security.md#the-vulnerability-gate): it needs
`vuln.go.dev` and `api.osv.dev`, scans each default client's release file by
the Go toolchain and modules that binary names (for `virtctl`, whose binary names none, the modules of its pinned release `go.mod`), fails on a finding no reviewed
exception in `cmd/bootwright/vulnerability-exceptions.json` covers, and stamps
its verdict, which `bootwright version` prints. The first run downloads the four
release files, about 160 MB, and `virtctl`'s release `go.mod`, into `CLIENTS_CACHE` (`client-releases` in the
shared cache directory), checking each against the tool table's SHA-256 and
again on every reuse, so a warm cache downloads none. `make check` runs the gate
too, and `.github/workflows/vulnerability.yml` runs it daily, so a new advisory
against an unchanged build fails within a day. The workflow's `images` job also
scans on a schedule the managed services' and the emulated BMC's images, read by
digest from the bill of materials, with a scanner pinned by digest, and
reports their high and critical findings in its summary without failing a
build. Without a route to the databases, run
`make build VULNCHECK=skip`, which stamps the executable `unchecked`. The
[verification tiers](../.agents/skills/code-implementation/references/verification.md#vulnerability-findings)
say how a finding is settled.

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
| `Registry` | `docker.io/library/registry` (Distribution 3.1.2) | not yet; resolved 2026-11-10, its live qualification is [B1250](../specs/milestones/backlog.md#b1250)'s |

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
reads, which no emulated e2e case exercises; the
[B73](../specs/milestones/m1.md#b73) real-hardware operator gate is its first
run.
