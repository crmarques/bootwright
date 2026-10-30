# Add-ons audit: architecture, API, operator CLI and developer path

Reviewer: Claude Fable 5.1, orchestrating two independent Claude Opus 5.5
reviews (operator journey, developer journey) and one Claude Sonnet 5.5 probe
run that reproduced the candidate defects. Assessment date: 2026-10-30.
Product baseline: `4294e7520` on `main`, clean tree. Scope: the add-on
architecture (registry, selection, rendering, frozen request, adapter,
evidence, retirement), the public add-on API, the operator's CLI and
documentation journey, the add-on developer's contribution path, and how
default and optional add-ons can be made available. This is a source and
test assessment with local, effect-free probes; it is not a live-cluster
qualification. It changes no product code, no contract and no milestone
state, and authorizes no implementation.

Line numbers refer to the baseline. The sibling report
[report-codex.md](report-codex.md) was read only after this report was
written; [report-final.md](report-final.md) merges the two.

Reading order: [assessment](#assessment), [how add-ons work today](#how-add-ons-work-today),
[operator journey](#operator-journey), [developer journey](#developer-journey),
[default and optional add-ons](#default-and-optional-add-ons),
[findings](#findings), [delivery sequence](#delivery-sequence),
[verification and limits](#verification-and-limits).

## Assessment

The add-on core is sound and deliberately small. A built-in package is a pure
Go descriptor that contributes operands, Secret placements and readiness checks
to one closed cluster object program; one lifecycle implementation runs every
package through one create-only adapter with ownership receipts, exact OLM
approval and read-only observation. Six packages ship in the executable, each
with render and plan goldens, admission tests and a fake-cluster journey. That
foundation is the right one to build the operator and developer experience on.

The experience around that core is where the gaps are. Three conclusions
matter most:

1. **"Available" works; "selected" is explicit; "added later" does not
   exist.** Every package in the executable is available offline through
   `bootwright add-ons list`. A package is installed only when a
   `ClusterAddonBinding` names it with its channel and exact CSV. Once an
   apply completes, no package can be added, reconfigured or removed without
   `destroy` then `apply` (D153, B769). If "as needed" means after bootstrap,
   a new, bounded additive apply scenario is required; nothing short of it
   (catalogs, presets, defaults, preflight) changes that ([C01](#c01)).
2. **The operator has no entry journey.** The operator guide does not
   mention add-ons; the CLI specification has no add-on journey; the listing
   names configuration members but not what a valid binding needs; the help
   for `add-ons` says "Manage add-ons" and offers one read-only command. The
   four examples and the lab-sno runbook are the only paths, and they are
   good ([C02](#c02), [C08](#c08)). Counterevidence: admission diagnostics
   are strong, including an unknown package name that lists the permitted
   packages and suggests a rename.
3. **Adding a built-in package is cheap in code and expensive in
   discovery.** The real touch set is four Go files, three goldens and one
   brittle test; the commit that added `kubevirt` was 389 lines, over half of
   them milestone bookkeeping. But no recipe exists, the extension skill
   routes contributors to the heavier lifecycle-capability path, generic code
   branches on package names in four places, and the shared golden harness
   accepts only OLM packages with a CRD-gated first phase. A provider package
   such as `certmgr` cannot even be registered before its consumer without
   editing generic code, and seven delivered or planned items cite a
   coverage-page parity test that does not exist ([C05](#c05), [C06](#c06),
   [C07](#c07), [C20](#c20)).

Two defects were reproduced without effects and one extension defect by a
synthetic probe: a fractional-second `timeout` passes `validate` and fails
`plan` with a bare error that has no code, field or remedy ([C03](#c03));
Environment kind defaults silently add a package to every binding that
selects any other package, with no per-package opt-out and a duplicate
diagnostic that blames the binding that inherited it ([C09](#c09)); and the
Secret-placement namespace allowlist is rebuilt from the literal package name
`gitops` when a request is thawed, so a second package cannot use the declared
mechanism ([C04](#c04)).

The recommended path is in [delivery sequence](#delivery-sequence): an
operator entry journey and a self-sufficient listing first, then the three
small code defects, then the developer recipe and harness, then the additive
apply scenario if the owner wants day-2 additions, and only then B90's
authored packages.

## How add-ons work today

The chain, with its owner:

| Step | What happens | Owner |
| --- | --- | --- |
| Registry | `packages.Builtin()` registers six pure descriptors; `NewRegistry` enforces name, namespace and OLM package uniqueness, a provided-token reader rule and the approval policy ([packages.go:154-216](../../internal/addons/packages.go), [registry.go](../../internal/addons/packages/registry.go)) | [built-in packages](../../specs/add-ons.md#built-in-packages-and-selection) |
| Listing | `add-ons list` joins the registry with the `spec.packages` schema; reads no context, file or network ([catalog/service.go:23-44](../../internal/addons/catalog/service.go)) | [the package listing](../../specs/add-ons.md#the-package-listing) |
| Selection | A binding's `spec.packages.<name>` member selects a package; normalization fills `timeout`, `source`, `sourceNamespace` and package defaults; admission checks the configuration against the bound cluster's admitted facts ([instances.go:126-247](../../internal/addons/instances.go)) | [ClusterAddonBinding](../../specs/api/addons.md#clusteraddonbinding), [built-in packages](../../specs/api/addons.md#built-in-packages) |
| Ordering | `Instances` builds one instance per package and cluster, orders by provided and required capability tokens, `afterRefs` and the last-package rule, and refuses duplicates and cycles ([instances.go:38-104](../../internal/addons/instances.go)) | [admission diagnostics](../../specs/api/addons.md#admission-diagnostics) |
| Rendering | `instance.Render` composes the OLM prelude, Before phases, the Subscription, After phases and checks into a program, freezes it and builds the request with its produced inputs and Secret names ([render.go:16-87](../../internal/addons/instance/render.go)) | [the cluster object program](../../specs/add-ons.md#the-cluster-object-program) |
| Planning | One block `addon-<package>-<cluster>` in the `add-ons` stage, requiring the cluster's blocks, its providers and the installation block's kubeconfig ([capability.go:27-77](../../internal/addons/instance/capability.go)) | [instances and blocks](../../specs/add-ons.md#instances-and-blocks) |
| Execution | The thin `addons_instance` role runs the shared `cluster_objects` role with the lent kubeconfig and `oc` of the cluster's release ([apply.yml](../../ansible/collections/ansible_collections/bootwright/core/roles/addons_instance/tasks/apply.yml)) | [the cluster object adapter](../../specs/add-ons.md#the-cluster-object-adapter), [OLM installation](../../specs/add-ons.md#olm-installation) |
| Evidence and removal | Receipts per owned object, OLM facts and check results; retirement contacts nothing ([capability.go:106-172](../../internal/addons/instance/capability.go)) | [ownership receipts](../../specs/add-ons.md#ownership-receipts), [retirement](../../specs/add-ons.md#retirement) |

What is strong, and should not be redesigned:

- The package contract is a value: no I/O, no process, no cluster. Purity and
  determinism are tested for every shipped package
  ([registry_test.go:84-118](../../internal/addons/packages/registry_test.go)).
- Schema and registry parity is tested
  ([registry_test.go:132-146](../../internal/addons/packages/registry_test.go)),
  typed decoding rejects unknown members
  ([packages.go:114-124](../../internal/addons/packages.go)), and the
  configuration is closed per package
  ([addons.go:136-149](../../api/v1alpha1/addons.go)).
- Exact OLM approval, create-only ownership, foreign-object refusal and
  retirement without contact are specified and journey-tested
  ([addon_journey_test.go](../../internal/reconciliation/lifecycle/addon_journey_test.go)).
- Authored `ClusterAddon` and `ClusterAddonProfile` are admitted, warned as
  `api.unrealized` at `validate` and refused at `plan`
  ([unrealized.go:25-26](../../api/v1alpha1/unrealized.go),
  [selection.go:19-23](../../internal/addons/instance/selection.go)); nothing
  is silently ignored.
- The e2e input chart already models "optional add-on" selection as values
  (`addons.nmstate`, `addons.metallb`) with required channel and CSV
  ([values.yaml:39-56](../../e2e/lab-sno/values.yaml),
  [nmstate.yaml](../../e2e/lab-sno/templates/add-ons/nmstate.yaml)).

The capability boundary at the baseline:

| Need | Behavior | Reference |
| --- | --- | --- |
| See which packages this build offers | `add-ons list`: `acm`, `fdf`, `gitops`, `kubevirt`, `metallb`, `nmstate`; offline | [cli-add-ons-list.golden](../../internal/cli/testdata/cli-add-ons-list.golden) |
| Install a package during bootstrap | a binding with `spec.packages.<name>.subscription.{channel,startingCSV}` | [examples/add-ons](../../examples/add-ons/) |
| Pause after one instance | `apply --until <cluster>.<package>` | [stopping after an add-on instance](../../specs/state-reconciliation.md#stopping-after-an-add-on-instance) |
| Add, change or remove a package after a completed apply | refused; `destroy` then `apply` | [C01](#c01) |
| Install on a cluster the context did not build | impossible: the request must name the installation block's kubeconfig ([requests.go:84-86](../../internal/addons/instance/requests.go)) | [instances and blocks](../../specs/add-ons.md#instances-and-blocks) |
| Reuse a package set by name across clusters | no: profiles hold authored references only ([addons.go:89-91](../../api/v1alpha1/addons.go)) | [C11](#c11) |
| Share package values across bindings | Environment kind defaults, with the semantics of [C09](#c09) | [kind defaults](../../specs/api/environment.md#kind-defaults) |
| Ship an add-on without rebuilding Bootwright | no: authored packages are B90, "Needs definition", after M4f | [C15](#c15) |
| Check native prerequisites before the instance runs | `preflight add-ons` answers `cli.not-implemented` (B770) | [C10](#c10) |
| Use `certmgr` or `gitlab` | specified, absent from the registry (B767, B93) | [coverage](../../specs/add-on-coverage.md#built-in-packages) |

## Operator journey

Walked as discovery, authoring, validation, planning and execution,
inspection, change and removal, documentation.

**Discovery.** `bootwright add-ons --help` says "Manage add-ons" and lists one
command ([help-add-ons.golden](../../internal/cli/testdata/help-add-ons.golden)).
`add-ons list` prints name, provides, requires, OLM package, default source
and top-level configuration member names
([output_addons.go:33-37](../../internal/cli/output_addons.go)). It does not
say which members are required, that `subscription` needs `channel` and
`startingCSV`, where to read them (the PackageManifest of the OLM package it
already prints), what `REQUIRES none` means (capability tokens, not cluster,
storage, entitlement or catalog prerequisites) or why `fdf` has `DEFAULT
SOURCE none`. The help's only example is `--output json`. Nothing links the
listing to the examples or the coverage page. An operator who starts from the
CLI reaches a valid binding only by reading `examples/add-ons` or the specs
([C08](#c08)).

**Authoring.** A minimal binding is small and the examples are exact
([lab-sno-nmstate binding](../../examples/add-ons/lab-sno-nmstate/add-ons/binding.yaml)).
Each README marks its channel and CSV as placeholders and gives the
`oc get packagemanifest` command; this is the right content, in the right
place, and absent from the product surface. The e2e values gate each add-on
by a boolean and require the two choices at render time
([values.yaml:39-56](../../e2e/lab-sno/values.yaml)).

**Validation.** Admission is the strongest part of the journey. An unknown
package member says `unknown field; spec.packages permits nmstate, kubevirt,
metallb, fdf, acm, gitops`, with `rename it to metallb` for a typo (probe P2).
Missing channel, CSV and source each have their own `api.required` or
`api.value` diagnostic with a remedy (probe P3d). Duplicate packages, missing
providers, cycles and cross-cluster ordering are refused with indexed fields
([admission diagnostics](../../specs/api/addons.md#admission-diagnostics)).
Two gaps: a fractional `timeout` is admitted and later fails planning with a
bare error ([C03](#c03)), and a package inherited from Environment defaults
is reported as if the binding had declared it ([C09](#c09)).

**Planning and execution.** The block is `addon-<package>-<cluster>` with
the description `install add-on <package> on <cluster>`
([plan-lab-sno-nmstate.golden](../../cmd/bootwright/testdata/plan-lab-sno-nmstate.golden)).
`--until <cluster>.<package>` pauses after one instance with precise refusals
([stopping after an add-on instance](../../specs/state-reconciliation.md#stopping-after-an-add-on-instance)).
During the readiness wait the attempt holds one absolute deadline and reports
each unmet check at the deadline ([readiness](../../specs/add-ons.md#readiness)).
An unresolved outcome names the foreign object or the unexpected CSV with the
remedy `destroy and apply again`
([capability.go:190-208](../../internal/addons/instance/capability.go)). The
first live check of a catalog, channel or CSV happens here, after the cluster
installed, because `preflight add-ons` is not implemented ([C10](#c10)).

**Inspection.** `status` reports block state; `diff --observe` reports an
instance's realization and never its field values
([observation](../../specs/add-ons.md#observation-and-continuation)). A
settled apply contacts no cluster.

**Change and removal.** Any change to a binding after a completed apply is
refused by name, with `destroy` then `apply` as the remedy
([execution.go:597-606, 649-662](../../internal/reconciliation/lifecycle/execution.go)).
No instance is removed online (B769). On a physical cluster the objects and
placed Secrets stay, unmanaged, which the removal description states
([retirement](../../specs/add-ons.md#retirement)). These are deliberate
(D153, Q32) and correctly specified; what is missing is that the operator
learns them from specs, not from the guide or the help ([C01](#c01),
[C02](#c02)).

**Documentation.** [docs/operator-guide.md](../../docs/operator-guide.md)
has 722 lines and no occurrence of "add-on"; its lab table names lab-rhel,
lab-sno and lab-baremetal only. [specs/cli.md](../../specs/cli.md) has no
add-on section among its journeys. The coverage page's `gitops` example still
uses `credentials` where admission requires `credentialsRef`
([add-on-coverage.md:260](../../specs/add-on-coverage.md), tracked as
[B884](../../specs/milestones/m4.md#b884)).

## Developer journey

Walked as: what a new built-in package touches, where generic code leaks
package names, what the harness assumes, where the recipe is, and what the
authored route offers.

**Touch set.** The commit that added `kubevirt`
([1a3a0d43b](../../internal/addons/packages/kubevirt.go)) changed 17 files
and 389 lines: the descriptor and its test, one registry line, nine schema
lines, one coverage line in the listing goldens, a count and positional
indexes in the catalog test, one implementation-identity row, and about 200
lines of specs, knowledge and milestone text. The code and test part is small
and well-shaped. The process part dominates, which is appropriate for a
reviewed built-in but must be stated somewhere a contributor will read.

**Name leaks in generic code.** Six places key on a package name where a
declared field should carry the fact:

| Place | What it does | Cost to the next package |
| --- | --- | --- |
| [packages.go:157](../../internal/addons/packages.go) | the reader allowlist for provided tokens is `kubevirt`, `dataFoundation`, `metallb`, `nmstate` plus what registered packages require | a provider cannot register before its consumer: a `certmgr` providing `certManager` makes `Builtin()` panic at [packages.go:188](../../internal/addons/packages.go) until `gitlab` requires it or the literal list grows; the slice order X153 before X154 runs into this |
| [packages.go:192](../../internal/addons/packages.go) | `OperatorManaged` approval is legal only when `p.Name == "fdf"` | the next operator that approves its own plans edits the registry rule, not its descriptor; the coverage row is the stated owner of that policy ([OLM installation](../../specs/add-ons.md#olm-installation)) |
| [instances.go:155-160](../../internal/addons/instances.go) | MetalLB pool defaulting and sorting run inline before the generic `Normalize` hook | a second package with list members copies the pattern into generic code instead of its hook |
| [requests.go:61-63](../../internal/addons/instance/requests.go) | the placement allowlist is rebuilt as `openshift-gitops` when `r.Package == "gitops"` | any other package naming `PlacementNamespaces` renders and then fails its own `Canonical()` ([C04](#c04)) |
| [instances.go:177-188](../../internal/addons/instances.go) | the one-consumer-per-`CephConsumer` rule reads `packages.fdf.cephConsumerRef` by literal path | a second package that consumes a `CephConsumer` is not covered by the rule until generic code learns its path |
| [bindings.go:198](../../internal/addons/bindings.go) | Environment cluster selection reads the same literal path to attach the consumer's chain | the same second package would not pull its consumer into the selection closure |

**Harness assumptions.** The shared golden test dereferences `p.OLM` for
every package and composes only `After`
([golden_test.go:139-146](../../internal/addons/packages/golden_test.go)); the
purity test requires every non-`gitops` package's first `After` phase to gate
on an `Established` CRD
([registry_test.go:99-103](../../internal/addons/packages/registry_test.go));
the catalog test pins the count and three positional indexes
([service_test.go:19](../../internal/addons/catalog/service_test.go)). A
manifest-only package, a package with a `Before` phase, or a package whose
first operand needs no CRD gate cannot join the harness unchanged, although
production rendering supports all three
([render.go:33-48](../../internal/addons/instance/render.go)). The goldens
also compose the program themselves instead of calling `instance.Render`, so
they do not pin what the request will hold ([C07](#c07)).

**Entry point.** There is none. [docs/development.md](../../docs/development.md)
has no add-on section. The
[extend-implementation skill](../../.agents/skills/extend-implementation/SKILL.md)
names "add-on driver" in its description and sends a contributor to
[adding a lifecycle capability](../../specs/architecture.md#adding-a-lifecycle-capability)
or to inferring the touch set from a sibling, while a built-in package needs
no capability, playbook, role or wiring. The facts a contributor needs are
spread over [package qualification](../../specs/add-ons.md#package-qualification),
[built-in packages](../../specs/api/addons.md#built-in-packages),
[qualification rules](../../specs/add-on-coverage.md#qualification-rules) and
the tests ([C06](#c06)).

**The authored route.** A `ClusterAddon` with an `olm` arm is admitted,
normalized and warned, and its binding is refused at plan
([C15](#c15)). The gap to running it is smaller than the deferred design
suggests. The program's vocabulary already covers an authored OLM arm:
`operatorPrelude` and `subscribe` take an `addons.OLM` value and the
configuration's `subscription`
([render.go:137-202](../../internal/addons/instance/render.go)); the three
authored readiness arms map onto the program's `csvSucceeded`, `condition` and
`resourceExists` checks; `customResources` are canonical bodies the program
already validates for content safety. What does not map, and would bound a
minimal B90: `namespace.management: external`, `installPlanApproval:
Automatic`, an optional `startingCSV`, a `targetNamespaces` list beyond own or
all namespaces, and a gate for each custom resource, because the program
cannot derive a CRD name from `apiVersion` and `kind`. The
[deferred design](../../specs/deferred/add-ons-design.md)'s catalog records,
drivers and host interface are not needed for that first step.

## Default and optional add-ons

The question has three meanings, and the product answers them differently:

| Meaning | Today | Assessment |
| --- | --- | --- |
| Available in every Bootwright build | every package in the registry, listed offline | right; keep the executable as the only catalog ([trust boundary](../../specs/add-ons.md#trust-boundary)) |
| Installed on a cluster unless the operator opts out | nothing is installed without a binding and an explicit channel and CSV (D163) | right for a day-0 tool: no package can carry a vendor release, so a product default cannot exist |
| Selected by default across an estate | Environment kind defaults, input templates, duplicated bindings | workable with pitfalls ([C09](#c09)); no named, selective preset ([C11](#c11)) |
| Optional, added when needed | before the first apply: add the member to the binding; after a completed apply: `destroy` then `apply` | the day-2 meaning needs an additive scenario ([C01](#c01)) |

**What Environment defaults do** (probe P3, [defaults.go:12-60](../../internal/desiredstate/compilation/defaults.go)):

| Binding declares | With `defaults.ClusterAddonBinding.packages.nmstate` present | Observed |
| --- | --- | --- |
| no `packages` member | inherits the whole map and selects `nmstate` | by rule 3 of [kind defaults](../../specs/api/environment.md#kind-defaults) |
| `packages: {metallb: {...}}` | selects `metallb` and `nmstate` | P3a: both instances planned on `sno` |
| `packages: {}` | inherits nothing; the binding is refused as selecting nothing | P3b |
| `packages: {nmstate: {}}` | selects `nmstate`, inherits none of its configuration; `subscription` required | P3d |
| two bindings on one cluster, one explicit `nmstate` | `api.duplicate` on the explicit binding, naming the binding that inherited | P3c: the message blames a binding that never wrote `nmstate` |

So Environment defaults can make a package "default" for every binding of
every cluster in the Environment, with no per-binding or per-package opt-out
and no origin in the diagnostic. They are the right place for shared values
(a catalog source, a channel and CSV an estate standardizes on) and the wrong
place for selection.

**Recommended design.** Separate availability, selection and activation, and
give each the smallest surface:

1. Availability stays the executable's registry. Make the listing
   self-sufficient offline: a `describe` or `example` form that prints, from
   the schema and the descriptor, the required members, the defaults, the
   PackageManifest lookup for the printed OLM package, the capability tokens
   with a one-line meaning of "requires", and a copy-ready binding skeleton
   with placeholder channel and CSV. This needs no new registry data beyond
   a one-line purpose per package; the schema already knows required fields
   ([C08](#c08)).
2. Selection stays explicit in bindings. Document the two estate patterns
   an operator has today, the input chart with boolean values and Environment
   defaults for shared values, with the semantics above, in the operator
   guide ([C02](#c02), [C09](#c09)). Only when a repeated consumer appears,
   amend Q26 to let a `ClusterAddonProfile` carry `packages` as a named
   preset, with one precedence rule (binding member over profile over
   Environment default, two profiles supplying one package refused) and
   origins in `render effective --origins`. Prefer that over a subtraction
   marker, which the API's omission and null rules do not admit cleanly
   ([C11](#c11)).
3. Activation after bootstrap is a new lifecycle scenario, added by D153's
   recipe: a decision, a state-machine row, one branch in the changed-input
   path. Its invariants: the only change in the input is added members under
   `spec.packages` or added bindings on an installed cluster, proved by the
   same comparison `diff` uses; every block of the frozen plan keeps its
   request; the new instances form a second operation that records the first
   as its predecessor and requires its done blocks; the kubeconfig custody of
   the installation block is lent to the second operation; ownership marks
   hash the second operation's identity, so receipts never collide; a later
   `destroy` takes both operations back in reverse order; `status`, `diff`
   and the ledger show both. Upgrade, reconfiguration and online removal stay
   out of the first scenario ([C01](#c01)).
4. Preflight joins when an operator asks (B770), scoped as the item states:
   API reachable through the lent kubeconfig, catalog `READY`, CSV in the
   channel ([C10](#c10)).

## Findings

Severity is the impact on the requested operator and developer experience:
High blocks or misleads the journey, Medium costs a document round-trip or an
edit to generic code, Low is hygiene. Class distinguishes a reproduced defect
from a design gap and from an intentional limit the owner decided.

| ID | Severity | Class | Finding |
| --- | --- | --- | --- |
| [C01](#c01) | High | Intentional limit | No package can be added, changed or removed after a completed apply |
| [C02](#c02) | High | Docs gap | The operator guide and the CLI specification have no add-on journey |
| [C03](#c03) | Medium | Reproduced defect | A render-time refusal has no diagnostic: fractional `timeout` passes `validate`, fails `plan` with a bare error |
| [C04](#c04) | Medium | Reproduced extension defect | The placement namespace allowlist is rebuilt from the package name at thaw |
| [C05](#c05) | Medium | Developer gap | Generic code keys on package names in six places; a provider cannot register before its consumer |
| [C06](#c06) | Medium | Developer gap | No contributor recipe; the extension skill routes to the wrong path; the catalog test pins positions |
| [C07](#c07) | Medium | Harness limitation | Package goldens assume OLM, a CRD-gated first phase and no `Before` phase, and bypass production composition |
| [C08](#c08) | Medium | UX gap | The listing and help do not carry what a first binding needs |
| [C09](#c09) | Medium | Reproduced semantics | Environment defaults select packages on every binding, with no opt-out and a misleading duplicate diagnostic |
| [C10](#c10) | Medium | Known gap | `preflight add-ons` is unimplemented; native checks first run after bootstrap |
| [C11](#c11) | Medium | Design limit | No named preset: profiles cannot carry packages |
| [C12](#c12) | Low | Hygiene | Orphan help goldens for the retired `add-ons add` and `add-ons delete` |
| [C13](#c13) | Low | Tracked docs defects | Coverage example `credentials`; misleading `nmstate` diagnostic beside an unresolved reference |
| [C14](#c14) | Low | Spec gap | The whole-seconds `timeout` rule is not in the API contract |
| [C15](#c15) | Medium | Planned gap | Authored packages cannot run; the minimal B90 is smaller than the deferred design |
| [C16](#c16) | Low | Examples gap | Placeholder channels and CSVs are marked in READMEs only; no `kubevirt`, multi-binding, `afterRefs` or defaults example |
| [C17](#c17) | Low | Spec and code drift | The duplicate-package remedy differs between the admission table and the code |
| [C18](#c18) | Low | Contract duplication | A package's default `timeout` lives in the schema and in `Budget` with no parity test |
| [C19](#c19) | Low | Hygiene | The budget ceiling, the name limit and the default-catalog list are repeated literals |
| [C20](#c20) | Medium | Qualification gap | Exit evidence names a coverage-page parity test that does not exist; schema-to-type agreement is tested on minimal inputs only |

### C01

**Evidence.** [execution.go:597-606](../../internal/reconciliation/lifecycle/execution.go)
refuses an apply over a completed apply when the input digest changed;
[execution.go:649-662](../../internal/reconciliation/lifecycle/execution.go)
names the change and the remedy `destroy` then `apply`.
[add-ons.md:152-155](../../specs/add-ons.md) states that an instance is never
added to or removed from a completed apply. [D153](../../specs/milestones/backlog.md#d153)
admits exactly three apply scenarios and names the recipe for a fourth.
[B769](../../specs/milestones/backlog.md#b769) parks online removal.

**Impact.** "Add an optional add-on as needed" means, after bootstrap, take
back the whole context. On a physical cluster that is a reinstall. The limit
is correct for the safety model and is stated in the specs; it is stated
nowhere an operator starts.

**Counterevidence.** An incomplete apply continues over unchanged input, and
`--until` lets an operator stage packages that are already declared. The
engine is a DAG of blocks with per-operation ownership marks, so an additive
operation is a bounded extension, not a redesign.

**Correction and exit evidence.** State the limit in the operator guide and
the `add-ons` help now. If day-2 addition is wanted, define the additive
scenario of [the design](#default-and-optional-add-ons) under D153's recipe.
Exit: a context applied without packages gains one instance by a second
operation that reruns no machine, cluster or network block; repeat apply
settles; interruption continues; a foreign object refuses; `destroy` removes
both operations in order; the state-machine rows and `TestApplyAdmitsOnlyTheThreeScenarios`
move to four.

### C02

**Evidence.** [docs/operator-guide.md](../../docs/operator-guide.md): 722
lines, zero occurrences of "add-on"; its lab table (lines 455-460) names
lab-rhel, lab-sno and lab-baremetal. [specs/cli.md](../../specs/cli.md)
section headings list no add-on journey; its only mention is a write-safety
sentence (line 434). The help root for `add-ons` says "Manage add-ons" with one
command ([help-add-ons.golden](../../internal/cli/testdata/help-add-ons.golden)).

**Impact.** An operator who has not read `specs/add-ons.md` does not learn
that packages install after the cluster, that channel and CSV are theirs to
choose, that `--until` exists, that a settled apply contacts no cluster, or
that a change means destroy. The examples' READMEs say most of this well, one
example at a time.

**Correction and exit evidence.** One operator-guide section, "Add-ons",
linking the listing, the examples, the lab-sno runbook flags, the readiness
wait, `--until`, the change rule and retirement, each as a link to its owning
spec section. Reword the `add-ons` help to "List the add-on packages this
build offers". Exit: `make docs-check`; `TestDocsCommandLinesMatchTheCatalog`
over any command line the section quotes.

### C03

**Evidence.** The `duration` lexical accepts any positive Go duration
([lexical.go:134-136](../../api/v1alpha1/lexical.go)); package admission checks
positivity and the `5h50m` ceiling only
([instances.go:205-208](../../internal/addons/instances.go)); rendering
requires whole seconds ([render.go:30-32](../../internal/addons/instance/render.go)).
`Unsupported` recognizes only four bound strings
([selection.go:36](../../internal/addons/instance/selection.go)). `Plan`
returns the render error unchanged
([capability.go:56-58](../../internal/addons/instance/capability.go)) and the
planner passes it through ([planning.go:99-101](../../internal/reconciliation/lifecycle/planning.go)).

**Reproduction (probe P1).** `timeout: 1500ms` under `packages.nmstate` in a
copy of lab-sno-nmstate: `validate` exits 0 with no advisory; the compiled
state has no diagnostics; `Unsupported` is empty; `Plan` returns
`*errors.errorString` `package nmstate timeout must use whole seconds`, with
no code, field or remedy. `500ms` behaves the same; `1.5m` plans.

**Impact.** Validated input cannot plan, and the refusal is a bare string.
The same path carries every other render error: a decode failure, the
KubeVirt hosted-namespace conflict
([kubevirt.go:81](../../internal/addons/packages/kubevirt.go)) and any future
package's render check. None reaches the operator as a diagnostic with a
field.

**Correction and exit evidence.** Two parts. Admit the whole-seconds rule at
`ValidatePackages` with `api.value` at `$.spec.packages.<name>.timeout` and a
remedy, and state it in the API ([C14](#c14)). Independently, make a render
error a `lifecycle.state` refusal that names the package, cluster and binding,
by wrapping at [capability.go:56-58](../../internal/addons/instance/capability.go)
or by extending the refusal table. Exit: `500ms`, `1500ms`, `1s`, `1.5m`,
`5h50m` and `5h50m1s` through production compilation and planning; a probe
package whose `Render` errors yields a refusal with a field.

### C04

**Evidence.** `Package.PlacementNamespaces` is declared
([packages.go:36](../../internal/addons/packages.go)) and used when the
program is frozen ([render.go:53](../../internal/addons/instance/render.go)),
but `Request.Objects()` rebuilds the allowlist from `r.Package == "gitops"`
([requests.go:61-63](../../internal/addons/instance/requests.go)), and
`Render` ends by calling `Canonical()`, which calls `Objects()`
([requests.go:71-116](../../internal/addons/instance/requests.go)). The spec
allows a placement in "one the package's registry entry names as its
operator's namespace" ([owned Secrets](../../specs/add-ons.md#owned-secrets)).

**Reproduction (probe P4).** The `gitops` descriptor with a bootstrap credential
renders. The same descriptor renamed `fixture`, `PlacementNamespaces`
unchanged, fails `step 4: Secret placement
openshift-gitops/bootwright-bootstrap-repository: invalid source or
namespace` from `Canonical()`, and `Objects()` on the returned request fails
the same way.

**Impact.** The declared mechanism works for one package by name. The next
package that places a Secret in an operator-created namespace edits generic
runtime code and a frozen-request reader. No current package is affected.

**Correction and exit evidence.** Carry the allowlist in the frozen request
(a `placementNamespaces` member beside `secrets`), validated at
`Canonical()` against the registry entry at plan time and read back at thaw
with no name switch. That moves `addon-instance-request-v2` and the content
digest, so it lands with a contract decision and a trust review, as the
[Go reference](../../.agents/skills/code-implementation/references/go.md#contract-versions-and-content-digests)
requires. Exit: two synthetic packages with different declared namespaces
render, canonicalize and thaw; an undeclared namespace and a tampered
request fail; the GitOps placement goldens are unchanged.

### C05

**Evidence.** The four places in the
[developer journey](#developer-journey) table:
[packages.go:157](../../internal/addons/packages.go),
[packages.go:192](../../internal/addons/packages.go),
[instances.go:155-160](../../internal/addons/instances.go),
[requests.go:61-63](../../internal/addons/instance/requests.go). The reader
rule errors at [packages.go:188](../../internal/addons/packages.go) and
`Builtin()` panics on a registry error
([registry.go:10-16](../../internal/addons/packages/registry.go)).

**Impact.** A contributor adding `certmgr`, the next package the specs name,
cannot register it alone: `certManager` has no reader until `gitlab` exists.
The planned order, X153 before X154, requires editing the literal list or
registering a consumer first. The approval allowlist and the MetalLB branch
teach a contributor that package policy lives in generic code.

**Counterevidence.** Each leak is small, tested and currently correct. The
reader rule protects a real invariant (a provided token that nothing reads is
dead API), and the approval allowlist is a deliberate safety gate.

**Correction and exit evidence.** Replace the literal reader list with the
API's defined-token list, which the spec already names as the second source
of meaning ([ClusterAddon](../../specs/api/addons.md#clusteraddon)), and let a
package declare `Readers` for a token it provides ahead of its consumer. Move
the approval policy's legality to a descriptor field the coverage parity test
checks against the coverage row. Move the MetalLB defaulting into its
`Normalize` hook with byte-identical effective output. [C04](#c04) covers the
fourth. Exit: a registry with a `certManager` provider and no consumer
constructs; the MetalLB effective-state and request goldens are unchanged.

### C06

**Evidence.** [docs/development.md](../../docs/development.md) has no add-on
section. The [extend-implementation skill](../../.agents/skills/extend-implementation/SKILL.md)
names "add-on driver" and routes to
[architecture.md:426-450](../../specs/architecture.md), the lifecycle-capability
and new-implementation recipes, which a built-in package does not use
([architecture.md:1211-1218](../../specs/architecture.md) says one
implementation serves every package).
[service_test.go:19](../../internal/addons/catalog/service_test.go) pins the
package count and three positional indexes, so every addition edits it. The
`kubevirt` commit touched 17 files, 389 lines.

**Impact.** A contributor either overbuilds a capability or infers the touch
set from `nmstate` and the tests, and discovers the process cost (coverage
row, API section, milestone item, knowledge page, listing goldens) by
failing gates.

**Correction and exit evidence.** One recipe, owned by the extension skill or a
new `add-builtin-package` skill, listing the touch set in order: schema member
and Go type, descriptor with `Normalize`, `Admit` and `Render`, registry
line, package test with render golden, the listing goldens, the coverage row
and API section, the knowledge page for native facts, the milestone item, and
the gates. Make the catalog test derive its expectations from the registry.
Exit: a contributor adds a synthetic package from the recipe touching no
generic Go, and the parity tests name any remaining step.

### C07

**Evidence.** [golden_test.go:139-146](../../internal/addons/packages/golden_test.go)
composes `After` only and dereferences `p.OLM.Namespace`;
[registry_test.go:99-103](../../internal/addons/packages/registry_test.go)
requires an `Established` CRD gate as the first `After` phase of every
package but `gitops`; production composition supports an absent `OLM` and
both phase groups ([render.go:33-48](../../internal/addons/instance/render.go)).

**Impact.** A manifest-only package, a `Before` phase, or an operand without a
CRD gate requires editing the shared harness before it can be tested; the
goldens pin a program the harness built, not the one `Render` freezes.

**Correction and exit evidence.** Drive the goldens through `instance.Render`
with a fixed `ClusterFacts` and controller, and pin the request's canonical
bytes; make the CRD-gate check a per-package expectation. Exit: a synthetic
non-OLM package and a `Before` phase package round-trip through the harness;
existing goldens change only in their wrapper, with the program bytes
unchanged.

### C08

**Evidence.** [output_addons.go:33-37](../../internal/cli/output_addons.go)
prints six columns; [service.go:29-41](../../internal/addons/catalog/service.go)
derives configuration from top-level member names only; the help example is
`--output json` ([help-add-ons-list.golden:16-17](../../internal/cli/testdata/help-add-ons-list.golden)).
The REQUIRES column carries capability tokens
([built-in packages](../../specs/add-ons.md#built-in-packages-and-selection)),
not prerequisites such as an entitlement in the pull secret or a trust bundle
([fdf](../../specs/api/addons.md#package-fdf)).

**Impact.** The listing answers "what exists" and not "what do I write". The
required nested fields, the PackageManifest lookup and the package's
prerequisites are in the examples and the API page, one document hop away.

**Counterevidence.** The listing is correctly a catalog: deterministic,
offline, derived from the schema. The admission diagnostics then carry an
operator from a wrong binding to a right one in few iterations (probe P2,
P3d).

**Correction and exit evidence.** [Design item 1](#default-and-optional-add-ons):
a per-package description and binding skeleton derived from the schema and a
one-line purpose, a "requires" legend, and a link to the examples in the help.
Exit: the skeleton validates after the two placeholders are replaced; the
output is identical with and without a context; unknown names get the remedy
`run bootwright add-ons list`.

### C09

**Evidence.** [defaults.go:12-60](../../internal/desiredstate/compilation/defaults.go)
inherits member by member into a nonempty record and returns an explicit
empty record unchanged (line 24); [kind defaults](../../specs/api/environment.md#kind-defaults)
rules 3 and 4. The `spec.packages` record has optional members, so a default
member is a new selection. Probe P3 observed the five rows of the
[defaults table](#default-and-optional-add-ons).

**Impact.** A default meant as "the nmstate values every binding should
share" becomes "install nmstate on every cluster with any binding", and a
binding cannot keep its other packages while declining it. The duplicate
diagnostic on P3c names `ClusterAddonBinding/metallb`, which never declared
`nmstate`; only `render effective --origins` shows why.

**Correction and exit evidence.** Document the semantics in the API page's
normalization section and the operator guide. Add the inherited origin to the
duplicate and selection diagnostics ("inherited from Environment defaults").
Decide whether selection through defaults is wanted; if not, make
`defaults.ClusterAddonBinding.packages` apply only to members the binding
already declares, which is one condition in `inherit` for that shape, and a
contract change the owner must accept. Exit: P3a to P3d as admission tests
with the chosen semantics; origins named in the diagnostics.

### C10

**Evidence.** [preflight/service.go:18-22](../../internal/addons/preflight/service.go)
returns `ErrNotImplemented`; the help says "recognized but not implemented"
([help-preflight-add-ons.golden](../../internal/cli/testdata/help-preflight-add-ons.golden));
[commands.md:136](../../specs/cli/commands.md) carries the Not-yet-met line;
[B770](../../specs/milestones/backlog.md#b770) is parked.

**Impact.** A wrong channel, a CSV the catalog no longer serves, or an
unreachable catalog is found by the instance after the cluster installed, as
`ResolutionFailed` naming the message
([OLM installation](../../specs/add-ons.md#olm-installation)). That is honest
and bounded, and it is the expensive moment.

**Correction and exit evidence.** Keep B770's scope; revive it with the first
operator request, and let `preflight all` include it. Exit as the item states,
plus proof of no create, patch or delete and no Secret bytes in output.

### C11

**Evidence.** [addons.go:89-91](../../api/v1alpha1/addons.go): a profile holds
`profileRefs` and `addonRefs` only; Q26 (a) chose "profiles cannot hold
packages" ([m4.md:132](../../specs/milestones/m4.md)). Authored references are
warned `api.unrealized` and refused at plan
([unrealized.go:25-26](../../api/v1alpha1/unrealized.go)).

**Impact.** An estate with three cluster shapes and two package sets cannot
name the sets; it duplicates bindings or uses templates. The public profile
and add-on kinds invite authoring what cannot run, although the warning is
clear.

**Correction and exit evidence.** [Design item 2](#default-and-optional-add-ons):
presets only with a concrete repeated consumer, as an amendment to Q26, with
the precedence rule and origins. Exit: nested profiles, a package supplied by
two profiles, a binding override and a last package through a profile, each
with its diagnostic.

### C12

**Evidence.** [help-add-ons-add.golden](../../internal/cli/testdata/help-add-ons-add.golden)
and [help-add-ons-delete.golden](../../internal/cli/testdata/help-add-ons-delete.golden)
describe commands Q30 removed ([m4.md:136](../../specs/milestones/m4.md));
[help_test.go:19-35](../../internal/cli/help_test.go) walks the live command
tree, so neither file is compared; `add-ons add` is an invalid invocation
([validation_test.go:19](../../internal/cli/validation_test.go)).

**Correction.** Delete both files. The `add-ons` group help also lists
`--context` among its common flags
([help-add-ons.golden:14](../../internal/cli/testdata/help-add-ons.golden))
although its only command reads no context; drop it from the group. Exit:
`make quick`.

### C13

Already tracked: [B884](../../specs/milestones/m4.md#b884) owns the coverage
example's `credentials` ([add-on-coverage.md:260](../../specs/add-on-coverage.md))
and the stale state-reconciliation line; [B886](../../specs/milestones/m4.md#b886)
owns the second `nmstate` diagnostic beside an unresolved reference. Both
affect the operator journey; neither needs a new carrier.

### C14

**Evidence.** [api/addons.md:298-300](../../specs/api/addons.md) says a
positive Go duration of at most `5h50m`; [render.go:30-32](../../internal/addons/instance/render.go)
adds whole seconds. **Correction.** With [C03](#c03): state the rule where the
field is owned, and admit it.

### C15

**Evidence.** [B90](../../specs/milestones/m4.md#b90) is "Needs definition",
scheduled after M4f, with open questions on payload freezing, descriptor
identity, typed configuration and qualification; the authored `olm` arm's
defaults differ from the package Subscription rules, which the API records as
Not yet met ([api/addons.md:130-139](../../specs/api/addons.md)). The mapping
in the [developer journey](#developer-journey) names what the program already
covers and the five constructs it does not.

**Impact.** A team with an operator Bootwright does not ship waits for a
release, and the deferred design reads as a large program (catalog records,
drivers, a host interface) that is not what the first step needs.

**Correction and exit evidence.** Define B90's first slice as "an authored OLM
`ClusterAddon` restricted to the shapes the program supports, compiled into
the same request by the same capability, with no typed configuration and no
manifest payload": managed namespace, own or all namespaces group, Manual
approval with a required `startingCSV`, custom resources each with an
authored gate, readiness from the three arms. Freeze the normalized
descriptor's digest into the request so a changed definition refuses
continuation as a changed package does. Manifest sets, payload freezing in
`context init`, and catalogs follow as their own slices. The authored
`catalogSource` also lacks the `priority` the package block requires
([addons.go:48-52](../../api/v1alpha1/addons.go) against
[addons.go:136-138](../../api/v1alpha1/addons.go)); the alignment the API
already records as Not yet met closes with that slice. Exit: the fake-cluster
journey over an authored package equals the built-in journey; a definition
edited after freezing refuses; the trust rules of
[the boundary](../../specs/add-ons.md#trust-boundary) hold by test.

### C16

**Evidence.** [lab-sno-nmstate binding](../../examples/add-ons/lab-sno-nmstate/add-ons/binding.yaml)
carries `example-channel` and `kubernetes-nmstate-operator.v0.0.0` with no
comment; the warning is in the README only. [lab-compact-hub](../../examples/add-ons/lab-compact-hub/add-ons/binding.yaml)
uses realistic-looking pins. No example shows `kubevirt`, two bindings on one
cluster, `afterRefs` or Environment defaults for a binding.

**Impact.** A binding copied without its README looks complete and fails at
the subscription step after the cluster installed. The ordering and defaults
features have no worked input.

**Correction and exit evidence.** A one-line placeholder comment in each
example binding; one example with two bindings and `afterRefs`; the
`kubevirt` example once its gate (B732) has an input. Exit: the examples
validate and plan as the example-plan golden test requires.

### C17

**Evidence.** [api/addons.md:275](../../specs/api/addons.md) gives the remedy
`select package <package> for <cluster> in one ClusterAddonBinding only`;
[instances.go:264](../../internal/addons/instances.go) emits `bind <package>
to <cluster> through one ClusterAddonBinding only` for packages and add-ons
alike, as probe P3c observed.

**Correction and exit evidence.** Make one of them match the other, and hold
the diagnostics table to the code with a test as the refusal table is held.
Exit: the admission golden for a duplicate package.

### C18

**Evidence.** Each package's schema carries its own `timeout` default
([addons.go:140-143](../../api/v1alpha1/addons.go),
[addons_hub.go:58-64](../../api/v1alpha1/addons_hub.go),
[addons_storage.go:17](../../api/v1alpha1/addons_storage.go)), and each
descriptor carries a `Budget` that `NormalizePackages` uses as a second
default ([instances.go:137](../../internal/addons/instances.go)). On the
compiled path the schema default wins, so the effective state shows `30m` and
the `Budget` default fires only when a test bypasses the schema. The six
pairs agree today; nothing tests that they do.

**Impact.** A contributor who reuses `packageBaseSchema()` for a package
whose `Budget` is `20m` ships a `30m` default silently; the API page states
one value and the request carries another.

**Correction and exit evidence.** Derive the schema default from `Budget`,
or add one registry test asserting equality per member. Exit: that test.

### C19

**Evidence.** The `5h50m` ceiling is written at
[packages.go:171](../../internal/addons/packages.go),
[instances.go:207](../../internal/addons/instances.go) and
[requests.go:75](../../internal/addons/instance/requests.go); the nine-byte
name limit at [packages.go:171](../../internal/addons/packages.go) with no
test deriving it from the block-identity bound; the default-catalog list at
[instances.go:218](../../internal/addons/instances.go) and
[fdf.go:22](../../internal/addons/packages/fdf.go).

**Correction.** One exported constant each, with a test that the name limit
still fits `BlockID` within `ValidSegment`.

### C20

**Evidence.** The exit evidence of B735, B883 and five other M4 items names
"the coverage-page parity test"
([m4.md:694, 828, 1104, 1446, 1688, 1765, 1894](../../specs/milestones/m4.md)),
and [package qualification](../../specs/add-ons.md#package-qualification)
requires "parity with its coverage row" and that "its schema and its Go
configuration type agree". No test in the tree mentions parity; the only
reference to the coverage page is a link-count entry
([docs_gates_test.go:517](../../test/architecture/docs_gates_test.go)).
Schema-to-type agreement is exercised only by each package's minimal test
configuration, and `Decode` rejects unknown members
([packages.go:114-124](../../internal/addons/packages.go)), so a schema member
absent from the Go type surfaces as a plan-time render error ([C03](#c03)).

**Impact.** Delivered items cite a gate that was never run, and a schema
field added without its Go field passes every test until an operator sets it.

**Correction and exit evidence.** A table-driven test over `Builtin()` that
builds each package's schema-maximal configuration, decodes it into the
package's Go type and renders it; and either a real parity test (registry
entries against the coverage table's package, provides, requires and
configuration columns) or the removal of the phrase from the exit evidence of
the delivered items. Exit: the test exists and the milestone text names it.

## Delivery sequence

Proposed work packages for the owner to authorize; this audit activates
nothing and creates no items.

| Order | Outcome | Findings | Carrier | Exit evidence |
| --- | --- | --- | --- | --- |
| 1 | Operator entry journey: guide section, help wording, listing with description and binding skeleton, defaults semantics documented, example placeholders | C02, C08, C09 (docs part), C01 (statement), C16, C17 | one M4 documentation and CLI item; Add-ons with CLI | `make docs-check`; listing goldens; skeleton validates |
| 2 | Admission agrees with planning; render errors are refusals; schema-maximal decode test | C03, C14, C20 | one M4 defect item; Add-ons with API | the duration cases through production compile and plan; a failing `Render` yields a refusal with a field; the decode test over every package |
| 3 | Placement allowlist in the frozen request | C04 | one M4 safety item; Add-ons with Secrets; moves the request version | two synthetic packages; tampered request refused; GitOps goldens unchanged |
| 4 | Registry without name leaks; harness over production composition; contributor recipe; one source for each limit and default | C05, C06, C07, C12, C18, C19 | one M4 enabling item; Add-ons | `certManager` provider registers alone; MetalLB output byte-identical; synthetic non-OLM and `Before` packages pass; a contributor exercise; the timeout parity test |
| 5 | Additive apply scenario | C01 | a new lifecycle item under D153's recipe; Reconciliation with Add-ons and Secrets; owner decision first | the exit evidence of [C01](#c01) |
| 6 | Named presets | C11 | amend Q26 only with a repeated consumer | precedence and origin tests |
| 7 | Add-on preflight | C10 | B770 as parked | B770's exit |
| 8 | Authored OLM packages, first slice | C15 | B90, defined as [C15](#c15) states | journey parity; frozen descriptor digest |

Items 1 to 4 are independent of each other and of any owner decision beyond
authorization; 3 and 4 touch the registry and should land in that order.
Item 5 is the only way to the day-2 meaning of "as needed"; item 8 is the only
way to add-ons without a rebuild; they solve different problems and neither
replaces the other.

## Verification and limits

Method: the orchestrator read the owning specs, the API, the registry and
packages, the instance capability, the CLI, the examples, the e2e case, the
milestone items and decisions, and the commits that added `kubevirt`, `acm`
and `metallb`. Two Opus reviewers walked the operator and developer journeys
independently with the same read-only constraints and without this report;
their findings were checked against the source before adoption. Adopted from
them after verification: the two `fdf` literal paths in [C05](#c05), and
[C16](#c16) to [C20](#c20). Not adopted: a claim that `Plan`'s generic
refusal text reaches the operator, because `refuseUnsupported` runs before
planning on every path
([execution.go:805](../../internal/reconciliation/lifecycle/execution.go),
[diff.go:204](../../internal/reconciliation/lifecycle/diff.go)); and a claim
that `destroy and apply again` dead-ends on a physical cluster, because the
apply reinstalls the machines, which [C01](#c01) already states as the cost.
One Sonnet probe run reproduced the candidate defects; its probe files were
deleted and the tree is clean. No command with effects ran: no apply, destroy, controller
install, media, context, machine or cluster command, and no cluster or host
was reached.

| Check | Result |
| --- | --- |
| `make build` | passed, exit 0 |
| `bin/bootwright add-ons list`, `add-ons --help` | passed; six packages; one command |
| `bin/bootwright validate -f <lab-sno-nmstate copy with timeout: 1500ms>` | exit 0, no advisory (P1a) |
| probe `TestAuditProbeTimeout` in `cmd/bootwright` over the same input | `Plan` error `*errors.errorString` `package nmstate timeout must use whole seconds`; `1.5m` plans (P1b, P1c) |
| `bin/bootwright validate -f <copy with packages.certmgr>` and `<packages.metalb>` | exit 1, `api.field` `unknown field; spec.packages permits nmstate, kubevirt, metallb, fdf, acm, gitops`; remedy `rename it to metallb` for the typo (P2) |
| probe over Environment defaults `ClusterAddonBinding.packages.nmstate` | P3a both instances selected on one binding; P3b `the binding selects no profile, add-on or package`; P3c `api.duplicate` on the explicit binding naming the inheriting one; P3d `api.required` `subscription` and `api.value` `source` |
| probe `TestAuditProbePlacement` in `internal/addons/instance` | `gitops` renders; renamed `fixture` fails `step 4: Secret placement openshift-gitops/bootwright-bootstrap-repository: invalid source or namespace` from `Canonical()` and `Objects()` (P4) |
| `git status --porcelain` after the probes | empty |
| `make docs-check` after writing this report | see the commit that carries it |
| `make check`, `make race`, live runs | not run; no product code changed |

Retained probe output, at the baseline on 2026-10-30:

```text
P1 (timeout: 1500ms)
  validate: ✓ Desired state is valid (files seen: 16, objects decoded: 16)   exit 0
  compile diagnostics: none
  len(Unsupported)=0
  Plan err: package nmstate timeout must use whole seconds
  %T: *errors.errorString
P1 (timeout: 1.5m)
  Plan err: <nil>

P3a (defaults nmstate; binding selects metallb)
  validate: ✓ Desired state is valid   exit 0
  effective ClusterAddonBinding/metallb spec.packages: nmstate (inherited), metallb
  addons.Instances: package=metallb binding=metallb cluster=sno
                    package=nmstate binding=metallb cluster=sno
P3c (defaults nmstate; bindings metallb and nmstate on sno)
  error  ClusterAddonBinding/nmstate  api.duplicate  $.spec.packages.nmstate
  package nmstate is already bound to OpenShiftCluster/sno by ClusterAddonBinding/metallb

P4
  Render(gitops) err=<nil>
  Render(fixture) err=step 4: Secret placement openshift-gitops/bootwright-bootstrap-repository: invalid source or namespace
```

Limits. The probe run could not use `bin/bootwright render --input-dir
--output-dir` in its sandbox: the command refused with `runtime.privilege`
("sudo authorization could not be obtained") before writing anything, so the
effective state for P3 was produced by the compiler's YAML encoder in a probe
test instead. Whether a render that writes only local artifacts should need
sudo was not investigated and is outside this audit's scope; the owner may
want to check it. No live catalog, channel or CSV was consulted, and no claim
here depends on a vendor fact. The milestone's operator gates (B755, B757,
B761, B766, B732) remain open exactly as [M4](../../specs/milestones/m4.md)
records them.
