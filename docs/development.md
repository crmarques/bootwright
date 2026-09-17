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
| `make build` | Build `bin/bootwright`, stamped with the version, commit and source state of the checkout it was built from. |
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

The current [M1d delivery](../specs/milestones.md#m1d--controller-setup) adds
controller dependency preparation and preflight to the existing admission,
context, rendering and Secret journeys. Go selects and freezes dependencies,
owns confirmation/recovery and orchestrates the embedded Ansible collection.
Ansible installs the selected host packages and target CLIs. The private
Python/Ansible bootstrap is materialized by Go so the playbooks can run.
Setup resolves latest stable dependencies by default, and only when no retained
resolution serves the selected intent; Environment `spec.dependencyVersions`
overrides individual roots, including Python and Ansible. Public resolver
downloads and maintained pip/DNF resolution use disposable unprivileged staging
before the installation plan is confirmed. A ready controller and every retry use
the frozen result without contacting a publisher. Development checks retain
their separate pinned tool versions for reproducible verification.
The [Ansible check tool lock and setup](../scripts/tools/ansible-check.md)
is separate from the product execution bundle and builds its own pinned
interpreter on first use, so the gate does not depend on the host's Python. `make check` includes its
unprivileged collection gate; it never installs controller packages.

Controller tests are unitary and host-independent, following the
[M1d verification model](../specs/milestones.md#m1d--controller-setup): no package
manager, network, privilege or second operating system, and no virtual machines.
End-to-end acceptance against a real controller is operator-run. Deferred
lifecycle commands keep their unavailable result until their owning milestone.

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
`setup` on a prepared host.

## M1f managed infrastructure components and staged apply

The [M1f delivery](../specs/milestones.md#m1f--managed-controller-network-services)
completes the `infra-components` stage: managed `Proxy`, `DNSServer` and
`NTPServer` join the managed `ArtifactServer` behind one capability port, and
`plan` and `apply` accept `--stage`. Go owns plans, operation records, leases,
continuation, stage gating and authorization; the embedded collection installs
and removes each service. The verification model is M1d's: every in-tree test
is unitary and host-independent, creates no container and needs no second host,
and real-system acceptance is operator-run.

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
An authored `spec.image` uses that reference instead, and any reference must
resolve to an immutable digest.

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
reads, which only the M5a rehearsal exercises.

The embedded collection participates in the dependency-bundle identity, so a
build that changes `ansible/` changes the bundle a context is bound to. Run
`setup` again after such a build; `preflight controller`
reports the incompatible retained bundle and `apply` refuses rather than
executing automation the receipt does not cover. An operation left incomplete
by the previous build cannot be continued under the new one, because a
continuation runs the automation it froze. A failed one is removed instead: a
fresh `destroy` runs under the build in hand, which is the ordinary loop when
the repair is to the role that failed. A removal first proves everything it
would take back is out of use, so stop any running machine with
`bootwright machine stop` before it; the refusal names each one and the command
that stops it, and nothing is registered until they are.

Changing a frozen block's Go request or plan shape changes its digests, so an
operation registered by an earlier build can be neither continued nor removed
by this one. Destroy or purge any live context before switching builds.

`make ansible-check` covers the new collection content with the same pinned
syntax, lint, sanity and unit gates as the controller entrypoints. Executed
service effects are not covered by any in-tree gate; these remain operator-run
and are not selectable from a test runner:

| Acceptance | How it is run |
| --- | --- |
| The complete journey in [`examples/lab-rhel`](../examples/lab-rhel/README.md): staged apply, replay, interrupt, continue and destroy against a real container runtime | by hand as root on a prepared controller |
| SSH placement against a second OS-ready host | by hand, with that host's authored access and bound host key |

## M1g controller prerequisites by selecting scope

The [M1g delivery](../specs/milestones.md#m1g--controller-prerequisites-by-selecting-scope)
splits controller prerequisites by what selects them. `bootwright setup`
prepares the host foundation every context shares and reads no desired state at
all; the `controller` stage of a context's own `apply` installs the target
clients its graph selects and the libvirt client it declares.

Those clients are shared host state, so they are published into a
content-addressed client area beside the setup bundle rather than into it, and
a `destroy` retains them. Two contexts selecting the same releases prove the same
sealed files; changing a release creates a new area and leaves the old one in
place, because an operation frozen against it may still need it.

The stage runs the same `bootwright.core.controller_prerequisites` role setup
runs, through the request version `controller-prerequisites-v3`. That version
adds `publicationBundle`: the one area a request may write. Setup passes the
bundle it executes from; the controller stage passes its client area, because
its own execution bundle is sealed. An older executable cannot read a request
at this version, so a build that changes `ansible/` still requires `setup`
again before the first `apply` of each context.

The verification model stays M1d's. Executed client installation is not covered
by any in-tree gate:

| Acceptance | How it is run |
| --- | --- |
| `apply --stage controller` for a context selecting OpenShift clients and Helm, then a repeated apply that reports `unchanged` without publisher access | by hand as root on a prepared controller |
| The same for a context declaring the `libvirt` capability, on Fedora; RHEL refuses before acquisition until an entitled source is defined | by hand as root on a prepared Fedora controller |

## M1h managed RHEL on emulated bare metal

The [M1h delivery](../specs/milestones.md#m1h--managed-rhel-on-emulated-bare-metal)
installs one RHEL Machine on a libvirt guest that boots its installer through an
emulated Redfish BMC. Its in-tree gates are unitary and host-independent: they
create no domain, start no container and contact no controller.

The verification model stays M1d's. Executed installation is not covered by any
in-tree gate:

| Acceptance | How it is run |
| --- | --- |
| The complete journey in [`examples/lab-rhel`](../examples/lab-rhel/README.md): apply, a replay that settles, a removal refused while the guest runs, `machine stop`, destroy and a fresh apply | by hand as root on a prepared libvirt host |
| A host restart followed by `machine start`, which proves the provider host carries its networks and pool across the restart | by hand as root on that host |

## M5a managed RHEL on physical bare metal

The [M5a delivery](../specs/milestones.md#m5a--managed-rhel-on-physical-bare-metal)
installs the same operating system on an operator-owned server through its own
management controller. Its in-tree gates drive the Redfish client against canned
firmware shapes rather than hardware.

Two acceptances are operator-run, and the first is a gate of the delivery
because it is what proves the physical contract without hardware:

| Acceptance | How it is run |
| --- | --- |
| The rehearsal in [`examples/lab-baremetal`](../examples/lab-baremetal/README.md): one context realizes a guest and its emulated controller, a second claims that guest as a physical Machine and installs it | by hand as root on a prepared libvirt host |
| Installation of a real server through its own controller | by hand against qualified firmware, after the client is driven by hand against that controller |

## M4a single-node OpenShift through the agent installer

The [M4a delivery](../specs/milestones.md#m4a--single-node-openshift-through-the-agent-installer)
installs one OpenShift cluster on the Machines a substrate realizes, through the
release's own `openshift-install`. Its in-tree gates are unitary: they build no
image, contact no controller and run no installer. The installer input
projection is guarded by byte goldens for a single-node libvirt cluster, a
multi-node libvirt cluster and a multi-node physical cluster, so a change to any
part of what the installer reads is visible in a diff.

`openshift-install` itself is not qualified by any in-tree gate. The release it
builds for is proved at execution instead: the attempt reads the version the
executable reports and refuses before building when it is not the declared one.

| Acceptance | How it is run |
| --- | --- |
| The journey in [`examples/lab-sno`](../examples/lab-sno/README.md) as far as the published boot image: `apply --stage controller`, then an apply that builds and publishes it, then a repeated apply that settles | by hand as root on a prepared libvirt host with a pull secret |
| A destroy that takes back the published image and the installer's work area | by hand on that host |
