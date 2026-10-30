# Add-ons audit: consolidated report

Consolidation of [report-codex.md](report-codex.md) (Codex, 2026-10-29,
baseline `2f35ee8c3`) and [report-claude.md](report-claude.md) (Claude Fable
5.1 with Opus and Sonnet agents, 2026-10-30, baseline `4294e7520`). Written
by Claude on 2026-10-30 after both reports existed. No add-on code, schema,
spec, example or CLI file changed between the two baselines
(`git diff --stat 2f35ee8c3..4294e7520` over `internal/addons`,
`api/v1alpha1/addons*.go`, the three add-on specs, `internal/cli/*addons*`,
`examples/add-ons` and `e2e/lab-sno` is empty), so every citation of either
report resolves at `4294e7520`, and the two assessments compare directly.

Method of the merge: each Codex finding was re-read against the tree and
against Claude's probes; each Claude finding was checked for a Codex
counterpart; where the reports disagree on severity, scope or remedy the
disagreement is recorded with the reasoning that settled it. Codex's P1, P2
and P3 map to High, Medium and Low. This document authorizes no
implementation and changes no milestone state; it is a plan for the owner.

Reading order: [verdict](#verdict), [agreement map](#agreement-map),
[consolidated findings](#consolidated-findings),
[where the reports disagree](#where-the-reports-disagree),
[what each report missed](#what-each-report-missed),
[merged design](#merged-design), [delivery sequence](#delivery-sequence),
[verification](#verification).

## Verdict

The two reports agree on the shape of the problem and on the two defects:

- The built-in package model, the closed cluster object program, the
  create-only adapter with receipts, exact OLM approval and offline listing
  are sound and should stay as they are.
- A package can be installed only during bootstrap, by an explicit binding
  with an operator-chosen channel and CSV; after a completed apply nothing can
  be added, changed or removed without `destroy` then `apply`. "Optional
  add-ons added as needed" therefore needs a new, bounded additive lifecycle
  scenario; no catalog, preset, default or preflight work substitutes for it.
- Two defects reproduce without effects: a fractional-second `timeout` passes
  `validate` and fails `plan` with a bare error, and the Secret-placement
  namespace allowlist is rebuilt from the literal package name `gitops` when a
  request is thawed, so no second package can use the declared mechanism.
- Environment kind defaults select a package on every binding that has a
  nonempty `packages` map, with no per-package opt-out; Codex derived this
  from the code and Claude reproduced it, including a duplicate diagnostic
  that blames the binding that inherited the package.
- Authored add-ons (B90) need a bounded first slice, not the deferred
  design's catalogs, drivers and host interface.

The reports differ in emphasis. Codex concentrates on the lifecycle limit, the
listing and the two defects, and frames the rest as design. Claude adds the
operator documentation gap as a High finding, widens the "package name in
generic code" finding from one place to six (one of which blocks the next
planned package from registering), adds a qualification-honesty finding (seven
exit-evidence lines cite a coverage-page parity test that does not exist) and
four Low hygiene findings, and gives the authored-package route a concrete
mapping onto the program the code already has. Codex ran the full gate;
Claude ran the probes and the build only.

Consolidated: 19 findings, 2 High, 11 Medium, 6 Low; 2 reproduced defects
and 1 reproduced extension defect; 1 intentional limit; the rest gaps in
docs, UX, contract and harness. The recommended order is unchanged from both
reports: entry journey and listing first, then the small code defects, then
the developer recipe and harness, then the additive scenario if the owner
wants day-2 additions, then B90.

## Agreement map

| Consolidated | Codex | Claude | Disposition |
| --- | --- | --- | --- |
| [F01](#f01) | A01 | C01 | both; same evidence; Claude adds the scenario's invariants |
| [F02](#f02) | none | C02 | Claude only; verified (zero occurrences in the guide, no CLI journey) |
| [F03](#f03) | A06 | C03, C14 | both reproduced; Claude generalizes to every render error and names the error type |
| [F04](#f04) | A07 | C04 | both reproduced with the same synthetic rename |
| [F05](#f05) | A09 | C05 | Codex names one place; Claude verifies six, including the reader allowlist that blocks `certmgr` |
| [F06](#f06) | A10 | C06 | both; Claude adds the positional catalog test and the commit's size |
| [F07](#f07) | A08 | C07 | both; Claude adds the CRD-gate-first assertion |
| [F08](#f08) | A04 | C08 | both; Claude's probes add counterevidence on admission quality |
| [F09](#f09) | journeys table | C09 | Codex inferred; Claude reproduced four rows and the misleading duplicate message |
| [F10](#f10) | A05 | C10 | both; identical scope (B770) |
| [F11](#f11) | A03 | C11 | both; same Q26 evidence |
| [F12](#f12) | A02 | C15 | both; Claude adds the program-vocabulary mapping and the `priority` gap |
| [F13](#f13) | none | C20 | Claude only; verified against seven milestone lines and the test tree |
| [F14](#f14) | none | C12 | Claude only; verified |
| [F15](#f15) | caveats | C13 | both; already tracked as B884 and B886 |
| [F16](#f16) | none | C16 | Claude only; verified |
| [F17](#f17) | none | C17 | Claude only; verified by probe output against the spec row |
| [F18](#f18) | none | C18 | Claude only; verified |
| [F19](#f19) | none | C19 | Claude only; verified |

Every Codex finding has a consolidated entry; no Codex finding was refuted.
Two Codex design sections were narrowed rather than adopted (see
[disagreements](#where-the-reports-disagree)).

## Consolidated findings

Severity: High blocks or misleads the requested journey; Medium costs a
document round-trip or an edit to generic code; Low is hygiene. Evidence
lines are at `4294e7520`; each entry links the fuller discussion in the
source reports instead of repeating it.

| ID | Severity | Class | Finding | Sources |
| --- | --- | --- | --- | --- |
| [F01](#f01) | High | Intentional limit | No package can be added, changed or removed after a completed apply | A01, C01 |
| [F02](#f02) | High | Docs gap | The operator guide and the CLI specification have no add-on journey | C02 |
| [F03](#f03) | Medium | Reproduced defect | A render-time refusal has no diagnostic; fractional `timeout` passes `validate` | A06, C03, C14 |
| [F04](#f04) | Medium | Reproduced extension defect | The placement allowlist is rebuilt from the package name at thaw | A07, C04 |
| [F05](#f05) | Medium | Developer gap | Generic code keys on package names in six places; a provider cannot register before its consumer | A09, C05 |
| [F06](#f06) | Medium | Developer gap | No contributor recipe; the extension skill routes to the wrong path | A10, C06 |
| [F07](#f07) | Medium | Harness limitation | Package goldens assume OLM, a CRD-gated first phase and `After` only | A08, C07 |
| [F08](#f08) | Medium | UX gap | The listing and help do not carry what a first binding needs | A04, C08 |
| [F09](#f09) | Medium | Reproduced semantics | Environment defaults select packages on every binding; no opt-out; misleading duplicate message | Codex journeys, C09 |
| [F10](#f10) | Medium | Known gap | `preflight add-ons` is unimplemented | A05, C10 |
| [F11](#f11) | Medium | Design limit | No named preset; profiles cannot carry packages | A03, C11 |
| [F12](#f12) | Medium | Planned gap | Authored packages cannot run; the minimal B90 is small | A02, C15 |
| [F13](#f13) | Medium | Qualification gap | Exit evidence cites a coverage-page parity test that does not exist | C20 |
| [F14](#f14) | Low | Hygiene | Orphan help goldens for retired commands; `--context` on the group | C12 |
| [F15](#f15) | Low | Tracked docs defects | Coverage example `credentials`; second `nmstate` diagnostic | B884, B886 |
| [F16](#f16) | Low | Examples gap | Placeholders marked in READMEs only; missing examples | C16 |
| [F17](#f17) | Low | Spec and code drift | The duplicate-package remedy differs between table and code | C17 |
| [F18](#f18) | Low | Contract duplication | The `timeout` default lives in the schema and in `Budget` | C18 |
| [F19](#f19) | Low | Hygiene | Repeated literals for the budget ceiling, name limit and catalog list | C19 |

### F01

A completed apply refuses changed input by name with `destroy` then `apply`
as the remedy ([execution.go:597-606, 649-662](../../internal/reconciliation/lifecycle/execution.go));
an instance is never added to or removed from a completed apply
([add-ons.md:152-155](../../specs/add-ons.md)); D153 admits three scenarios
and names the recipe for a fourth; B769 parks online removal. Both reports
call this correct for the safety model and absent from where an operator
starts. Codex's exit evidence (a context applied without packages gains one
instance without rerunning machine or installer work; repeat settlement;
continuation; foreign objects refused; removal across both operations) and
Claude's invariants (only additions under `spec.packages` or new bindings on
an installed cluster, proved by the `diff` comparison; every frozen block
keeps its request; a second operation records the first as predecessor;
kubeconfig custody lent across; marks hash the second operation; destroy
takes both back in reverse; `status`, `diff` and the ledger show both)
together define the scenario. Upgrade, reconfiguration and online removal
stay out of it. Detail: [A01](report-codex.md#a01--optional-after-bootstrap-requires-a-new-lifecycle-scenario),
[C01](report-claude.md#c01).

### F02

[docs/operator-guide.md](../../docs/operator-guide.md) has no occurrence of
"add-on" in 722 lines; [specs/cli.md](../../specs/cli.md) has no add-on
journey; the `add-ons` help says "Manage add-ons" with one command. Codex's
journey section and its delivery item 1 ("truthful operator entry journey")
assume this gap without stating it as a finding. The merge rates it High
because it is the first thing an operator meets and the cheapest to close:
one guide section linking the listing, the examples, the runbook flags, the
readiness wait, `--until`, the change rule and retirement to their owning
spec sections. Detail: [C02](report-claude.md#c02).

### F03

`timeout: 1500ms` passes `validate` (the `duration` lexical accepts any
positive duration, [lexical.go:134-136](../../api/v1alpha1/lexical.go);
package admission checks only positivity and `5h50m`,
[instances.go:205-208](../../internal/addons/instances.go)) and fails
planning at [render.go:30-32](../../internal/addons/instance/render.go) with
`package nmstate timeout must use whole seconds`. Both reports reproduced it;
Claude's probe adds that the error is a bare `*errors.errorString` returned
through [capability.go:56-58](../../internal/addons/instance/capability.go)
and [planning.go:99-101](../../internal/reconciliation/lifecycle/planning.go),
and that `Unsupported` maps only four bound strings
([selection.go:36](../../internal/addons/instance/selection.go)), so every
other render error (a decode failure, the KubeVirt hosted-namespace conflict)
reaches the operator the same way. The merged fix: admit whole seconds at
`ValidatePackages` with `api.value` and a remedy, state the rule in the API
page, and make a render error a refusal naming package, cluster and binding.
Codex's alternative of a specified conversion is not adopted: the request
carries `budgetSeconds`, so whole seconds is the honest contract. Detail:
[A06](report-codex.md#a06--package-timeout-validation-and-planning-disagree),
[C03](report-claude.md#c03), [C14](report-claude.md#c14).

### F04

`Package.PlacementNamespaces` is honored when the program is frozen
([render.go:53](../../internal/addons/instance/render.go)) and replaced by a
literal when the request is read back
([requests.go:61-63](../../internal/addons/instance/requests.go)); because
`Render` ends with `Canonical()`, which calls `Objects()`, a descriptor
renamed from `gitops` to `fixture` with the same namespaces fails
`step 4: Secret placement openshift-gitops/bootwright-bootstrap-repository:
invalid source or namespace`. Both reports reproduced this identically. No
shipped package is affected. The merged fix carries the allowlist in the
frozen request, validated against the registry at plan time, which moves
`addon-instance-request-v2` and the content digest and therefore lands with a
contract decision and a trust review; Codex's alternative, injecting the
registry into decode, avoids the version move but keeps a read-time lookup
in a path that should be self-contained. Detail:
[A07](report-codex.md#a07--generic-namespace-permission-becomes-a-gitops-special-case),
[C04](report-claude.md#c04).

### F05

Codex found the MetalLB normalization inline in `NormalizePackages`
([instances.go:155-160](../../internal/addons/instances.go)). Claude verified
five more: the reader allowlist for provided tokens
([packages.go:157](../../internal/addons/packages.go)), which makes a
`certmgr` providing `certManager` panic at `Builtin()`
([packages.go:188](../../internal/addons/packages.go),
[registry.go:10-16](../../internal/addons/packages/registry.go)) until `gitlab`
requires the token, although the planned order is X153 before X154; the
`OperatorManaged` approval legal only for `p.Name == "fdf"`
([packages.go:192](../../internal/addons/packages.go)); the placement
allowlist (F04); and the two literal `packages.fdf.cephConsumerRef` paths in
the consumer-uniqueness rule ([instances.go:177-188](../../internal/addons/instances.go))
and in cluster selection ([bindings.go:198](../../internal/addons/bindings.go)).
Codex's caveat stands: the consumer-uniqueness rule is cross-object and must
stay generic; the fix is a declared field the generic rule iterates, not a
per-instance hook. Merged fix: the API's defined-token list as the reader
source plus a `Readers` declaration for a provider ahead of its consumer; the
approval policy as a descriptor field checked against the coverage row; the
MetalLB defaulting in its `Normalize` hook with byte-identical output. Detail:
[A09](report-codex.md#a09--metallb-normalization-belongs-behind-its-existing-hook),
[C05](report-claude.md#c05).

### F06

No recipe exists; [docs/development.md](../../docs/development.md) is silent;
the [extend-implementation skill](../../.agents/skills/extend-implementation/SKILL.md)
names "add-on driver" and routes to the lifecycle-capability recipes
([architecture.md:426-450](../../specs/architecture.md)), which a built-in
package does not use. The commit that added `kubevirt` touched 17 files and
389 lines, over half milestone and spec text; the catalog test pins the
package count and three positional indexes
([service_test.go:19](../../internal/addons/catalog/service_test.go)). Both
reports recommend one recipe and no generator until a contributor exercise
shows repeated mechanical errors. Detail:
[A10](report-codex.md#a10--the-contribution-entry-point-describes-the-wrong-sized-task),
[C06](report-claude.md#c06).

### F07

The shared golden test dereferences `p.OLM` and composes `After` only
([golden_test.go:139-151](../../internal/addons/packages/golden_test.go)); the
purity test requires an `Established` CRD gate as every non-`gitops`
package's first `After` phase
([registry_test.go:99-103](../../internal/addons/packages/registry_test.go));
production rendering supports an absent `OLM` and both phase groups
([render.go:33-48](../../internal/addons/instance/render.go)). Merged fix:
drive goldens through `instance.Render` with fixed facts and pin the request
bytes; make the CRD-gate check a per-package expectation. Detail:
[A08](report-codex.md#a08--qualification-harness-narrows-the-extension-seam),
[C07](report-claude.md#c07).

### F08

The listing prints names, tokens, OLM package, default source and top-level
member names ([output_addons.go:33-37](../../internal/cli/output_addons.go),
[service.go:29-41](../../internal/addons/catalog/service.go)); nothing marks
required nested fields, points to the PackageManifest of the printed OLM
package, explains `REQUIRES none` or `DEFAULT SOURCE none`, or links the
examples. Counterevidence from Claude's probes, which Codex did not run: an
unknown package member is refused with `unknown field; spec.packages permits
nmstate, kubevirt, metallb, fdf, acm, gitops` and `rename it to metallb` for
a typo, and a missing `subscription` gets `api.required` with
`add subscription (mapping)`; admission carries an operator from a wrong
binding to a right one quickly. Merged fix: Codex's `describe`-style surface,
extended by Claude's schema-derived binding skeleton with placeholder channel
and CSV, a "requires" legend and package-specific remedy text that names the
PackageManifest. Detail:
[A04](report-codex.md#a04--listing-is-useful-but-insufficient-for-first-use),
[C08](report-claude.md#c08).

### F09

[defaults.go:12-60](../../internal/desiredstate/compilation/defaults.go)
inherits member by member into a nonempty record and leaves an explicit
empty record alone. With `defaults.ClusterAddonBinding.packages.nmstate`
present, Claude observed: a `metallb`-only binding selects both; `packages: {}`
inherits nothing and is refused as selecting nothing; `nmstate: {}` selects
without inheriting and fails `subscription`; two bindings on one cluster get
`api.duplicate` on the explicit binding naming the one that inherited. This
matches Codex's table row for row. Merged position: defaults are the right
place for shared values and the wrong place for selection; document the
semantics, name the inherited origin in the diagnostics, and let the owner
decide whether a default may select, which is one condition in `inherit` for
that shape and a contract change. Detail: [Codex journeys](report-codex.md#operator-from-catalog-to-a-selected-package),
[C09](report-claude.md#c09).

### F10

[preflight/service.go:18-22](../../internal/addons/preflight/service.go)
returns `ErrNotImplemented`; the help says so; B770 is parked with a bounded
scope (API through the lent kubeconfig, catalog `READY`, CSV in the channel).
A wrong channel or CSV is found by the instance after the cluster installed.
Both reports keep B770's scope and revive it on the first operator request;
Codex adds that foreign-object detection would be an expansion needing its
own definition. Detail:
[A05](report-codex.md#a05--native-preflight-remains-unavailable),
[C10](report-claude.md#c10).

### F11

Profiles hold `profileRefs` and `addonRefs` only
([addons.go:89-91](../../api/v1alpha1/addons.go)); Q26 (a) chose "profiles
cannot hold packages". Authored references are warned `api.unrealized` at
`validate` and refused at `plan`. Both reports: amend Q26 only with a
repeated consumer, with a precedence rule (binding member over profile over
Environment default; two profiles supplying one package refused) and origins
in `render effective --origins`; never resolve `addonRefs` to built-in names.
Detail: [A03](report-codex.md#a03--profiles-are-not-built-in-presets),
[C11](report-claude.md#c11).

### F12

B90 is "Needs definition" after M4f. Codex's position: minimal B90 before any
marketplace, explicit local definitions, frozen identity, the existing program
and trust boundary. Claude's mapping shows how small the first step is:
`operatorPrelude` and `subscribe` already take an `addons.OLM` value and the
configuration's `subscription` ([render.go:137-202](../../internal/addons/instance/render.go));
the three authored readiness arms map onto program checks; `customResources`
are canonical bodies the program already validates. Not covered, and the
bounds of the first slice: `namespace.management: external`,
`installPlanApproval: Automatic`, an optional `startingCSV`, a
`targetNamespaces` list beyond own or all namespaces, a gate per custom
resource (a CRD name cannot be derived from `apiVersion` and `kind`), and the
authored `catalogSource` lacking `priority`
([addons.go:48-52](../../api/v1alpha1/addons.go) against
[addons.go:136-138](../../api/v1alpha1/addons.go)). Merged first slice: an
authored OLM `ClusterAddon` restricted to those shapes, compiled into the same
request by the same capability, no typed configuration, no manifest payload,
the normalized descriptor's digest frozen in the request; manifest sets,
payload freezing and catalogs as later slices. Detail:
[A02](report-codex.md#a02--third-party-declarative-extension-remains-b90),
[C15](report-claude.md#c15).

### F13

Seven exit-evidence lines on the M4 page cite "the coverage-page parity test"
([m4.md:694, 828, 1104, 1446, 1688, 1765, 1894](../../specs/milestones/m4.md)),
including delivered B735 and B883, and
[package qualification](../../specs/add-ons.md#package-qualification) requires
parity with the coverage row and schema-to-type agreement. No test in the
tree mentions parity; the only reference to the coverage page is a link
count ([docs_gates_test.go:517](../../test/architecture/docs_gates_test.go)).
Schema-to-type agreement is exercised by each package's minimal test input
only, and `Decode` rejects unknown members, so a schema member without its Go
field surfaces as a plan-time render error (F03). Merged fix: a table-driven
schema-maximal decode-and-render test over `Builtin()`, and either a real
parity test or the removal of the phrase from the items that claim it.
Detail: [C20](report-claude.md#c20).

### F14

[help-add-ons-add.golden](../../internal/cli/testdata/help-add-ons-add.golden)
and [help-add-ons-delete.golden](../../internal/cli/testdata/help-add-ons-delete.golden)
describe commands Q30 removed; the help test walks the live command tree, so
neither is compared. The `add-ons` group help lists `--context` although its
only command reads no context. Delete the two files; drop the flag from the
group. Detail: [C12](report-claude.md#c12).

### F15

[B884](../../specs/milestones/m4.md#b884) owns the coverage example's
`credentials` and the stale state-reconciliation line;
[B886](../../specs/milestones/m4.md#b886) owns the second `nmstate`
diagnostic beside an unresolved reference. Both reports: no new carrier.

### F16

Example bindings carry `example-channel` and `v0.0.0` with no comment; the
warning is in each README. No example shows `kubevirt`, two bindings on one
cluster, `afterRefs` or Environment defaults. One placeholder comment per
binding and one multi-binding example. Detail: [C16](report-claude.md#c16).

### F17

[api/addons.md:275](../../specs/api/addons.md) gives `select package
<package> for <cluster> in one ClusterAddonBinding only`;
[instances.go:264](../../internal/addons/instances.go) emits `bind <package>
to <cluster> through one ClusterAddonBinding only`, as the probe observed.
Make them agree and hold the table to the code. Detail:
[C17](report-claude.md#c17).

### F18

Each package's schema carries its own `timeout` default and each descriptor
a `Budget` that `NormalizePackages` uses as a second default; the schema
wins on the compiled path, the six pairs agree today, nothing tests it.
Derive one from the other or add the parity test. Detail:
[C18](report-claude.md#c18).

### F19

The `5h50m` ceiling appears at [packages.go:171](../../internal/addons/packages.go),
[instances.go:207](../../internal/addons/instances.go) and
[requests.go:75](../../internal/addons/instance/requests.go); the default
catalog list at [instances.go:218](../../internal/addons/instances.go) and
[fdf.go:22](../../internal/addons/packages/fdf.go). One constant each.
Detail: [C19](report-claude.md#c19).

## Where the reports disagree

1. **Severity of the documentation gap.** Codex has no P1 documentation
   finding; its highest is A04 at P2. Claude rates C02 High. The merge keeps
   High: the guide is where the owner's operators start, the gap is total,
   and closing it costs one section of links.
2. **Scope of the package-name leak.** Codex's A09 is MetalLB alone and
   warns against moving the fdf uniqueness rule into a hook. Claude's C05
   counts six places. The merge adopts six, keeps Codex's caveat for the
   cross-object rule, and raises the reader allowlist to the decisive case
   because it blocks the next planned package in the planned order.
3. **Timeout remedy.** Codex offers admission of whole seconds or a specified
   conversion; Claude admits whole seconds only. The merge adopts whole
   seconds: the request already carries `budgetSeconds`, and a conversion
   would make the effective state and the request disagree.
4. **Placement allowlist fix.** Codex leaves the mechanism open with a
   trust-boundary review; Claude's agent proposed injecting the registry into
   decode; Claude's report carries the allowlist in the request. The merge
   prefers the request: a frozen request should decode without a registry,
   and the version move is the honest record that the contract changed.
5. **Codex's "minimal authored bundle" and transport sections.** Codex spends
   its design section on a distribution unit, collision rules, an optional
   remote transport, and comparisons with Helm OCI and Krew consulted on the
   web. The merge narrows this: the trust boundary excludes ambient discovery
   and B90 names the input tree as the source, so the first slice is a
   compiled authored OLM definition, and transport is not on any path the
   owner has opened. The comparisons add no repository evidence and are not
   carried forward.
6. **Codex's `add-ons describe`.** Adopted, extended by a schema-derived
   binding skeleton, and placed first in the sequence because the probes show
   admission already does the rest of the work once a skeleton exists.
7. **Claude's Opus reviewer claims not adopted.** That `Plan`'s generic
   refusal text ("selected add-on cannot be realized") reaches the operator:
   not adopted, because `refuseUnsupported` runs before planning on every
   path ([execution.go:805](../../internal/reconciliation/lifecycle/execution.go),
   [diff.go:204](../../internal/reconciliation/lifecycle/diff.go)). That
   `destroy and apply again` dead-ends on a physical cluster: not adopted,
   because the apply reinstalls the machines; the cost is what F01 states.

## What each report missed

Codex did not: run `validate` against wrong bindings, so A04 understates how
good admission is; look past MetalLB for name leaks, so it missed the reader
rule, the approval allowlist and the fdf paths; check the exit-evidence
claims of delivered items (F13); notice the orphan goldens, the remedy drift,
the duplicated `timeout` default or the repeated literals; or state the
operator guide's silence as a finding. Its line citations were checked for
the files that matter and all resolve at the current baseline.

Claude did not: run `make check`, `make race` or the lint and vulnerability
gates (no product code changed, so none was required by the verification
tiers, but Codex's run is the only full-gate evidence either report holds);
reproduce the `nmstate: null` row of the defaults table (taken from the
Environment rule that explicit null is invalid); or test what the readiness
wait prints live, because no M4 operator gate has passed. Claude's
`render --input-dir` probe refused with `runtime.privilege` in its sandbox and
substituted the compiler's encoder; whether a local render should need sudo is
unexamined and outside this audit.

Neither report verified a vendor fact; no claim here depends on a channel,
CSV or catalog, and the M4 operator gates (B755, B757, B761, B766, B732)
remain open exactly as the milestone records them.

## Merged design

Separate availability, selection and activation; both reports arrive at this
split and the merge keeps it.

| Meaning | Today | Decision |
| --- | --- | --- |
| Available in every build | the registry, listed offline | keep; the executable is the only catalog |
| Installed unless opted out | nothing without a binding and an explicit channel and CSV | keep; D163 forbids a vendor release in the product, so no product default can exist |
| Selected by default across an estate | Environment kind defaults (F09), input templates, duplicated bindings | document the two patterns; defaults for shared values, templates for selection; presets (F11) only with a repeated consumer |
| Optional, added as needed | before the first apply: add the member; after: `destroy` then `apply` | the additive scenario (F01) is the only route to the day-2 meaning |
| Add-ons without a rebuild | none | B90's first slice as F12 states; transport and catalogs later, if ever |

The listing becomes self-sufficient (F08): a description and a binding
skeleton derived from the schema, the PackageManifest lookup for the printed
OLM package, a legend for `requires`, and remedies that name where a value
comes from. Preflight (F10) joins on the first operator request. Online
removal and upgrade stay parked (B769, B106).

## Delivery sequence

Proposed work packages for the owner's authorization; nothing here creates or
activates an item.

| Order | Outcome | Findings | Carrier |
| --- | --- | --- | --- |
| 1 | Operator entry journey: guide section, help wording, listing with description and binding skeleton, defaults semantics documented, example placeholders, remedy drift | F02, F08, F09 (docs), F01 (statement), F16, F17 | one M4 documentation and CLI item; Add-ons with CLI |
| 2 | Admission agrees with planning; render errors are refusals; schema-maximal decode test; parity claim made true or removed | F03, F13 | one M4 defect item; Add-ons with API |
| 3 | Placement allowlist in the frozen request | F04 | one M4 safety item; Add-ons with Secrets; request version moves; trust review |
| 4 | Registry without name leaks; harness over production composition; contributor recipe; one source per limit and default; orphan goldens removed | F05, F06, F07, F14, F18, F19 | one M4 enabling item; Add-ons |
| 5 | Additive apply scenario | F01 | a new lifecycle item under D153's recipe; Reconciliation with Add-ons and Secrets; owner decision first |
| 6 | Named presets | F11 | amend Q26 only with a repeated consumer |
| 7 | Add-on preflight | F10 | B770 as parked |
| 8 | Authored OLM packages, first slice | F12 | B90, defined as F12 states |

Items 1 to 4 need no owner decision beyond authorization and are independent
except that 3 precedes 4's registry work. Item 5 is the only answer to "as
needed" after bootstrap; item 8 is the only answer to "without a rebuild";
they solve different problems.

## Verification

| Check | Codex (2f35ee8c3, rebased 69314aa20) | Claude (4294e7520) |
| --- | --- | --- |
| `make build` | passed | passed |
| `add-ons list`, `add-ons --help`, `preflight add-ons` | passed; six packages; preflight `cli.not-implemented` exit 1 | passed; six packages; one command |
| Fractional `timeout` | reproduced: compile accepted, no unsupported-shape report, plan refused | reproduced: `validate` exit 0, `Plan` returns `*errors.errorString`; `1.5m` plans |
| Placement rename | reproduced: `fixture` refused at step 4 | reproduced: same message from `Canonical()` and `Objects()` |
| Unknown package member | not run | `api.field` listing permitted packages; `rename it to metallb` |
| Environment defaults rows | derived from code | four rows observed (P3a to P3d) |
| `./scripts/go test` over addons, lifecycle, cli | passed | not run (no product change) |
| `make docs-check` | passed | run on the commit carrying this report |
| `make check` | first run failed on an intermittent contextfs test (B673, B799); rerun passed | not run |
| Tree after probes | probes removed | `git status --porcelain` empty |

No command with effects ran in either audit; no cluster or host was reached.
