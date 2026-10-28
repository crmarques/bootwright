# OLM v0 install pinning and exact approval

What a Subscription pin fixes, how InstallPlan approval works, what proves the
intended operator is up, and which native behaviors move an install after it.
[OLM installation](../../specs/add-ons.md#olm-installation) and
[readiness](../../specs/add-ons.md#readiness) own the required behavior; the
program is [B750](../../specs/milestones/delivered.md#b750), the adapter
[B751](../../specs/milestones/delivered.md#b751), and the packages that apply it are
[B83](../../specs/milestones/m4.md#b83) and
[B735](../../specs/milestones/m4.md#b735). The `oc` mechanics are in
[kubernetes-object-ownership-with-oc.md](kubernetes-object-ownership-with-oc.md).

Provenance: `R<n>:C<m>` is a claim id of the M4 definition's research (R1 OLM
and catalogs, R2 Kubernetes API mechanics, R3 Data Foundation, R4 ACM and
GitOps, R5 GitLab) and `S<n>` its summary items; `RV` marks a claim the
adversarial review re-checked, with its verdict (confirmed, corrected or
uncertain). Release labels name where a fact was read; they are not supported
or required versions (D163). The ids name research notes of 2026-10-20 that
this repository does not track; each statement here carries its fact, and an
id only records where it was read. "Observed by the v1 implementation
(main-v1); re-verify on this tree" marks a v1 fact the research did not
confirm; the v1 lesson ids `L1-nn` and `D-nn` name untracked review notes of
that branch.

## What a pin fixes

- A Subscription `startingCSV` with `installPlanApproval: Manual` pins only the
  first install. The resolver applies `startingCSV` only while the
  Subscription has no installed CSV (`status.installedCSV` empty). When the
  named CSV is not in the channel, the Subscription reports `ResolutionFailed`
  with `no operators found with name <csv> in channel <channel> of package
  <package> in the catalog referenced by subscription <name>`. The comment and
  message are the same on the 4.20 and 4.22 branches (4.21 not read; R1:C10, confirmed; RV:
  the predicate runs only when no CSV is installed).
- After the first install `startingCSV` is ignored. The candidates are the CSVs
  that replace or skip-range from the installed one, so a newer channel head
  creates another InstallPlan that waits in `RequiresApproval`, and the
  Subscription shows `state: UpgradePending` for as long as the catalog is
  ahead (R1:C19, confirmed). The documentation's own example pairs
  `startingCSV` with `Manual` "in case your specified version is superseded".
- Dependencies in the same plan are chosen by the resolver, not pinned by
  `startingCSV` (R1:C12 and C19, confirmed). A pin therefore needs the
  `clusterServiceVersionNames` check below.
- The pinned CSV must stay in the named channel of the catalog. Whether the
  Red Hat catalogs keep old bundles is not verified (R1:C19, low confidence).

## Approval is namespace-wide and whole-plan

- If any Subscription in a namespace is `Manual`, every InstallPlan generated
  there is `Manual`. The OLM code says so in a comment ("an odd artifact of the
  older resolver") on the 4.20 and 4.22 branches (4.21 not read; R1:C9,
  confirmed). Two operators in
  one namespace cannot hold independent approval policies.
- A plan created `Manual` starts in `RequiresApproval`; setting
  `spec.approved: true` moves it to `Installing` (R1:C7, confirmed). The
  OpenShift documents show approval only in the console. The command route is
  documented by GitLab's operator guide and implied by the types, and no
  OpenShift page documents it (medium confidence).
- Approval is one boolean for the whole plan. `spec.clusterServiceVersionNames`
  lists every CSV the plan installs, including dependency CSVs the solver
  added, so a controller reads the list before approving (R1:C8, confirmed).

## Reading the install

- Subscription status gives `currentCSV`, `installedCSV`, `state`
  (`UpgradeAvailable`, `UpgradePending`, `AtLatestKnown`, `UpgradeFailed`),
  `installPlanRef` and conditions: `CatalogSourcesUnhealthy`,
  `InstallPlanMissing`, `InstallPlanPending`, `InstallPlanFailed` and
  `ResolutionFailed`, plus `BundleUnpacking` and `BundleUnpackFailed` set in
  code (R1:C6, confirmed).
- `installedCSV` equals `currentCSV` only when no upgrade is pending. With
  Manual approval or a newer channel head, `currentCSV` moves ahead with state
  `UpgradePending` and `installPlanRef` points at the pending plan while the
  installed operator is healthy. Readiness and a pending upgrade are separate
  facts (R2:C22 confirmed; RV-R2 correction 5).
- CSV phases are Pending, InstallReady, Installing, Succeeded, Failed, Unknown,
  Replacing and Deleting. The CSV type has no `observedGeneration` (R1:C11 and
  R2:C21, confirmed).
- An `AllNamespaces` operator gets a copied CSV in every namespace unless
  copied CSVs are disabled, so a proof names the CSV in the operator's own
  namespace and never a cluster-wide list (R1:C4, confirmed).
- Failures classify by where they show. `ResolutionFailed` is a wrong channel,
  `startingCSV` or catalog. `BundleUnpackFailed` is an image pull. The unpack
  Job and its ConfigMap live in the CatalogSource's namespace and take pull
  secrets from the CatalogSource `spec.secrets`; an OperatorGroup annotation
  `operatorframework.io/bundle-unpack-timeout` overrides the timeout, whose
  default was not read (R1:C15, confirmed).
- `CatalogSource.status.connectionState.lastObservedState` is `READY` when the
  catalog serves; `latestImageRegistryPoll` shows the last poll (R1:C13,
  confirmed).
- One OperatorGroup per namespace. With more than one the InstallPlan does not
  generate the CSV and reports "attenuated service account query failed - more
  than one operator group(s) are managing this namespace" (R1:C1, confirmed;
  the "with none" half is not in the modules read). A CSV whose install modes
  do not support the group's targets fails with `UnsupportedOperatorGroup`
  (R1:C3, confirmed).

## Catalogs and what moves them

- The default sources `redhat-operators`, `certified-operators` and
  `community-operators` serve index images tagged with the cluster minor. The
  Cluster Version Operator moves the tag at a cluster upgrade. A tag pulls with
  policy `Always`; a digest pulls with `IfNotPresent`. The documents do not say
  how often Red Hat rebuilds a tag, so two runs of one declaration on different
  dates can resolve different channel heads (R1:C17 and C18, confirmed).
- The Marketplace Operator reverts manual changes to a default source's
  `grpcPodConfig`. A persistent local change first disables the default
  (`disableAllDefaultSources` on the `OperatorHub` object). A Subscription that
  names a disabled source fails resolution (R1:C14 and C21, confirmed; the
  consequence is inferred from the resolver).
- A custom CatalogSource is not touched by the Cluster Version Operator, so one
  named by digest holds the catalog constant across a cluster upgrade
  (R1:C20, corrected on one detail). Its priority matters: a custom source
  without `priority` gets 0 and outranks `redhat-operators` at -100, so the
  resolver and ACM take a package from it when it carries the channel. Two
  custom catalogs tied at the highest priority that both offer a channel make
  ACM error (RV-R1 correction 7, confirmed against the ACM guide and code).
- The same package name can come from two catalogs. `oc get packagemanifest`
  can return it "from an unexpected catalog" unless filtered with
  `--selector=catalog=<catalogsource>`, and a Subscription names `source`
  (R1:C3, confirmed). GitLab's operator is the shipped example
  ([gitlab-operator-on-openshift.md](gitlab-operator-on-openshift.md); R1:C56,
  confirmed).
- Dependency resolution uses every catalog on the cluster, preferring higher
  priority, then the dependent's own catalog, then the default channel, then the
  newer head, with at most one operator per package per namespace (R1:C12,
  confirmed). A generated dependency Subscription is named
  `<package>-<channel>-<catalog>-<catalogNamespace>`, has `installPlanApproval:
  Automatic` and `startingCSV` set to the resolved CSV, and its plan is still
  `Manual` when any Subscription in the namespace is (RV-R1 correction 5,
  confirmed in `resolver/steps.go` and `step_resolver.go`).

## Dependent Subscriptions

An operator that creates another operator's Subscription gives that
Subscription nothing from the first Subscription's pin. The only lever is what
the first operator accepts, and OLM's namespace-wide `Manual` rule then covers
the dependent's namespace (R1:S2, confirmed). ACM is the example. Its hub
operator creates the `multicluster-engine` Subscription with `Automatic`
approval, and its `mce-subscription-spec` annotation is unmarshalled into a
full Subscription spec that overrides `channel`, `name`, `source`,
`sourceNamespace`, `startingCSV` and `installPlanApproval` (RV-R1 correction 3,
confirmed in the ACM operator code). The detail is in
[acm-hub-install.md](acm-hub-install.md). The dependent approval step is part
of [B750](../../specs/milestones/delivered.md#b750) and
[B751](../../specs/milestones/delivered.md#b751). Because Bootwright approves a plan
on a Subscription it did not create, it checks every spec field the
annotation sets, the Subscription's creation time against its creator's, and
the plan's `status.bundleLookups[].catalogSourceRef`, never only
`installPlanApproval` and `startingCSV`.

## An operator that approves its own plans

odf-operator reads a `pkgs-config` ConfigMap of package, channel and CSV
records. It creates or updates the `odf-dependencies` Subscription itself with
a `startingCSV`, and `ApproveInstallPlanForCsv` sets `spec.approved: true` on
any `Manual` plan in its namespace that is in `RequiresApproval` and whose
`clusterServiceVersionNames` contains a configured CSV (RV-R1 correction 4;
C57 corrected). An inspect-then-approve gate for Data Foundation therefore
races with the operator after the first odf-operator CSV runs, and for later
plans the add-on is not the sole approver. The pin is the `odf-operator`
`startingCSV` and the catalog. Details are in
[fusion-data-foundation-catalog.md](fusion-data-foundation-catalog.md) and
[data-foundation-external-mode.md](data-foundation-external-mode.md).

[B759](../../specs/milestones/delivered.md#b759) installs such an operator by
approving only its first exact plan. Not yet observed on this tree: the first
plan's CSV list and which later plans odf-operator approves.
[B761](../../specs/milestones/m4.md#b761) records both.

## The cluster upgrade gate

- A CSV annotation `olm.properties` with `olm.maxOpenShiftVersion` blocks a
  cluster update past that minor until the installed operator is replaced by a
  bundle without it. The value is major and minor only, one per CSV (RV-R1
  correction 6 and C16, corrected).
- OLM v0 computes the incompatible operators in code and words the refusal
  `maximum supported OCP version for <ns>/<name> is <x.y>`. The 4.22 code
  treats 4.23 and 5.0 as the same next version. The OLM ClusterOperator
  reports `Upgradeable=False` with reason `IncompatibleOperatorsInstalled` on
  classic OLM (RV-R5 C8 corrected confirms the reason string only). The
  ClusterOperator's name is not settled: R1 asked about
  `operator-lifecycle-manager`, and RV-R3 C9's summary of the OLM v1
  compatibility page says `olm`; the gate records the name from `oc get
  clusteroperator`.
- Carriers read on 2026-10-20: the ACM hub and multicluster engine CSVs, the
  GitLab operator CSV and the odf-operator bundles of the IBM catalog (R1:C16,
  R3:C9, R4:C5, R5:C8). An ACM install brings two blocking CSVs. The research
  registers no rule from this; the installed CSV's property is evidence.

## Reading Red Hat's shipped CSVs

- Read the shipped content of a Red Hat operator from `manifests/stable` on its
  `release-4.xx` branch, never the upstream `bundle/` template (RV-R1
  correction 2, confirmed).
- NMState: the shipped CSV supports OwnNamespace, SingleNamespace and
  AllNamespaces, has no webhooks, declares the disconnected feature, suggests
  `openshift-nmstate` and carries channels `stable` and `alpha` with `stable` as
  the default. The documented OperatorGroup with `targetNamespaces:
  [openshift-nmstate]` is valid. R1's install-mode contradiction came from the
  upstream `bundle/` template (RV-R1 C51, refuted; C40 and C58, corrected).
- MetalLB's shipped CSV is in
  [metallb-on-openshift.md](metallb-on-openshift.md) (RV-R1 C50, corrected).

## OpenShift Virtualization

Source: Red Hat OpenShift Container Platform 4.21, Virtualization >
Installing, sections 4.3.3 "Subscribe to the OpenShift Virtualization catalog
by using the CLI" and 4.3.4 "Deploy the OpenShift Virtualization Operator by
using the CLI"
(https://docs.redhat.com/en/documentation/openshift_container_platform/4.21/html/virtualization/installing).
The package is [B883](../../specs/milestones/m4.md#b883); its rendering is
[coverage](../../specs/add-on-coverage.md#kubevirt).

Documented (4.21):

- The namespace `openshift-cnv` carries the label
  `openshift.io/cluster-monitoring: "true"`.
- The OperatorGroup `kubevirt-hyperconverged-group` in `openshift-cnv` targets
  `openshift-cnv`.
- The Subscription `hco-operatorhub` names the package
  `kubevirt-hyperconverged`, channel `stable`, source `redhat-operators`, source
  namespace `openshift-marketplace` and a `startingCSV` of the form
  `kubevirt-hyperconverged-operator.v4.21.<z>`, with Manual approval so that a
  superseded CSV is not installed unasked. Bootwright registers no channel or CSV
  (D163).
- After the operator runs, the `HyperConverged` singleton is created from a
  manifest `hco.kubevirt.io/v1beta1`, named `kubevirt-hyperconverged` in
  `openshift-cnv`, with `spec: {}`. Readiness is the operator's `Available`
  condition on that object.

Unverified, re-verified by the package's operator gate (B883):

- That the first InstallPlan lists only the HCO's own CSV
  (`clusterServiceVersionNames`); the operator is known to bring KubeVirt,
  CDI and other component operators, and whether their CSVs ride in the first
  plan or arrive through the HCO is not read.
- That the shipped CSV supports the OwnNamespace install mode the documented
  OperatorGroup relies on; Red Hat's page shows the group, not the CSV.

Not rendered, by design: the `HyperConverged` `spec` fields. The location of
`vmStateStorageClass` differs between API versions of the object, so the
package renders the documented empty `spec` and nothing else.

## Wiring facts from v1

Observed by the v1 implementation (main-v1); re-verify on this tree (v1 lesson
L1-07):

- A PackageManifest named for the package in the CatalogSource's namespace
  exposes `status.catalogSource`, `status.catalogSourceNamespace` and
  `status.channels[].{name,currentCSV}`. v1 resolved the target CSV from the
  Subscription's channel entry and refused a PackageManifest from another
  CatalogSource.
- A Subscription exposes `status.installPlanRef.{name,uid}` and
  `status.installedCSV`. The InstallPlan has `spec.approval`, `spec.approved`,
  `spec.clusterServiceVersionNames` and an owner reference to the Subscription.
  [B755](../../specs/milestones/m4.md#b755) re-verifies the owner reference.
- v1 required `spec.version` on a Succeeded CSV before it counted the check.
- v1 refused any InstallPlan that listed more than one CSV. That is an
  inference from the code: it never ran against an operator whose plan pulls
  dependency CSVs, and it ran only against odf-operator, whose second-level
  operators arrive through a Subscription the operator creates later (L1-07,
  D-18).
