# Controller prerequisites and bastion setup

Controller owns inspection and preparation of the local host on which
Bootwright runs. This contract defines `bastion setup` and `preflight bastion`;
[M1d](milestones.md#m1d--bastion-setup) owns delivery and qualification status.
The bastion is the [Environment-selected controller Machine](api/environment.md#controller-machine)
when a context is supplied. Setup never provisions that Machine's OS or
executes a managed service's lifecycle.

## Supported host and dependency selection

M1d must qualify **both RHEL 9 and Fedora on Linux/amd64**. Each supported
combination identifies an exact OS release, tested kernel/filesystem and
privilege primitives, package repositories, native package-manager version,
package builds, and immutable execution bundle. A family name alone does not
qualify all minor releases, Fedora releases or future updates. Unsupported or
unprovable combinations refuse before installation. The exact matrix and
dependency locks must be recorded here before implementation is promoted;
none is claimed qualified by this definition change.

The dependency catalog is compiled into the executable and follows
[dependency selection](architecture.md#dependency-selection-and-reuse) and
[supply-chain integrity](security.md). It distinguishes:

| Prerequisite | Selection and allowed setup |
| --- | --- |
| Host foundation | Verify the provided OS, architecture, local identity, account/sudo boundary, filesystem containment/durability, free-space limits and trusted package sources. No OS installation, release upgrade, repository enrollment, entitlement registration or reboot. |
| Baseline execution bundle | Publish the exact qualified Python and `ansible-core` closure in an isolated Bootwright-owned location. Select only its declared supporting OS packages. Do not use system/user Python imports, ambient Ansible configuration or a floating package resolver. |
| Container runtime | Select the qualified Podman implementation only for an explicit context whose controller declares `container-runtime`. Install missing approved packages if the complete native transaction is safe; verify an existing exact compatible runtime without taking ownership of its containers or configuration. Do not start a service, pull a managed-service image or create a container. |
| Future consumer tools | Installer clients, `oc`, `kubectl`, `virtctl`, Helm, Ceph tools, service images and adapter-specific collections belong to their first supported consumer. An admitted graph or download mirror does not request speculative installation. M1e adds and qualifies its artifact-server adapter closure. |

Setup is additive. It may install absent catalogued dependencies and publish a
new immutable private bundle. It does not replace the running Bootwright
executable, modify shell profiles or global tool search paths, run a general
package update, downgrade/remove packages, or repair unrelated files. A native
transaction that would replace an installed package or affect a protected
dependency refuses with the exact prerequisite requiring operator preparation.
Approved publisher package hooks are part of that transaction's qualified
effect boundary; a list of package names alone is not a complete plan.

An already installed dependency is usable only after identity, version and
integrity checks against the catalog. Process exit status alone does not prove
readiness. An older bundle needed for retry or a future frozen lifecycle is
retained; setup supplies no upgrade, uninstall or garbage-collection command.

## Selection and command journeys

These commands use a named context **only when `--context` is explicit and
nonempty**. An explicitly empty value is omission. Nonempty names follow the
existing context-name grammar. Omission ignores the invoking user's current selection, resolves no desired
state and selects the baseline bundle with direct download routing. This
allows preparation before context creation or Environment import. No current
directory, user profile or ambient proxy selects inputs.

An explicit context must be ready and have an admitted input revision. Resolve
its immutable input, select its controller Machine and add only the controller
prerequisites consumed by this delivery. An empty context returns
`context.input` with the import command. Unsupported unrelated cluster or
service lifecycles do not block controller setup, and setup does not claim
those lifecycles ready. A controller requirement outside the supported setup
matrix does block setup before effects. Global SSH flags remain unconsumed.

| Invocation | Required behavior |
| --- | --- |
| `bootwright bastion setup --dry-run` | Produce the deterministic baseline dependency/action plan from the embedded catalog and bounded local file metadata. No dependency subprocess, network, Secret read, privilege escalation or write. Facts requiring those effects are explicitly unverified. |
| `bootwright bastion setup` | Inspect, present the complete bounded local plan, confirm when it contains changes, prepare the baseline and verify every required postcondition. |
| `bootwright bastion setup --context <name> --dry-run` | Add controller requirements, binding disposition and declared egress from immutable context input. Existing verified sudo may be used solely to read the private store. After that boundary, no dependency subprocess, network, Secret material access, binding publication or other write. |
| `bootwright bastion setup --context <name>` | Verify the local target, include any first host binding in the plan, then perform the confirmed prerequisite work under host and context coordination. |
| `bootwright preflight bastion [--context <name>]` | Read and verify the selected prerequisites with bounded local probes. May use the verified privilege boundary for private metadata. Never install, download, refresh repository metadata, contact a managed endpoint, create/repair state or publish a binding. |

Dry-run does not execute a native package resolver. It lists the pinned required
closure and identifies any transaction feasibility, live identity or readiness
evidence still needed by real setup. A valid dry-run exits successfully with
unverified checks visibly labeled; it never reports completed setup. Preflight
requires positive current evidence for all selected checks; an unbound context
is a failure with setup guidance, distinct from host mismatch.

Real setup resolves the complete package transaction and checks its effects
before confirmation. Any source access required for that inspection is
read-only, bounded and uses the selected route; metadata remains in memory.
Only after confirmation may it cache/download package payloads or write state.
Revalidate the transaction, host and input under the held coordination boundary
before the first mutation. A changed transaction requires a fresh plan and
confirmation, never extra work silently appended to the approved plan.

`--yes` suppresses ordinary confirmation only. Use the existing bounded
yes/no confirmation semantics, with the plan before the prompt; decline,
noninteractive input without `--yes`, or cancellation starts no setup mutation.
A verified no-op needs no prompt or writes. First binding publication is a
change even when every dependency is already ready. Setup changes neither
current-context selection nor Environment input.

## Egress and local effects

With an explicit context, the controller Machine's normalized
[proxy choice](api/machines.md#machine-proxy) is the sole route selection.
M1d supports direct access and qualified unauthenticated external proxies using
the qualified system trust store. A managed Proxy, `proxyAuthRef` or
`trustBundleRef` is unsupported for setup in this slice and refuses before
acquisition. Never fall back to direct access or read Secret material to probe
an unsupported route. Authenticated/private-trust setup acquisition needs its
own Secrets consumer and recovery definition before promotion.

Reject a bootstrap dependency on a proxy or other service whose readiness
requires this setup or a subsequent apply. Admission of a reference is not
readiness evidence. Proxy routing applies only to setup's child processes and
download client; it does not reconfigure the provided OS. TLS verification,
trusted publisher metadata, digest checks, bounded redirects and acquisition
limits follow [Security](security.md). Package/source URLs contain no
credentials; ambient proxy, Python, Ansible, package-manager and executable-path
configuration cannot alter the approved closure.

Controller consumes typed local inspection, acquisition, package-transaction
and bundle-publication ports. Requests carry exact target evidence, dependency
identities, authorized actions, route, limits and cancellation. Results report
per-action postconditions and `unchanged`, `changed`, `failed`, `canceled` or
`unknown` outcomes; they contain no raw subprocess transcript. Pure policy
selects and orders actions; composition binds fixed local adapters. Qualified
native package management is an explicitly local Controller capability, not
managed-remote automation. The [Go/Ansible boundary](architecture.md#go-and-ansible-responsibility-boundary)
continues to govern managed targets.

## Host identity and shared prerequisites

Controller verifies the executing installed host; a Machine name, address,
DNS answer or `access.local` cannot prove it. The selected host identity
implementation must define stable installed-host evidence and how it detects
substitution, copied state, cloned identity, containers and a different mount
namespace. Qualification must distinguish ordinary reboot from relocation;
unprovable identity refuses. A hostname or machine-id string alone is
insufficient. Hardware attestation and controller relocation are not implied.

[Workspace](contexts.md#controller-relationship-and-host-binding) persists the
verified identity and context relationship. A first explicit setup displays
the selected Machine and proposed local binding and confirms it before
publication. Subsequent setup and context preflight verify that exact binding;
they never silently rebind. Context-free setup can prepare the host without
claiming any context. Lifecycle protection of the OS, power, runtime and state
follows [controller-host protection](state-reconciliation.md#controller-host-protection).

Workspace serializes shared prerequisite mutation across every context in the
fixed root using its exclusive root lock, acquired before a context lease.
Preflight holds a shared root lock when the root exists, for coherent stored
evidence. An absent root reports missing setup with `bastion setup` guidance
and creates nothing. A busy lock refuses; there is no timeout-based takeover. Before installation, reject any
package/runtime transaction that could alter dependencies in use or retained
by another setup or frozen lifecycle. The OS package-manager lock is an
additional requirement, not a replacement for Bootwright coordination.

No service port or container name is reserved by setup. M1e must define those
conflict identities and join this host coordination before local service
effects become available. Dependency readiness never establishes service
ownership or authorizes another context's resources.

## Publication and interrupted setup

Setup has a private durable receipt, separate from lifecycle operation state
and logs. Before the first installation or bundle-publication effect, record
the verified host, selected implementation and complete immutable local action
plan. For a context-bound attempt, retain its context ID, controller identity
and input revision. Keep required input/binding evidence protected from update
and purge until the attempt has a definitive terminal outcome.

Each action has attributable before-state, fixed request, postcondition and
outcome. Record intent durably before starting it; record success only after
both postcondition verification and durable evidence publication. Interrupted
or failed setup may leave approved packages installed. Do not claim rollback
or remove shared OS dependencies automatically. A receipt is recovery evidence,
not a lifecycle operation ID, ownership of the host, or GitOps readiness.

An explicit repeat of the same setup command first resolves the exact pending
receipt. If no effect occurred, it may retry the same action. Positive proof
of the requested postcondition permits completion without repeating an install.
Contradictory or incomplete proof leaves the action unknown and blocks further
mutation with recovery guidance. Never turn a lost child result into failure
with a presumed no-effect outcome. Changed input, host identity or dependency
closure cannot replace an incomplete setup; restore the exact compatible
executable/dependencies and resolve that receipt first.

Cancellation stops new actions, cancels and reaps the complete child process
tree, and preserves verified progress. Never release coordination while an
installer child can still mutate the host. Qualification must also cover
parent process death and a surviving package-manager process: another setup
must refuse until that transaction has stopped and its exact outcome is
proved. Publication/sync failure leaves uncertainty explicit and authorizes
neither cleanup nor an unrecorded retry.

Completed setup retains enough bounded host and bundle evidence for later
inspection; no-op repetition revalidates without republishing. A later input
update may change controller prerequisite intent after setup recovery is
complete, but cannot silently change an established controller Machine/host
binding. Changes in readiness require another explicit setup. Deleting a
disposable context removes its binding/receipt references, never shared host
packages or bundles. Complete-store restore and relocation remain separately
defined work.

## Results and qualification

Both commands retain their existing text-only CLI surface. Order checks and
actions by catalog dependency order, then stable prerequisite identity. Show
the baseline or explicit context scope, required/observed versions, planned
changes, readiness and next safe command. Host fingerprints, private paths,
credentials, environment dumps and raw native-tool output are not public
results. No new JSON flag, lifecycle receipt or private operation log is added.

The [diagnostic taxonomy](cli/output.md#diagnostic-taxonomy-and-order) owns
`preflight.*` and `controller.*` failure codes; existing context, privilege,
output and cancellation codes retain their meanings. Failures include a safe
actionable next step. The [output contract](cli/output.md) owns streams and exit
status, including complete negative readiness reports and partial setup
outcomes. No setup failure may imply that already verified effects rolled back.

M1d's gate requires independent RHEL 9 and Fedora end-to-end evidence, including
clean install, already-ready no-op, context-free preparation, explicit binding,
read-only preflight and dry-run, unsupported combinations, dependency/identity
substitution, multi-context contention, protected dependencies, canceled and
killed processes, package failures, uncertain publication and exact retry.
Unit or fake-adapter results do not qualify a host or native installer.
