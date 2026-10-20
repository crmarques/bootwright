# OpenShift GitOps and the Argo CD handoff

What installing OpenShift GitOps creates, when its default Argo CD instance is
ready, what that instance may do, and what Argo CD does to objects it is pointed
at. The package is [B92](../../specs/milestones/m4.md#b92), the bootstrap object
[B765](../../specs/milestones/m4.md#b765), and the live proof
[B766](../../specs/milestones/m4.md#b766). The required shape of the object is
[the GitOps bootstrap object](../../specs/add-ons.md#the-gitops-bootstrap-object).
The ACM side is in [acm-hub-install.md](acm-hub-install.md), the OLM mechanics in
[olm-v0-exact-install.md](olm-v0-exact-install.md).

Provenance: `R4:C<n>` is a claim id of the M4 definition's research of
2026-10-20 (ACM and GitOps; not the M3 research that uses the same labels on
the MetalLB page) and `RV-R4` its adversarial review, with the verdict
(confirmed, corrected or uncertain). Release labels name where a fact was read;
they are not supported or required versions (D163). The ids name research notes
this repository does not track; each statement here carries its fact, and an id
only records where it was read. No v1 fact is recorded: v1 had no GitOps
implementation.

## Install

- The Red Hat package is `openshift-gitops-operator` in `redhat-operators`. The
  documented install uses the namespace `openshift-gitops-operator` (recommended
  "as it provides monitoring support"; before GitOps 1.10 the default was
  `openshift-operators`), an OperatorGroup of the same name with `upgradeStrategy:
  Default` and no `targetNamespaces`, and a Subscription of that name with
  `channel: latest` and `installPlanApproval: Automatic`. The channels are
  `latest` (the default) and `gitops-<x.y>` for a version stream (R4:C25,
  confirmed).
- The release notes say the `stable` channel was removed in OpenShift 4.13, yet
  the ACM 2.17 documents still show a Subscription with `channel: stable` in
  `openshift-operators`. The ACM sample is stale; the product install pages and
  release notes win (R4:C25, confirmed).
- The upstream repository bundle is not the product bundle. It names the package
  `gitops-operator`, channels `latest` and `gitops-1.8`, and AllNamespaces only,
  identically on branches v1.18 to v1.22. Package and channel facts come from the
  Red Hat documents, not from it (R4:C26, confirmed).
- The documents also say to remove the community Argo CD operator first, need the
  Marketplace capability or a manually configured Red Hat catalog, and label the
  namespace `openshift.io/cluster-monitoring=true` for cluster monitoring
  (R4:C25). The documented OpenShift versions differ per GitOps release and the
  1.22 table has no OpenShift column, so the install-time truth is the CSV
  (R4:C27, medium).
- Release-note items confirmed for 1.22 (RV-R4 C28): a NetworkPolicy is on by
  default for the Image Updater workload, `*-redis-ha-server-N` pods may
  crash-loop after an upgrade (delete one at a time) and `runOnInfra: true` does
  not reschedule existing pods. That the operator skips source namespaces that
  have a deletion timestamp is also in the notes (R4:C28, not re-read by RV).
  Two further items could not be sourced and are not evidence: a
  tracking-method switch on upgrade and Keycloak removal (RV-R4 correction 4;
  R4:C28 uncertain).

## The default instance

- At startup the operator creates the cluster-scoped `GitopsService` `cluster`
  (`pipelines.openshift.io/v1alpha1`) if it is absent and continues if it
  exists. Reconciling it creates the namespace `openshift-gitops` and then
  `ArgoCD/openshift-gitops` in it, owned by the GitopsService. An existing
  `ArgoCD` only has nil resource requests and limits filled and its node
  placement synced; other edits persist (R4:C29, confirmed).
- The built default sets resource requests, Dex SSO through OpenShift OAuth,
  resource exclusions and RBAC `policy.csv` of `g, system:cluster-admins,
  role:admin` and `g, cluster-admins, role:admin`, scopes `[groups]` and an empty
  `policy.default`. The tracking method default is `annotation`. The `admin`
  user's password is in the Secret `<instance>-cluster` key `admin.password`
  (R4:C30, confirmed; the Secret name is medium).
- Setting `DISABLE_DEFAULT_ARGOCD_INSTANCE` to `"true"` in the Subscription's
  `spec.config.env` makes the reconciler delete `ArgoCD/openshift-gitops` if it
  exists, leaving the namespace. The usage guide warns the existing install "will
  be deleted". `DISABLE_DEFAULT_ARGOCD_CONSOLELINK` removes the console link;
  `ARGOCD_CLUSTER_CONFIG_NAMESPACES`, `CONTROLLER_CLUSTER_ROLE` and
  `SERVER_CLUSTER_ROLE` are also documented (R4:C32, confirmed). Whether the
  namespace and the GitopsService exist at all when the variable is set at
  creation, and what deleting the GitopsService does, are open.
- Route and workload names derive from helper suffixes, not from a live cluster:
  the web Route `openshift-gitops-server`, the StatefulSet
  `openshift-gitops-application-controller`, and Deployments for the server, repo
  server, Redis and ApplicationSet controller (R4:C31, uncertain). Readiness
  reads `ArgoCD.status`, not workload names.

## Readiness

- `ArgoCD.status.phase` is `Pending`, `Available`, `Failed` or `Unknown`.
  `Available` needs the application controller StatefulSet, Redis, the repo
  server, the server and SSO each running (or disabled), with remote Redis
  counting as available. The ApplicationSet controller is not part of the
  computation. Per-component fields are `applicationController`,
  `applicationSetController`, `repo`, `server`, `redis`, `sso` and `host`
  (R4:C33; RV-R4 confirmed the components and added the Route gate below).
- When a Route or Ingress is enabled, which is the default on the OpenShift
  instance, the phase is also forced to `Pending` until the Route is admitted
  with a host, and to `Failed` on error. A Route stuck unadmitted, for example on
  a broken ingress controller, reads as an Argo CD readiness timeout, so a
  failure summary reports `status.host` and the Route condition apart from the
  component statuses (RV-R4 correction 2; S10 and C33 corrected).

## What the default instance may do

- It is not cluster-admin. It is admin in its own namespace and has cluster-wide
  `get` and `list` and `watch` on everything. The cluster-scoped instance
  (`ARGOCD_CLUSTER_CONFIG_NAMESPACES`, default `openshift-gitops` in the
  upstream CSV) adds `*` on `operators.coreos.com`, `operator.openshift.io`,
  `user.openshift.io`, `config.openshift.io`, `console.openshift.io`,
  `rbac.authorization.k8s.io`, `storage.k8s.io` and `machine.openshift.io`, and
  `*` on `namespaces`, `persistentvolumeclaims`, `persistentvolumes` and
  `configmaps`. The list has no `apiextensions.k8s.io` and no group such as
  `metallb.io`, `nmstate.io`, `ocs.openshift.io` or
  `operator.open-cluster-management.io` (R4:C34, confirmed as upstream
  documentation; the product CSV still needs the live check).
- To manage another namespace: label it `argocd.argoproj.io/managed-by:
  <argocd namespace>` (a namespace-admin Role and RoleBinding result), set
  `CONTROLLER_CLUSTER_ROLE`, or bind your own ClusterRole to the ServiceAccount
  `openshift-gitops-argocd-application-controller`. An Application that creates a
  namespace, a CRD or an operator's custom resource outside the list fails with
  Forbidden until one of these is in place (R4:C34).

## What Argo CD does with the objects it is pointed at

- An `Application` (`argoproj.io/v1alpha1`) belongs in the Argo CD instance's
  namespace by default; for the default instance that is `openshift-gitops`. In
  OpenShift GitOps, Applications in non-control-plane namespaces have been
  generally available since 1.13 and ApplicationSets in any namespace since 1.22,
  both needing the instance's `sourceNamespaces` and the AppProject's
  `spec.sourceNamespaces`. The default is not a product rule, and the trust
  boundary covers `sourceNamespaces` too: a user-controlled namespace is never
  added to a privileged project (RV-R4 correction 3; S11 and C36 corrected). The
  fields are `spec.project` (default `default`), `spec.source.repoURL`,
  `targetRevision` and `path`, `spec.destination.server`
  (`https://kubernetes.default.svc` for in-cluster) and `namespace`, and
  `spec.syncPolicy`. Without the finalizer `resources-finalizer.argocd.argoproj.io`
  deleting an Application does not delete its resources (R4:C36).
- A repository credential is a Secret in the Argo CD namespace with the label
  `argocd.argoproj.io/secret-type: repository` and keys `type`, `url`, then
  `username` and `password`, `sshPrivateKey`, GitHub App keys or
  `tlsClientCertData` and `tlsClientCertKey`, and an optional `project` that
  scopes it. A credential template uses `argocd.argoproj.io/secret-type:
  repo-creds` with a URL prefix; the longest prefix wins and a template applies
  only to a repository with no credential of its own. A custom CA for HTTPS goes
  into the ConfigMap `argocd-tls-certs-cm` keyed by host, SSH host keys into
  `argocd-ssh-known-hosts-cm`, and ConfigMaps carry the label
  `app.kubernetes.io/part-of: argocd`. Sealed secrets strip the labels unless
  re-added (R4:C37, confirmed).
- `syncPolicy.automated` takes `enabled`, `prune`, `selfHeal` and `allowEmpty`.
  Automated sync does not prune, does not self-heal and does not allow empty by
  default, and syncs once per commit SHA with its parameters. The documented sync
  options include `CreateNamespace=true`, `ServerSideApply=true`, `Prune=false`,
  `Prune=confirm`, `Delete=false`, `Delete=confirm`, `PruneLast=true`,
  `Replace=true`, `Validate=false`, `ApplyOutOfSyncOnly=true`,
  `SkipDryRunOnMissingResource=true`, `FailOnSharedResource=true` and
  `RespectIgnoreDifferences=true` (R4:C38, confirmed).
- A custom resource whose CRD is not in the same sync fails the dry run with `the
  server could not find the requested resource` unless
  `SkipDryRunOnMissingResource=true` is set. Argo CD ships health checks for a
  Subscription (healthy needs `AtLatestKnown` and no failure condition, and
  `UpgradePending` under `Manual` with an installed CSV counts as healthy) and for
  CRDs, and none was found for a ClusterServiceVersion or an InstallPlan, so
  sync-wave order over a Subscription does not wait for the CSV or the operator's
  CRDs (R4:C39, confirmed; the absent CSV check is medium).
- Argo CD's bootstrapping guide recommends ApplicationSets for most cases and
  offers app-of-apps as an alternative. "App of Apps is an admin-only tool":
  whoever can push to the parent repository can create Applications in any
  project, and a project that can deploy to the Argo CD namespace has admin
  privileges (R4:C40, confirmed).
- The default tracking method is `annotation`, with
  `argocd.argoproj.io/tracking-id: <app>:<group>/<Kind>:<namespace>/<name>`; the
  alternatives are `annotation+label` and `label`. A tracking annotation that
  does not reference the object it sits on is ignored. A namespace created by
  `CreateNamespace=true` is normally not tracked. By default Argo CD applies
  every manifest in the Git path "regardless if the resources defined in the
  yamls are already applied by another Application"; `FailOnSharedResource=true`
  fails instead. An object created earlier by another tool and later listed in
  Git is applied and tracked, so it is adopted; an object never listed is
  neither pruned nor reported (R4:C41, confirmed; the adoption reading is
  medium).
- To stop Argo CD fighting another tool: `Prune=false` and `Delete=false` per
  object, `IgnoreExtraneous` to drop an object from the sync status only,
  `ignoreDifferences` with `jsonPointers`, `jqPathExpressions` or
  `managedFieldsManagers`, and `RespectIgnoreDifferences=true`, which is "only
  effective when the resource is already created in the cluster" (R4:C42,
  confirmed).

## Removal

- Documented in two steps: delete the Argo CD instances (`oc delete gitopsservice
  cluster -n openshift-gitops`; an instance cannot be deleted from the web
  console) and then uninstall the operator. Uninstalling only the operator leaves
  the instances. The page says nothing about namespaces, CRDs or finalizers
  (R4:C35, confirmed).
- An Application that still holds the cascade finalizer cannot finish deleting
  after the controller is gone; that is an inference from how finalizers work
  (R4:C35, medium).

## The objects a handoff itself needs

Derived from the facts above, and not a documented procedure (R4:C43,
uncertain): the repository Secret, an optional `repo-creds` Secret and
`argocd-tls-certs-cm` entries, an AppProject if a non-default project is wanted,
the root `Application` or `ApplicationSet` in the Argo CD namespace, and the
permission objects (a `managed-by` namespace label or a ClusterRoleBinding).
Everything listed in Git after that is created and owned by Argo CD, which may
adopt an identical existing object.

## Open on this tree

[B766](../../specs/milestones/m4.md#b766) re-verifies the product CSV's install
modes and operator environment defaults, the application controller
ServiceAccount name and its built-in permissions, and the default instance's
`status.server` and `status.phase` paths. Open beyond it: whether the namespace
`openshift-gitops` and the GitopsService exist when the disable variable is set
at creation, whether deleting the GitopsService garbage-collects the default
instance, and whether an Application with the cascade finalizer blocks removal of
`openshift-gitops` after the controller is gone.
