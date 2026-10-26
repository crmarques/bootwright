# Add-on coverage and qualification

This page owns M4's built-in package coverage: each package's native objects,
the capabilities it provides and requires, its typed configuration, readiness
and removal, the item and qualification behind it, and the acceptance
profiles. [Add-ons](add-ons.md) owns the lifecycle every package shares, the
[add-on API](api/addons.md#built-in-packages) owns each package's fields and
admission rules, and [M4](milestones/m4.md) owns delivery and sequencing. Rows
describe day-0 bootstrap, never a day-2 control plane: an instance is retired
with its cluster, and removing one add-on from a running cluster is parked as
[B769](milestones/backlog.md#b769). A package not yet delivered is not in the
executable, so naming it under `spec.packages` is an unknown field, never a
silent omission.

## Release evidence

The operator declares each package's OLM `channel` and `startingCSV`, and the
catalog image when it declares an owned CatalogSource, by tag or digest as it
chooses (D157). Bootwright registers no default channel, CSV or catalog image,
no version pairing between a package, its operands, its dependencies and
OpenShift, and no support statement (D163). One rendering serves every release
minor; where a minor's native mechanics differ, a registered version
specialization adapts the mechanics with its evidence and a parity test in
the [version-specialization catalog](../.agents/knowledge/version-specializations.md),
and never approves, refuses or recommends a release (D162).

Package facts come from the shipped CSV on the release branch of each
operator (`manifests/stable` where the operator publishes one), never from
upstream `bundle/` templates. A bundle's `olm.maxOpenShiftVersion`, which
holds a cluster's minor update past it until the operator upgrades the
operator, is recorded in the instance's evidence and the acceptance row and is
never enforced. A catalog served by two sources with identical bundles needs
an explicit `source`. Each acceptance run records the build commit, the
release minor, every channel, CSV and catalog image chosen, each phase's
duration and every native fact it re-verified; that record is evidence for its
profile on that release only.

## Built-in packages

| Package | Native objects | Provides / requires | Typed configuration | Readiness | Removal | Item | Submilestone | Qualification |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| nmstate | owned `v1 Namespace` `openshift-nmstate`; `operators.coreos.com/v1 OperatorGroup` `openshift-nmstate` targeting `[openshift-nmstate]`; `operators.coreos.com/v1alpha1 Subscription` `kubernetes-nmstate-operator` (package `kubernetes-nmstate-operator`, Manual, declared `startingCSV`); the one approved InstallPlan; `nmstate.io/v1 NMState` `nmstate` after `nmstates.nmstate.io` is `Established` | provides `nmstate`; requires none | `subscription` (source default `redhat-operators`); `timeout` (default `30m`) | `csvSucceeded`; `NMState` `nmstate` `Available` `True`; `Deployment` `nmstate-webhook` in `openshift-nmstate` `Available` `True` | retired with its cluster | [B735](milestones/m4.md#b735) | M4b | package qualification in-tree; addons-sno ([B755](milestones/m4.md#b755)); networking use on lab-networks ([B729](milestones/m3.md#b729)) |
| metallb | owned `v1 Namespace` `metallb-system`; `operators.coreos.com/v1 OperatorGroup` `metallb-operator` with no `spec` (all namespaces); `operators.coreos.com/v1alpha1 Subscription` `metallb-operator-sub` (package `metallb-operator`, Manual, declared `startingCSV`); the one approved InstallPlan; `metallb.io/v1beta1 MetalLB` `metallb` after `metallbs.metallb.io` is `Established`; per pool, after `MetalLB` is `Available`, `metallb.io/v1beta1 IPAddressPool` `<name>` and `L2Advertisement` `<name>` (`ipAddressPools: [<name>]`, `interfaces` when declared) in `metallb-system` | provides `metallb`; requires none | `subscription` (source default `redhat-operators`); `timeout` (default `30m`); `pools[]` (`name`, `addresses`, `autoAssign` default `true`, optional `l2.interface`) admitted against the cluster's networks, node addresses, endpoints, next hops and held addresses; an interface pool needs the cluster's `routingViaHost: true` and `ipForwarding: Global` | `csvSucceeded`; `MetalLB` `metallb` `Available` `True` | retired with its cluster; on a physical cluster that keeps running, its pools keep answering their addresses, unmanaged | [B84](milestones/m4.md#b84), [B727](milestones/m4.md#b727) | M4c | package qualification in-tree; addons-lb ([B757](milestones/m4.md#b757)) for machine-network pools; lab-networks ([B729](milestones/m3.md#b729)) for an interface pool |
| fdf | owned `v1 Namespace` `openshift-storage` (labelled for cluster monitoring); `operators.coreos.com/v1 OperatorGroup` `openshift-storage` targeting `[openshift-storage]`; optional owned CatalogSource in `openshift-marketplace`; `operators.coreos.com/v1alpha1 Subscription` `odf-operator` (package `odf-operator`, Manual, declared `startingCSV`); the first InstallPlan, approved under the operator-managed policy, because odf-operator approves its own later plans; after `storageclusters.ocs.openshift.io` is `Established`, the owned `Opaque` Secret `rook-ceph-external-cluster-details` (key `external_cluster_details`) placed from the export block's produced details, and `ocs.openshift.io/v1 StorageCluster` `ocs-external-storagecluster` with `spec.externalStorage.enable: true` | provides `dataFoundation`; requires none; requires the referenced StorageExport's block | `subscription` (`source` required: `isf-data-foundation-catalog` through an owned CatalogSource, or `redhat-operators` for Red Hat OpenShift Data Foundation); `storageExportRef` (required, one consumer per export); `defaultStorageClass` (default `false`); `timeout` (default `1h`); the Red Hat catalog serving the same package is expected, unverified | `csvSucceeded`; StorageCluster `status.phase` `Ready` and `ReconcileComplete` `True`; StorageClasses `ocs-external-storagecluster-ceph-rbd`, `ocs-external-storagecluster-cephfs`, `openshift-storage.noobaa.io` and, with a gateway, `ocs-external-storagecluster-ceph-rgw`; `NooBaa` `noobaa` `status.phase` `Ready`; never the `Available` condition | retired with its cluster; the export's principals leave when the export retires under its [retirement condition](storage-resources.md#storageexport), which requires this cluster to be removed by the same destroy on owned virtual Machines; otherwise the export's removal refuses | [B85](milestones/m4.md#b85) (Ceph side [B86](milestones/m4.md#b86), [B762](milestones/delivered.md#b762)) | M4d | package qualification in-tree; addons-df ([B761](milestones/m4.md#b761)) |
| acm | owned `v1 Namespace` `open-cluster-management`; `operators.coreos.com/v1 OperatorGroup` `open-cluster-management` targeting `[open-cluster-management]`; `operators.coreos.com/v1alpha1 Subscription` `acm-operator-subscription` (package `advanced-cluster-management`, Manual, declared `startingCSV`); the one approved InstallPlan; `operator.open-cluster-management.io/v1 MultiClusterHub` `multiclusterhub` after `multiclusterhubs.operator.open-cluster-management.io` is `Established`, carrying the engine subscription annotation; the approved first InstallPlan of the hub-created Subscription `multicluster-engine` in `multicluster-engine`, a dependent | provides none; requires none | `subscription` (source default `redhat-operators`); `mce` (channel, startingCSV, source, sourceNamespace); `hub` (availabilityConfig, disableHubSelfManagement, nodeSelector, tolerations); `timeout` (default `45m`) | `csvSucceeded` for the hub operator; `csvSucceeded` for the engine at its declared CSV; `MultiClusterHub` `multiclusterhub` `status.phase` `Running` | retired with its cluster | [B91](milestones/m4.md#b91) | M4e | package qualification in-tree; addons-hub ([B766](milestones/m4.md#b766)) |
| gitops | owned `v1 Namespace` `openshift-gitops-operator` labelled for cluster monitoring; `operators.coreos.com/v1 OperatorGroup` `openshift-gitops-operator` (all namespaces); `operators.coreos.com/v1alpha1 Subscription` `openshift-gitops-operator` (package `openshift-gitops-operator`, Manual, declared `startingCSV`); the one approved InstallPlan; with `bootstrap`: the owned placed `v1 Secret` `bootwright-bootstrap-repository`, `rbac.authorization.k8s.io/v1 ClusterRoleBinding` `bootwright-gitops-bootstrap` when `access` names a role, and `argoproj.io/v1alpha1 Application` `bootwright-bootstrap`, all in or for `openshift-gitops` | provides none; requires none; a last package | `subscription` (source default `redhat-operators`); `bootstrap` (repoURL, targetRevision, path, destinationNamespace (required), credentials, access.clusterRole, sync); `timeout` (default `20m`) | `csvSucceeded`; `ArgoCD` `openshift-gitops` `status.server` `Running`; then its `status.phase` `Available` | retired with its cluster; the bootstrap object keeps syncing on a physical cluster that outlives its context | [B92](milestones/m4.md#b92), [B765](milestones/m4.md#b765) | M4e | package qualification in-tree; addons-hub ([B766](milestones/m4.md#b766)) |
| certmgr | owned namespace `cert-manager-operator`, its OperatorGroup and Subscription (package and group shape fixed by its definition); the one approved InstallPlan; operands in `cert-manager`, created by the operator | provides `certManager`; requires none | `subscription` (source default to be verified); `timeout` (default `20m`) | `csvSucceeded`; operand Deployments `Available`; `certificates.cert-manager.io` and `issuers.cert-manager.io` `Established` | retired with its cluster | [B767](milestones/m4.md#b767) | M4f | Needs definition; package qualification in-tree; addons-gitlab ([B768](milestones/m4.md#b768)) |
| gitlab | owned `v1 Namespace` `gitlab-system`; `operators.coreos.com/v1 OperatorGroup` `gitlab-system` targeting `[gitlab-system]`; `cert-manager.io/v1 Issuer` `gitlab-selfsigned-issuer` and `Certificate` `gitlab-serving-cert` (Secret `webhook-server-cert`); `operators.coreos.com/v1alpha1 Subscription` `gitlab-operator-kubernetes` (Manual, declared `startingCSV`, explicit `source`); the one approved InstallPlan; up to seven placed Secrets named with the prefix `bootwright-gitlab-`; `apps.gitlab.com/v1beta1 GitLab` `gitlab` | provides none; requires `certManager`, and `dataFoundation` when `gitaly.storageClass` names an `fdf` class | `subscription` (`source` required); `timeout` (default `1h`); `chartVersion`; `rootPasswordRef`; `tlsCertificateRef`; `licenseRef`; `database` (host, port, name, username, passwordRef); `redis` (host, port, passwordRef); `objectStorage` (connectionRef, registryRef, registryBucket); `gitaly` (storageClass, size) | `csvSucceeded`; `GitLab` `gitlab` `Available` `True`; claim `repo-data-gitlab-gitaly-0` `Bound`; pod `gitlab-gitaly-0` `Ready`; Deployment `gitlab-registry` `Available` | retired with its cluster; external database, Redis and buckets untouched | [B93](milestones/m4.md#b93) | M4f | package qualification in-tree; addons-gitlab ([B768](milestones/m4.md#b768)) |

Every row installs through the shared [OLM installation](add-ons.md#olm-installation):
an owned namespace, OperatorGroup and Subscription with manual approval of the
declared `startingCSV`, an optional owned CatalogSource with an explicit
priority, and one absolute readiness deadline. Exact approval is the policy
of every row except `fdf`, whose operator approves the later plans of its own
namespace. Every row's removal is [retirement](add-ons.md#retirement) with its
cluster; on a physical cluster that keeps running, its objects and placed
Secrets stay, unmanaged, as its removal description says. A changed
configuration over a completed apply is destroyed and applied again (D153).

Not yet met: the cert-manager and GitLab packages are absent; tracked as
[B767](milestones/m4.md#b767) and [B93](milestones/m4.md#b93).

### nmstate

The Kubernetes NMState Operator ([B735](milestones/m4.md#b735)). A binding
selects it with:

```yaml
spec:
  clusterRef: lab-sno
  packages:
    nmstate:
      subscription:
        channel: stable
        startingCSV: <the CSV the channel lists>
```

The operator reads a channel's CSV from the PackageManifest
`kubernetes-nmstate-operator` in `openshift-marketplace`
(`status.channels[].currentCSV`); Bootwright chooses neither. The shipped CSV's
install modes and channels are evidence in
[knowledge](../.agents/knowledge/olm-v0-exact-install.md). The cluster network
block of M3d requires its instance; its probe host on a disconnected cluster is
parked as [B775](milestones/backlog.md#b775).

### metallb

The MetalLB Operator ([B84](milestones/m4.md#b84)) with its layer 2 pools
([B727](milestones/m4.md#b727)). A binding selects it with:

```yaml
spec:
  clusterRef: lab-sno
  packages:
    metallb:
      subscription:
        channel: stable
        startingCSV: <the CSV the channel lists>
      pools:
        - name: lab
          addresses:
            - <first>-<last>
```

The operator reads a channel's CSV from the PackageManifest
`metallb-operator`. Its operator accepts one `MetalLB` resource, named
`metallb`, in the namespace `metallb-system`. Forwarding on a secondary
interface needs host routing and the cluster-wide `ipForwarding: Global`, which
this package requires (Q34). No specialization is registered; one of
forwarding would discriminate on the cluster's exact release, and only after a
run shows `ipForwarding: Global` failing. The per-release documentation, the
status and advertisement resources the package renders none of, and the
platforms the vendor lists are evidence in
[knowledge](../.agents/knowledge/metallb-on-openshift.md); addons-lb and
lab-networks are evidence on libvirt guests, never a support statement.

### fdf

IBM Fusion Data Foundation in external mode ([B85](milestones/m4.md#b85)),
attaching the cluster to the Ceph cluster a
[StorageExport](storage-resources.md#storageexport) names. A binding selects
it with:

```yaml
spec:
  clusterRef: lab-compact
  packages:
    fdf:
      storageExportRef: odf-compact
      subscription:
        channel: <a channel the catalog lists>
        startingCSV: <the CSV that channel lists>
        source: isf-data-foundation-catalog
        catalogSource:
          name: isf-data-foundation-catalog
          image: <the IBM catalog tag or digest the operator chooses>
          priority: <an explicit integer>
```

The operator chooses the catalog tag or digest, the channel and the CSV; the
registry holds no pairing between them (D163), and the operator reads the
channel's CSV from the PackageManifest `odf-operator` in `openshift-marketplace`
of the catalog it chose
([knowledge](../.agents/knowledge/fusion-data-foundation-catalog.md)). IBM's catalog serves the package `odf-operator`;
naming `redhat-operators` as `subscription.source` is expected to install Red
Hat OpenShift Data Foundation, but that `redhat-operators` serves the same
package name, namespace and objects is unverified and is re-verified by
[B761](milestones/m4.md#b761) with `oc get packagemanifest odf-operator -n
openshift-marketplace` on a Red Hat catalog. The cluster's install pull secret carries `cp.icr.io` with
user `cp` and the IBM entitlement key, and the Red Hat registries the selected
bundle's images name; Bootwright never merges a registry credential into a
running cluster (Q33). v1 observed NooBaa failing with `x509` against a
private RGW CA until the issuing CA was in the cluster-wide trusted bundle;
the trust source is not established for any release
([knowledge](../.agents/knowledge/data-foundation-external-mode.md#noobaa-and-the-rgw-certificate)).
As a
conservative Bootwright choice, not a native fact, `fdf` therefore admits a
TLS object gateway only when the cluster selects an additional trust bundle
under policy `Always` ([B195](milestones/delivered.md#b195)); a gateway certificate
that already chains to a CA in cluster trust is refused too, and the operator
adds its issuing CA, which changes nothing else. Installing Data Foundation through OLM alone, rather than through IBM Fusion,
is a choice the operator makes. Bootwright enables no console plugin
([B780](milestones/backlog.md#b780)). No specialization is registered.
Candidate discriminators, should a run show divergence: the ocs-operator bundle the
approved odf-operator CSV pulls in, whether the cluster serves a
StorageSystem kind its console expects, the Ceph monitors' allowed ciphers,
and the nodes' kernel version for `aes256k` keys.

### acm

Red Hat Advanced Cluster Management ([B91](milestones/m4.md#b91)). A
binding selects it with:

```yaml
spec:
  clusterRef: lab-compact
  packages:
    acm:
      subscription:
        channel: <a release channel of advanced-cluster-management>
        startingCSV: <a CSV that channel lists>
      mce:
        channel: <an engine channel the operator chooses>
        startingCSV: <a CSV that channel lists>
```

The operator reads both packages' channels and CSVs from the PackageManifests
`advanced-cluster-management` and `multicluster-engine`; Bootwright chooses
neither and registers no pairing; what a hub does with an engine minor other
than the one its source requests is unobserved
([B766](milestones/m4.md#b766)). The hub source's engine channels, the hub
phases, the bundles' maximum OpenShift versions and install modes are evidence
in [knowledge](../.agents/knowledge/acm-hub-install.md). A single-node hub
needs the basic availability configuration. Later engine and hub upgrades are
the operator's: the hub keeps the annotation's channel and manual approval
until the operator edits it.

### gitops

Red Hat OpenShift GitOps ([B92](milestones/m4.md#b92),
[B765](milestones/m4.md#b765)). A binding selects it with:

```yaml
spec:
  clusterRef: lab-compact
  packages:
    gitops:
      subscription:
        channel: <latest, or a gitops-1.x channel>
        startingCSV: <a CSV that channel lists>
      bootstrap:
        repoURL: https://git.example.com/platform/cluster-config.git
        targetRevision: main
        path: clusters/lab-compact
        destinationNamespace: cluster-config
        credentials: cluster-config-reader
```

The operator reads the channel's CSV from the PackageManifest
`openshift-gitops-operator`
([knowledge](../.agents/knowledge/openshift-gitops-and-argocd-handoff.md)). The
default instance's phase is `Available` only after its server Route is admitted
with a host. Applications and ApplicationSets outside `openshift-gitops` need
the instance's source namespaces, which Bootwright leaves unset. The operator's
source must
exclude every object the handoff evidence lists
([the GitOps bootstrap object](add-ons.md#the-gitops-bootstrap-object)).

### certmgr

The cert-manager Operator for Red Hat OpenShift
([B767](milestones/m4.md#b767)). It runs in `cert-manager-operator` and
places its operands, including the CA injector GitLab's operator relies on,
in `cert-manager`; both namespaces are fixed by the operator. Its OLM package,
channels, install modes and operand names are closed by its definition from
the shipped CSV.

### gitlab

GitLab through the GitLab Operator ([B93](milestones/m4.md#b93)). Bootwright
installs through OLM with exact approval because that is the route its closed
program supports; the operator's release manifest is
[B786](milestones/backlog.md#b786) (Q51), and the vendor's position on each
route is evidence in
[knowledge](../.agents/knowledge/gitlab-operator-on-openshift.md). A binding
selects it with:

```yaml
spec:
  clusterRef: lab-compact
  packages:
    certmgr:
      subscription:
        channel: <a channel the catalog lists>
        startingCSV: <the CSV that channel lists>
    gitlab:
      subscription:
        channel: stable
        startingCSV: <the GitLab Operator CSV the channel lists>
        source: certified-operators
      chartVersion: <one of the chart versions that operator release lists>
      rootPasswordRef: gitlab-root
      tlsCertificateRef: apps-wildcard
      database:
        host: <PostgreSQL host>
        username: gitlab
        passwordRef: gitlab-db
      redis:
        host: <Valkey host>
      objectStorage:
        connectionRef: gitlab-s3-connection
        registryRef: gitlab-s3-registry
      gitaly:
        storageClass: <a ReadWriteOnce StorageClass>
```

The operator supplies outside the cluster: a PostgreSQL database with the
extensions GitLab lists (`amcheck`, `btree_gist`, `pg_trgm` and `plpgsql`)
available and empty; a standalone Redis or Valkey; an S3 endpoint with the
buckets `gitlab-artifacts`, `git-lfs`, `gitlab-packages`, `gitlab-uploads`,
`gitlab-terraform-state`, `gitlab-ci-secure-files` and the registry bucket,
each distinct. The versions of each are the operator's choice (D163); GitLab's
stated ranges and its position on Ceph RGW are evidence in
[knowledge](../.agents/knowledge/gitlab-operator-on-openshift.md). The `GitLab` resource `gitlab` in `gitlab-system`
carries the declared chart version and these values, and no others:

| Value | Setting |
| --- | --- |
| `global.edition` | `ee` |
| `global.hosts.domain` | the applications domain |
| `global.ingress.enabled` | `true` |
| `global.ingress.class` | `none`, so the OpenShift router takes the Ingress objects |
| the route-termination ingress annotation | `edge` |
| `global.ingress.configureCertmanager` | `false` |
| `global.ingress.tls.secretName` | `bootwright-gitlab-tls` |
| `global.gatewayApi.enabled`, `global.gatewayApi.configureCertmanager`, `installEnvoy`, `nginx-ingress.enabled` | `false` |
| `prometheus.install` | `false` |
| `global.initialRootPassword.secret`, `.key` | `bootwright-gitlab-root-password`, `password` |
| `global.psql.host`, `.port`, `.database`, `.username` | from `database` |
| `global.psql.password.secret`, `.key` | `bootwright-gitlab-postgresql`, `password` |
| `global.redis.host`, `.port` | from `redis` |
| `global.redis.auth.enabled`, `.secret`, `.key` | `true`, `bootwright-gitlab-redis`, `password` with `redis.passwordRef`; else `false` |
| `global.appConfig.object_store.enabled`, `.connection.secret`, `.connection.key` | `true`, `bootwright-gitlab-object-storage`, `connection` |
| `global.registry.bucket` | `objectStorage.registryBucket` |
| `registry.storage.secret`, `.key` | `bootwright-gitlab-registry-storage`, `config` |
| `global.gitlab.license.secret`, `.key` | `bootwright-gitlab-license`, `license`, only with `licenseRef` |
| `gitlab.gitaly.persistence.storageClass`, `.size` | from `gitaly` |

GitLab has no Git over SSH through Routes; clone and push use HTTPS
([B788](milestones/backlog.md#b788)). The database, Redis and buckets are
outside the cluster and outside Bootwright: they keep GitLab's data after a
destroy, while GitLab's Rails secret, generated in the cluster, does not
survive a virtual cluster, so a fresh apply needs an empty database and empty
buckets ([B793](milestones/backlog.md#b793)). Each operator release renders only
the chart versions it lists, and the operator's OpenShift support table and its
CSV's maximum OpenShift version are evidence, never a rule, in
[knowledge](../.agents/knowledge/gitlab-operator-on-openshift.md).

## Acceptance profiles

Profiles are created by the slices that implement them, beside the cluster
examples they bind to, and each names its cluster, the controller budget an
operator provides, its packages and its operator gate.

| Profile | Submilestone | Cluster | Controller budget | Packages | Operator gate |
| --- | --- | --- | --- | --- | --- |
| addons-sno | M4b | M3b's lab-sno single node on libvirt+sushy, connected | [lab-sno's](container-cluster-coverage.md#acceptance-profiles): with a 4/8/40 reserve, at least 12 logical CPUs, 24 GiB available memory and 170 GiB free | `nmstate` | [B755](milestones/m4.md#b755) |
| addons-lb | M4c | M3b's lab-sno single node on libvirt+sushy, connected, with one machine-network pool of eight addresses; an optional row on M3c's lab-compact | lab-sno's (lab-compact's for the optional row) | `metallb` | [B757](milestones/m4.md#b757) |
| addons-df | M4d | M2's three-node virtual Ceph cluster (MON 3, MGR 2, OSD 3, two MDS for one CephFS filesystem with the `csi` subvolume group, a prometheus service and a TLS object gateway) beside M3c's lab-compact cluster, whose install pull secret carries the IBM entitlement and whose trust bundle policy is `Always`, on one libvirt host | the Ceph guests at 4 vCPU and 13 GiB each, the lab-compact guests and the reserve: with 4/8/40, at least 34 logical CPUs, 95 GiB and 820 GiB | `fdf` | [B761](milestones/m4.md#b761) |
| addons-hub | M4e | M3c's lab-compact topology with three schedulable masters of 8 vCPU, 24 GiB and 120 GiB, connected, plus an operator-provided HTTPS Git repository | with a 4/8/40 reserve, at least 28 logical CPUs, 80 GiB and 400 GiB | `acm`, `gitops` | [B766](milestones/m4.md#b766) |
| addons-gitlab | M4f | a fresh apply of addons-df's complete input (Ceph cluster, export, lab-compact, `fdf`) plus the GitLab bindings, after any earlier addons-df apply is destroyed, with at least 4 vCPU and 12 GiB of memory free beyond addons-df's use; Gitaly on its Data Foundation RBD StorageClass; S3 on M2's gateway or an operator-run endpoint; PostgreSQL 17 and Valkey 7.2 run by the operator on the libvirt host | addons-df's plus GitLab's increment | `fdf`, `certmgr`, `gitlab` | [B768](milestones/m4.md#b768) |

## Qualification rules

- **Tiers.** In-tree tests qualify contracts, refusals and recovery over a
  fake `oc` and never reach a cluster. Each submilestone exits on an emulated
  rehearsal on libvirt+sushy; no package needs real hardware.
- **Package qualification.** Every package passes the
  [package qualification](add-ons.md#package-qualification) suite before it
  is offered: registry conformance, purity and determinism, content safety at
  its largest admitted configuration, an admission test per rule, render and
  plan goldens, a fake-cluster journey, and parity between this page's rows
  and the registry. A unit or fake success never qualifies a package; its
  submilestone's operator gate does.
- **Rows.** Each operator gate is an owner-accepted
  [ledger](../docs/acceptance.md) row from a build that contains its items'
  slices, recording the build commit, the release minor, every channel, CSV
  and catalog image chosen, each phase's duration, its interruption drills
  (each continued by repeating `apply` with every object `unchanged`),
  `bootwright diff --observe` reporting no drift, a settled apply that
  contacts no cluster, and a fresh `destroy` completing by retirement. The
  public row summarizes these as the ledger defines it; the fields it does not
  carry stay in the private evidence archive.
- **Versions.** When a slice moves a frozen request version, a row accepted on
  an earlier build is repeated, and a context an earlier build applied is
  destroyed with that build first. A package's rendering change moves only
  its instance requests; a fix to the cluster objects role or modules alone
  continues a live operation.
- **Evidence, not approval.** A run proves its profile on its release only.
  A release minor whose native mechanics differ gets its own row for the
  affected profile and a registered specialization; no row or specialization
  approves or refuses a release, a channel or a CSV.
- **Unverified native facts.** A fact the research left uncertain, or one only
  the earlier implementation observed, is re-verified by the first run that
  meets it and recorded in knowledge: `--ignore-not-found` classifying only
  NotFound, `oc create --field-manager` recording the manager, an
  InstallPlan's owner reference to its Subscription, its
  `status.bundleLookups[].catalogSourceRef` readable before approval, how `oc
  create` exposes a webhook refusal's status reason, whether `ResolutionFailed`
  can be transient on a Subscription of a ready catalog (the fail-at-once rule
  is an assumption re-verified by [B755](milestones/m4.md#b755)), the
  `nmstate-webhook` Deployment name ([B755](milestones/m4.md#b755)), that
  `MetalLB` `Degraded` also reports transient sync failures
  ([B757](milestones/m4.md#b757)), the `openshift.io/cluster-monitoring`
  label on `openshift-storage` and whether ocs-operator or Rook accept a
  `ceph-rgw-tls-cert` without the private key, which Red Hat's document says
  the file includes ([B761](milestones/m4.md#b761)), that `upgradeStrategy:
  Default` is the OperatorGroup API default ([B766](milestones/m4.md#b766)),
  the single CSV of the MetalLB and GitLab
  first plans, which plans odf-operator approves, `redhat-operators` serving
  `odf-operator` with the same namespace and objects, the CSI Secret `userID`
  form and an `aes` key accepted by the Data Foundation client, NooBaa's
  trust of the gateway's issuing CA, the engine
  Subscription honoring the hub's annotation, what a hub does with an engine
  minor other than the one its source requests, the GitOps application
  controller's identity and permissions, the GitLab operator's webhook
  certificate under OLM, and each operator leaving its package's declared
  fields unchanged.

## Left out

Designs M4 leaves out are parked items, each with its reason and the evidence
that would revive it: online removal of one instance
([B769](milestones/backlog.md#b769)), add-on preflight
([B770](milestones/backlog.md#b770)), the `add-ons/_store` selection
exception's removal ([B771](milestones/backlog.md#b771)), OLM v1
([B772](milestones/backlog.md#b772)), the global pull-secret merge
([B773](milestones/backlog.md#b773)), a least-privilege cluster identity
([B774](milestones/backlog.md#b774)), NMState's disconnected probe host
([B775](milestones/backlog.md#b775)), per-interface MetalLB forwarding
([B776](milestones/backlog.md#b776)), overlapping pools of two clusters
([B777](milestones/backlog.md#b777)), MetalLB BGP
([B744](milestones/backlog.md#b744)), Data Foundation on a disconnected
cluster ([B778](milestones/backlog.md#b778)), in-transit encryption
([B779](milestones/backlog.md#b779)), the console plugin
([B780](milestones/backlog.md#b780)), an external Ceph cluster
([B107](milestones/backlog.md#b107)), hub import of other clusters
([B781](milestones/backlog.md#b781)), GitOps cluster registration
([B782](milestones/backlog.md#b782)), a Bootwright-owned Argo CD instance
([B783](milestones/backlog.md#b783)), SSH, private-CA and `ApplicationSet`
bootstrap roots ([B784](milestones/backlog.md#b784)), ACM component overrides
([B785](milestones/backlog.md#b785)), GitLab's release manifest
([B786](milestones/backlog.md#b786)), composed object-storage documents
([B787](milestones/backlog.md#b787)), Git over SSH
([B788](milestones/backlog.md#b788)), in-cluster PostgreSQL and Redis
([B789](milestones/backlog.md#b789)), GitLab Community Edition
([B790](milestones/backlog.md#b790)), GitLab on a disconnected cluster
([B791](milestones/backlog.md#b791)), GitLab Runner
([B792](milestones/backlog.md#b792)), GitLab backups and Rails secret
custody ([B793](milestones/backlog.md#b793)), an HTTP endpoint check
([B794](milestones/backlog.md#b794)) and TLS trust toward GitLab's external
services ([B795](milestones/backlog.md#b795)).
