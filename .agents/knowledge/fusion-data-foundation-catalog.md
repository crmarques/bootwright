# IBM Fusion Data Foundation catalog, entitlement and images

How IBM delivers Data Foundation as an OLM catalog, what it needs on the
cluster to pull, and what differs from Red Hat's product. The package is
[B85](../../specs/milestones/m4.md#b85), the live proof
[B761](../../specs/milestones/m4.md#b761), and the mirror set a disconnected
cluster would need is parked as [B778](../../specs/milestones/backlog.md#b778).
The attach itself is in
[data-foundation-external-mode.md](data-foundation-external-mode.md), the OLM
side in [olm-v0-exact-install.md](olm-v0-exact-install.md).

Provenance: `R<n>:C<m>` is a claim id of the M4 definition's research of
2026-10-20 (R1 OLM and catalogs, R3 Fusion Data Foundation) and `S<n>` a
summary item; `RV` marks a claim the adversarial review re-checked, with its
verdict (confirmed, corrected or uncertain); `O<n>` is an open question of R3.
Release labels name where a fact was read; they are not supported or required
versions (D163). The ids name research notes this repository does not track;
each statement here carries its fact, and an id only records where it was read.
"Observed by the v1 implementation (main-v1); re-verify on this tree" marks a
v1 fact the research did not confirm; the v1 lesson ids `D-nn` name untracked
review notes of that branch.

## One product under two brands

- IBM states that Fusion Data Foundation is "also known as Red Hat OpenShift
  Data Foundation" and that its content applies to ODF unless noted (R3:C1,
  confirmed). The package `odf-operator`, the namespace `openshift-storage`, the
  CRDs and the `stable-4.NN` channel scheme are the same (RV-R3 C51, corrected).
- IBM's differences: display name "IBM Storage Fusion Data Foundation",
  provider IBM, `support: IBM`, `valid-subscription` `["IBM Storage Fusion"]`
  on `odf-operator`; the catalog `icr.io/cpopen/isf-data-foundation-catalog`
  instead of `redhat-operators`; images from `icr.io/cpopen` and
  `cp.icr.io/cp/df`; extra packages in the catalog (`ibm-spectrum-scale-operator`,
  `cnsa-dependencies`). The `ocs-operator` CSV still lists the ODF subscription
  names (R3:C3 and C51, confirmed). The Red Hat package name in
  `redhat-operators` was not independently confirmed.
- One code path can serve both products with a catalog, channel and
  pull-secret variation (R3:C1). IBM's documented install is through IBM
  Fusion's console, and no IBM document describes an OLM-only install of the
  catalog (R3:C2 confirmed; C15 uncertain, a negative finding). Whether IBM
  supports that path is IBM's answer. No source says whether an IBM Storage Ceph
  external deployment consumes an entitlement (R3:C3, open), and the
  supportability of an exporter and Ceph combination is decided by Red Hat's
  authenticated checker (observed by the v1 implementation (main-v1);
  re-verify on this tree; D-25).
- IBM documents Fusion Data Foundation minors up to one below the newest
  catalog tag when read, and Red Hat's pages say ODF minor N needs OpenShift N or
  later. Neither is a support or version rule here (R3:C13 and C14; D163).
- The external IBM Storage Ceph must run on x86 bare metal; consuming clusters
  can run elsewhere. IBM recommends one RBD pool per Data Foundation cluster, the
  Ceph Dashboard and the PG autoscaler. No IBM page lists Ceph versions per
  Data Foundation release (R3:C16 and C53, confirmed in part).

## The catalog

- `icr.io/cpopen/isf-data-foundation-catalog` answers anonymously. The image is
  `opm serve /configs`, runs as user 1001 and is built for amd64, ppc64le and
  s390x. Tags `v4.12` through `v4.22` plus patch tags existed on 2026-10-20 and
  no tag for the next minor (R3:C4 and C5, confirmed).
- IBM's CatalogSource is `isf-data-foundation-catalog` in `openshift-marketplace`
  with publisher IBM, `sourceType: grpc` and `registryPoll` of 15 minutes. IBM
  says it is "normally created automatically" by Fusion and otherwise must be
  created by hand. The pages show neither `grpcPodConfig` nor a pull secret
  (R3:C6, confirmed).
- The v4.21 catalog holds 16 packages (RV-R3 C7), among them `odf-operator`, `ocs-operator`,
  `mcg-operator`, `rook-ceph-operator`, `cephcsi-operator`, `ocs-client-operator`,
  `odf-csi-addons-operator`, `odf-prometheus-operator`, `recipe` and
  `odf-dependencies`. `odf-operator` has one entry per channel, no `replaces`
  edge, and relies on `olm.skipRange`. The default channel is `stable-<NN>`.
- Catalog tag `v4.N` carries the channels `stable-4.N` and `stable-4.(N-1)`, and
  the older channel sits at an older patch than catalog `v4.(N-1)` itself offers.
  On 2026-10-20 catalog v4.22 held `stable-4.21` at 4.21.7 while catalog v4.21
  held it at 4.21.13; the operator reads the channel's CSV from the catalog it
  chose (RV-R3 C7 and correction 3, confirmed from the decoded catalogs). This
  is an observation, never a pairing rule (D163).
- The `odf-operator` bundle supports OwnNamespace and SingleNamespace, so the
  OperatorGroup targets `openshift-storage`; it is `standalone`, suggests
  `openshift-storage`, enables the `odf-console` plugin and declares the
  disconnected feature (R3:C8, confirmed). IBM's page installs into "A specific
  namespace on the cluster" with the console plugin enabled, approval automatic
  or manual, and an optional `oc annotate namespace openshift-storage
  openshift.io/node-selector=` when a cluster default node selector exists
  (R3:C10, medium: the page is for an old release). The Fusion 2.13 page checks
  that `openshift-storage` does not exist before enabling (R3:C2).
- The four `odf-operator` bundles decoded on 2026-10-20 (4.20.19, 4.21.13,
  4.21.7 and 4.22.5) carry `olm.maxOpenShiftVersion` set to the minor after
  their own, which blocks a cluster update past it
  ([olm-v0-exact-install.md](olm-v0-exact-install.md); R3:C9, confirmed for the
  values, medium for the OpenShift wording).

## Dependent Subscriptions and approval

- `odf-operator` copies its Subscription's `CatalogSource` and namespace into
  the Subscriptions of its dependent packages. IBM's upgrade procedure patches
  the `odf-operator`, `cephcsi-operator`, `ocs-operator`, `mcg-operator` and
  `odf-csi-addons-operator` Subscriptions to the new source (R3:C11,
  confirmed).
- The generated `odf-dependencies` bundle declares nine `olm.package`
  dependencies (`ocs-operator`, `rook-ceph-operator`, `ocs-client-operator`,
  `noobaa-operator`, `csi-addons`, `odf-external-snapshotter-operator`,
  `cephcsi-operator`, `odf-prometheus-operator`, `recipe`), an extra one for
  `ibm-storage-odf-operator` in the IBM build, and a conditional `CNSA` block.
  `odf-operator` reads a `pkgs-config` ConfigMap, creates or updates the
  `odf-dependencies` Subscription with a `startingCSV`, and approves any
  `Manual` plan in its namespace that lists a configured CSV (RV-R1 C57 and
  correction 4, corrected).
  The approval race is in
  [olm-v0-exact-install.md](olm-v0-exact-install.md).
- v1 approved the single InstallPlan that listed only the expected CSV, and its
  second-level operators arrived through a Subscription the operator created
  later (observed by the v1 implementation (main-v1); re-verify on this tree;
  D-18).

## Registries and entitlement

- Operand images under `cp.icr.io/cp/df/*` answer 401 without credentials, and
  the bundle and operator images under `icr.io/cpopen/*` answer 200 anonymously.
  IBM documents `docker login cp.icr.io -u cp -p <entitlement key>` and says the
  key "must include the IBM Fusion entitlement". The cluster-wide route merges
  `cp:<key>` as `cp.icr.io` into `openshift-config/pull-secret` with `oc set
  data` (R3:C12, corrected on the Red Hat registries below; the user is `cp`).
- Every inspected Fusion bundle also needs Red Hat registry credentials. They
  pull `registry.redhat.io/openshift4/*` and `rhel9/*`; some also pull
  `registry.redhat.io/odf4/*`, `registry.redhat.io/rhceph/*` or
  `cp.icr.io/cp/ibm-ceph/*`. The image set varies per bundle, so a pull-secret
  check or a mirror set comes from the selected bundle's `relatedImages`, never
  from a fixed per-product list (RV-R3 C12 and C51, corrected; correction 2).
- For proxies IBM lists `cp.icr.io`, `icr.io`, `dd0.icr.io`, `dd2.icr.io`,
  `dd4.icr.io` and `dd6.icr.io`. For mirrors it documents an
  ImageDigestMirrorSet `isf-fdf-idms` and an ImageTagMirrorSet `isf-fdf` for the
  catalog image (R3:C12, uncertain: the names and allowlist were not
  re-checked). The IBM mirroring example lists a subset of the catalog's
  packages plus `odf-dependencies`. `oc-mirror` v2 does not detect bundle
  dependencies, so the packages and the default channel are listed explicitly
  (R1:C24, confirmed for 4.22).
- v1 recorded that Fusion operand images need the IBM entitlement in the cluster
  pull secret, and that the catalog index at `icr.io/cpopen` is public
  (observed by the v1 implementation (main-v1); re-verify on this tree; D-16).

## Changing the global pull secret

- The documented procedure downloads `openshift-config/pull-secret`, appends the
  registry's credentials and applies `oc set data secret/pull-secret -n
  openshift-config --from-file=.dockerconfigjson=<file>`. The OLM and IBM text
  says the update "rolls out to all nodes" and that cluster resources "must
  adjust", which can briefly limit usability (R1:C45 and C47, confirmed). IBM's
  own caution: add auths and never overwrite.
- The machine-config-daemon classifies a change to the rendered pull secret
  (`/var/lib/kubelet/config.json`) as "no action": no reboot and no drain on
  the 4.20 and 4.22 branches (4.21 not read) when it is the only change. A MachineConfig that also
  changes other files can still reboot or drain (R1:C46 and S4, confirmed from
  `update.go` and `drain.go`). IBM's "reload all the worker nodes" sentence is on
  its IBM Cloud page and is not an OpenShift requirement (R1:C47).
- The 4.21 and 4.22 documentation modules open with a sentence about an
  `additional-pull-secret` in `kube-system` and a note that the global pull
  secret is a HostedControlPlane feature, which describe the hosted flow and
  contradict the standalone steps in the same page (R1:C45, confirmed).
- A per-namespace or per-CatalogSource secret covers only OLM's own pulls of the
  index and the unpack Jobs. OLM does not copy secrets into operand namespaces,
  so operand images need the global secret or a secret linked to the operator's
  ServiceAccount (R1:S5, confirmed). Whether operand pods pull through the global
  secret alone is open (O8).

## The catalog pod's security context

- The catalog image runs as user 1001. OLM documents
  `spec.grpcPodConfig.securityContextConfig` as `legacy` or `restricted`. Red Hat
  documents unset as following the namespace's pod security admission, while an
  upstream issue reports that the CRD default `legacy` applies whenever
  `grpcPodConfig` is present and the field is unset (R3:C52, uncertain).
- v1 saw the catalog go `Unknown` then `TRANSIENT_FAILURE` without
  `restricted`, because the registry pod was incompatible with the namespace's
  pod security enforcement, so the Fusion operator never appeared (observed by
  the v1 implementation (main-v1); re-verify on this tree; D-14). Research
  found no IBM source that requires it, and IBM's CatalogSource examples show no
  `grpcPodConfig` (R3:C52, uncertain; RV-R3 uncertain). The live check is O7:
  apply the CatalogSource without `grpcPodConfig` and read
  `status.connectionState.lastObservedState`.
- v1 pinned the catalog by a mutable tag and never recorded a live check of the
  tag, channel and package against IBM's registry (observed by the v1
  implementation (main-v1); re-verify on this tree; D-15).
