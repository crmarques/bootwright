# Advanced Cluster Management hub install

What installing the ACM hub involves: the operator, the `MultiClusterHub`, the
multicluster engine it brings, readiness, and removal. The package is
[B91](../../specs/milestones/m4.md#b91), the dependent approval it needs is
[B750](../../specs/milestones/m4.md#b750) and
[B751](../../specs/milestones/m4.md#b751), and the live proof is
[B766](../../specs/milestones/m4.md#b766). The OLM mechanics are in
[olm-v0-exact-install.md](olm-v0-exact-install.md).

Provenance: `R4:C<n>` is a claim id of the M4 definition's research of
2026-10-20 (ACM and GitOps; not the M3 research that uses the same labels on
the MetalLB page), `RV-R4` and `RV-R1` its adversarial reviews, with
the verdict (confirmed, corrected or uncertain). Release labels name where a
fact was read, here the 2.15 to 2.17 release branches and the `main` branch;
they are not supported or required versions (D163). The ids name research notes
this repository does not track; each statement here carries its fact, and an id
only records where it was read. No v1 fact is recorded: v1 had no ACM
implementation.

## The operator and its install objects

- The package is `advanced-cluster-management` in `redhat-operators`, with one
  bundle channel per minor (`release-2.x`). The documented objects are a
  namespace, an OperatorGroup with `spec.targetNamespaces` naming it, and a
  Subscription `acm-operator-subscription` with `installPlanApproval:
  Automatic` (R4:C1, confirmed). The channel is the operator's choice.
- The 2.15 and 2.16 CSVs support OwnNamespace only; 2.17 adds SingleNamespace.
  All suggest the namespace `open-cluster-management`, and the documents say to
  pick another name if that namespace exists. A namespace holds one OperatorGroup
  (R4:C7, confirmed).
- A `release-5.0` branch of the operator repository declares the package
  `multiclusterhub-operator`, channel `stable`, OwnNamespace and AllNamespaces,
  and `olm.maxOpenShiftVersion` 5.1. The documentation's version selector lists
  no 5.x and no support matrix was found, so it is not documented as released
  (R4:C3, confirmed for the bundle; the unreleased reading is medium). A package
  name is therefore a field, not a constant.
- Branch CSV names and `createdAt` dates are source-repository facts, not proof
  of what the catalog serves, and `createdAt` is not an ordering signal; the
  installed CSV is read from the Subscription (R4:C2, confirmed).
- Each bundle carries `olm.maxOpenShiftVersion`: 4.21 on the 2.15 branch, 4.22
  on 2.16 and 2.17, 5.1 on 5.0. An installed hub can therefore block a later
  cluster minor update (R4:C5, confirmed for the values;
  [olm-v0-exact-install.md](olm-v0-exact-install.md)).
- The hub operator has its own OpenShift floor, `MinimumOCPVersion`, a constant
  that differs per release. The MultiClusterHub annotation
  `installer.open-cluster-management.io/ignore-ocp-version` (and the deprecated
  spelling `ignoreOCPVersion`) and the environment variable
  `DISABLE_OCP_MIN_VERSION` bypass it, and a failed check surfaces as the
  `RequirementsNotMet` reason. Setting the annotation hides an unmet floor.
  release-2.17 declares `RequiredMCEVersion` 2.17.0 (R4:C6, confirmed); where
  it is checked and what a hub does with another engine minor are unobserved
  (the call site of `RequirementsNotMet` was not traced; B766 records it).
- The documented order is namespace, OperatorGroup, Subscription, wait for the
  CRD, create the `MultiClusterHub`, then watch `oc get mch -o yaml` until the
  phase is `Running`. An early create fails with `no matches for kind
  "MultiClusterHub" in version "operator.open-cluster-management.io/v1"`. The
  documents say `Running` can take up to 10 minutes (R4:C8, confirmed).

## The MultiClusterHub

- It is `operator.open-cluster-management.io/v1` `MultiClusterHub`, documented
  as `multiclusterhub` in the ACM namespace with `spec: {}`. The validating
  webhook has `failurePolicy=fail` for create, update and delete. It rejects a
  second hub (`MultiClusterHub in Standalone mode already exists`), an invalid
  `availabilityConfig`, an unknown component (`invalid component config: <name>
  is not a known component`) and a `localClusterName` of 35 or more characters.
  A create before the operator serves its webhook can fail transiently, which is
  an inference (R4:C9, confirmed; the error strings for unknown components and
  `availabilityConfig` were not re-read).
- Spec fields in 2.16 and 2.17: `imagePullSecret`, `availabilityConfig`
  (`Basic` or `High`, default `High`, with replica count 1 or 2 for most
  components), `nodeSelector`, `tolerations` (a custom list replaces the default
  infra toleration), `overrides` (`imagePullPolicy` and `components[]` with
  `name`, `enabled` and `configOverrides`), `disableHubSelfManagement`,
  `disableUpdateClusterImageSets` and `localClusterName` (default
  `local-cluster`, changeable only with self-management disabled). `networkPolicies`
  exists only on `main` and 5.0. Single-node OpenShift requires
  `availabilityConfig: Basic`, and High availability is unsupported there
  (R4:C10 and C14, confirmed).
- The documented components, with defaults: `app-lifecycle`, `cluster-lifecycle`,
  `cluster-permission`, `console`, `grc`, `insights`, `multicluster-observability`,
  `search`, `submariner-addon` and `volsync` on; `cluster-backup`,
  `cnv-mtv-integrations`, `fine-grained-rbac` and `siteconfig` off. The table
  describes `console` as the ACM plug-in while the note beside it says disabling
  it "disables the OpenShift console", so its effect needs a live check.
  `cnv-mtv-integrations` needs `local-cluster` (R4:C11, confirmed).
- The documented annotations under `installer.open-cluster-management.io/` are
  `pause`, `image-repository`, `image-overrides-configmap`,
  `template-override-configmap`, `ignore-ocp-version`, `kubeconfig`,
  `mce-subscription-spec`, `oadp-subscription-spec`, `release-version`,
  `default-storage-class`, `is-editable`, `resource-adoption-policy`
  (`Strict` default or `Adopt`) and probe settings. Deprecated spellings trigger
  webhook warnings (R4:C16, confirmed). `pause: "true"` gives the phase `Paused`.
- The hub reads the default StorageClass (annotation
  `storageclass.kubernetes.io/is-default-class: "true"`) or an override
  annotation, and logs a warning when several classes exist and none is default.
  Which operands need a PVC was not established (R4:C15, medium).
- The sizing page says its figures are "not minimum requirements"; a compact
  bare-metal hub with three schedulable control-plane nodes is documented as
  supported (R4:C14, confirmed).
- "A ServiceAccount with a ClusterRoleBinding automatically gives cluster
  administrator privileges to ACM and to any user credentials with access to the
  namespace where you install ACM". The documents also say not to give a
  non-administrator access to the local-cluster namespace (R4:C18, confirmed).

## The multicluster engine

- After the hub is created its operator installs the engine itself and creates
  the `MultiClusterEngine` `multiclusterengine`. The source fixes the package
  `multicluster-engine`, the operand namespace `multicluster-engine`, the
  Subscription name `multicluster-engine`, `installPlanApproval: Automatic` and
  a channel per hub release (2.15 and 2.16 read: the 2.10 and 2.11 engine
  streams). It finds the catalog by listing PackageManifests that carry the
  channel and takes the highest priority, and it fails with `found more than one
  multicluster-engine catalogSource with expected channel` when two tie
  (R4:C12 confirmed; RV-R1 C53, constants confirmed).
- The annotation `installer.open-cluster-management.io/mce-subscription-spec`
  holds a JSON Subscription spec that the code unmarshals whole. It overrides
  `channel`, `name`, `source`, `sourceNamespace`, `startingCSV` and
  `installPlanApproval`, and "annotations always take priority". A pinned or
  `Manual` engine is therefore possible, and "pinning ACM cannot pin the engine"
  is false (RV-R1 correction 3 and S10, corrected). The ACM guide shows only
  `source`, calling the annotation required for disconnected installs.
- A custom CatalogSource without `priority` gets 0 and outranks `redhat-operators`
  at -100, so the engine is taken from it when it carries the channel. Two custom
  catalogs tied at the highest priority that both offer the channel make ACM
  error (RV-R1 correction 7).
- The engine CSV read also declares `olm.maxOpenShiftVersion` (value 4.22), so
  an ACM install brings two blocking CSVs (RV-R1 correction 6;
  [olm-v0-exact-install.md](olm-v0-exact-install.md)).
- The disconnected flow mirrors both packages, creates a CatalogSource in
  `openshift-marketplace`, verifies both appear as PackageManifests, redirects
  images and sets the annotation. Without it "the install or upgrade can stall at
  the multicluster engine subscription". The console form has no field for the
  annotation. The documents name `ImageContentSourcePolicy`, whose equivalent on
  a current cluster is an ImageDigestMirrorSet, and their `oc-mirror` sample uses
  stale placeholders (R4:C17, confirmed).
- The hub's finalizer deletes the engine objects it created and returns an error
  until the engine has terminated. A pre-existing engine or Subscription it did
  not create is left alone (R4:C12 and C21, confirmed).

## Readiness

- On 2.15 to 2.17 the hub phase is one of `Pending`, `Paused`, `Running`,
  `Installing`, `Updating`, `Uninstalling`, `UpdatingBlocked` or `Error`.
  `Waiting`, for a hub waiting on the engine's version, exists only on `main`,
  the 5.x line. A classifier does not expect `Waiting` on a 2.x hub and treats an
  unknown phase as not ready, not as an error. `Running` is the success signal
  (RV-R4 correction 1; R4:C13 corrected).
- The condition types are `Progressing`, `Complete`, `Terminating`, `Blocked` and
  `ComponentFailure`. `Complete=True` with reason `ComponentsAvailable` and
  message "All hub components ready." holds when every component is available and
  no old resource waits to be pruned; otherwise `Complete=False`, reason
  `ComponentsUnavailable`. Status also has `currentVersion`, `desiredVersion`,
  a `components` map with a `multicluster-engine` entry and
  `mceVersionCompliance` (R4:C13, confirmed).

## Removal

- Before deleting the hub: detach managed clusters other than `local-cluster`,
  delete `DiscoveryConfig`, `AgentServiceConfig` and the
  `MultiClusterObservability`, then `oc delete multiclusterhub --all`. The
  documents say it "can take up to 20 minutes". The two levels are removing the
  custom resource, which keeps the operator and CRDs, and uninstalling the
  operator, which excludes CRDs (R4:C19, confirmed).
- The webhook denies deletion with `cannot delete MultiClusterHub resource
  because <Name> resource(s) exist` while any `MultiClusterObservability`,
  `DiscoveryConfig`, `AgentServiceConfig` or `ManagedCluster` other than the
  local one exists. The local exception needs the name equal to
  `spec.localClusterName` and the label `local-cluster=true`. This is read on
  release-2.17 only (R4:C20, confirmed).
- The finalizer removes labelled cluster roles and bindings, `apps` subscriptions
  and the engine, and records reasons `ManagedClusterTerminating`,
  `ManagedClusterNamespaceTerminating`, `HelmReleaseTerminating` and
  `DeletionTimestampPresent` (R4:C21, confirmed).
- Leftovers affect a reinstall. The documents publish a cleanup script that
  deletes the `v1.admission.cluster.open-cluster-management.io` and
  `v1.admission.work.open-cluster-management.io` APIServices, all
  `ClusterImageSet`s, named cluster roles, six named CRDs, the webhook
  configurations and the namespace, and say to install once it reports no
  resources found. Known issue ACM-14343: after an uninstall, installing an
  earlier version and updating can fail when custom resources remain. A reinstall
  of the same version needs no operator uninstall (R4:C22, confirmed).
- By default the hub imports itself as the ManagedCluster `local-cluster`.
  Setting `disableHubSelfManagement` to true and managing the hub by hand "leads
  to unexpected behavior". The operator recreates a deleted `local-cluster`
  while self-management is on, and the webhook refuses a `localClusterName`
  change while a ManagedCluster labelled `local-cluster=true` exists (R4:C23,
  confirmed). Import of other clusters is outside this page (R4:C24, uncertain:
  not re-verified).

## Open on this tree

[B766](../../specs/milestones/m4.md#b766) re-verifies and records: the engine
Subscription honoring the annotation, the engine plan's owner reference to that
Subscription, the hub spec after `Running`, whether the first hub create met the
webhook window and how `oc create` exposed the refusal, and the hub's behavior
with no default StorageClass. The research questions not answered yet: which
channels and CSVs `redhat-operators` serves (`oc get packagemanifest
advanced-cluster-management -n openshift-marketplace`), the real time to
`Running`, which operands need a PVC, whether an older ACM channel on a newer
cluster blocks `Upgradeable` on the `olm` operator, and which objects persist
after a hub delete against the cleanup script. The 2.17 support matrix sits
behind a Red Hat login and was not read (R4:C4).
