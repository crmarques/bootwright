# Data Foundation external mode against Ceph

How Data Foundation (ODF, and IBM's Fusion Data Foundation) attaches to an
external Ceph cluster: the details Secret, what the exporter prints, what
`ocs-operator` builds, when the StorageCluster is ready, and what stays on Ceph
after removal. The package is [B85](../../specs/milestones/m4.md#b85), the Ceph
side is [B86](../../specs/milestones/m4.md#b86) and
[B762](../../specs/milestones/m4.md#b762), the produced-details placement is
[B760](../../specs/milestones/m4.md#b760), and the live proof is
[B761](../../specs/milestones/m4.md#b761). The catalog and entitlement are in
[fusion-data-foundation-catalog.md](fusion-data-foundation-catalog.md), key
types in [cephx-key-types.md](cephx-key-types.md), the OLM side in
[olm-v0-exact-install.md](olm-v0-exact-install.md).

Provenance: `R3:C<n>` is a claim id of the M4 definition's research of
2026-10-20 (Fusion Data Foundation) and `RV-R3` its adversarial review, with
the verdict (confirmed, corrected or uncertain); `O<n>` is one of its open
questions. Release labels name where a fact was read; they are not supported or
required versions (D163). The ids name research notes this repository does not
track; each statement here carries its fact, and an id only records where it
was read. "Observed by the v1 implementation (main-v1); re-verify on this tree"
marks a v1 fact the research did not confirm; the v1 lesson ids `D-nn` name
untracked review notes of that branch.

## The operator and the CRD race

- The `odf-operator` bundle supports OwnNamespace and SingleNamespace only and
  owns no CRDs. `ocs-operator` is non-standalone and owns `storageclusters`,
  `ocsinitializations`, `storageautoscalers`, `storageclusterpeers` and
  `storageconsumers` (R3:C8, confirmed).
- `odf-operator` creates the Subscriptions of its dependent packages itself and
  copies its own Subscription's catalog source into them. `ocs-operator`
  therefore appears only after `odf-operator` has run, and
  `storageclusters.ocs.openshift.io` does not exist at the first CSV (R3:C11,
  confirmed; the full dependent-package list was not enumerated, O3).
- Applying the StorageCluster right after the first CSV fails with `no matches
  for kind "StorageCluster" in version "ocs.openshift.io/v1"`. Waiting for
  `ocs-operator` is unsatisfiable: v1 observed its Deployment held at zero
  replicas until a StorageCluster exists (observed by the v1 implementation
  (main-v1); re-verify on this tree; D-01, with the zero-replica part
  unverified, R3:C54). The wait is the CRD reaching `Established`.

## The details Secret and the exporter's JSON

- The console creates `Secret/rook-ceph-external-cluster-details` (Opaque) in
  the ODF namespace with `stringData.external_cluster_details` set to the
  uploaded file. The constants are the same in 4.20, 4.21 and 4.22. The operator
  parses a JSON array of `{kind, name, data}` entries and creates from it, owned
  by the StorageCluster: ConfigMaps, Secrets (updating `Data` when it differs),
  `CephFilesystemSubVolumeGroup`, `CephBlockPoolRadosNamespace` and
  StorageClasses. `CephCluster/monitoring-endpoint` is only read.
  `status.externalSecretHash` tracks the checksum (R3:C34, confirmed).
- The script is `create-external-cluster-resources.py` in the downstream Rook
  repository, delivered as `ceph-external-cluster-details-exporter.py`. It
  needs `rados`, `rbd` and `requests`, connects with admin credentials and
  prints the JSON to stdout, and to `--output` when given (R3:C17 and C18,
  confirmed). Its entries, in order: ConfigMap `external-cluster-user-command`;
  ConfigMap `rook-ceph-mon-endpoints`; Secrets `rook-ceph-mon` and
  `rook-ceph-operator-creds`; CephCluster `monitoring-endpoint`; the CSI Secrets
  for RBD and, when CephFS resolves, CephFS; Secret `rook-ceph-dashboard-link`
  when `ceph mgr services` lists a dashboard; the RADOS-namespace entries; the
  StorageClasses `ceph-rbd`, `ceph-rbd-topology`, `cephfs` and `ceph-rgw`;
  Secrets `rgw-admin-ops-user` and `ceph-rgw-tls-cert` (R3:C33, confirmed with
  the conditions below). Under restricted authentication the CSI Secret names
  are carried by the StorageClass parameters, not fixed.
- The mon entry is the quorum leader's address, on the msgr2 port by default
  (`--v2-port-enable` defaults to true downstream); `maxMonId` is `"0"` and
  `mapping` is `"{}"` (R3:C31, confirmed).
- Re-running reuses existing users and returns their stored keys, tolerates an
  existing RGW user, and gets or creates the `csi` subvolume group (R3:C55 and
  C26, confirmed in part). The script cannot say which entities it created and
  which it reused.
- The script prints every key, the RGW keys and the dashboard link in plain
  JSON; `--verbose` also prints raw monitor command output with key material
  (R3:C56, confirmed in part).

## What exit 0 does not prove

- `ExecutionFailureException` exits non-zero. `KeyError` and `OSError` are
  printed and the exit code is 0 (R3:C18, confirmed).
- A failed HEAD of the RGW endpoint (HTTP then HTTPS, status 200 only), an fsid
  mismatch or a missing prefixed pool leaves `RGW_ENDPOINT` empty, writes to
  stderr and exits 0, so the JSON has no `ceph-rgw` entry. In the same function
  the error variable is reassigned, so a failed realm, zonegroup or zone
  validation is overwritten, and a failed `rgw-admin-ops-user` create or caps
  step is overwritten by the endpoint validation, which skips the fsid check
  when the `info` cap is unsupported. The JSON can then carry `ceph-rgw` and
  `rgw-admin-ops-user` with `accessKey: null` and `secretKey: null`.
  `ceph-rgw-tls-cert` is emitted whenever `--rgw-tls-cert-path` is given, even
  when `ceph-rgw` was dropped (RV-R3 C28, corrected; correction 1).
- v1 saw the first form: everything healthy, no `ceph-rgw` StorageClass, NooBaa
  on a 50 GiB RBD `pv-pool` backing store (observed by the v1 implementation
  (main-v1); re-verify on this tree; D-02).
- A check that does not trust the exit code compares `ceph-rgw`'s endpoint with
  the declared one, requires non-empty string keys in `rgw-admin-ops-user`,
  accepts `ceph-rgw-tls-cert` only together with `ceph-rgw`, and checks each
  declared multisite name separately (RV-R3 correction 1).
- Multisite names are all or none: realm, zonegroup and zone (R3:C28). IBM adds,
  for a non-default zonegroup, the hostname
  `rook-ceph-rgw-ocs-external-storagecluster-cephobjectstore.openshift-storage.svc`,
  because Data Foundation sends S3 requests with that host name (R3:C28; the IBM
  page is for an old release).

## The monitoring endpoint

- `ocs-operator` 4.20, 4.21 and 4.22 treat the `monitoring-endpoint` entry as
  required and return `unable to retrieve "monitoring-endpoint" external
  resource`. The script raises when the manager map lists no prometheus service
  and omits the entry under `--skip-monitoring-endpoint`. The documents call the
  monitoring flags optional, which means the flag, not the entry (R3:C27,
  confirmed). The operator then rejects an empty address and, with a port,
  dials each address and accepts the set when one answers.
- v1 saw the StorageCluster in `Phase: Error` over this entry, and saw the
  prometheus module serve TLS signed by the cephadm root CA when the management
  gateway was secured, so the script's dial with no CA argument failed with
  `CERTIFICATE_VERIFY_FAILED` (observed by the v1 implementation (main-v1);
  re-verify on this tree; D-09).

## The StorageCluster and what the operator builds

- The console applies `externalStorage.enable: true`, a network encryption
  setting, a default-StorageClass choice and an empty `labelSelector`. The
  external block has only `enable` in 4.21, and `spec.storageDeviceSets` must be
  empty ("'StorageDeviceSets' should not be initialized in an external
  CephCluster"). The console sets `metadata.name` to the wizard's input; the
  documents' `ocs-external-storagecluster` is a convention. The 4.20 and 4.21
  documents still say "Create StorageSystem" and the 4.21 console creates none;
  the 4.22 documentation still verifies a StorageSystem named
  `ocs-external-storagecluster` (RV-R3 C35); whether the 4.20 and 4.22 consoles
  match is open (O2). Derived names follow
  the StorageCluster name (RV-R3 C35, corrected; correction 4).
- The operator builds an external CephCluster, crash collector disabled,
  monitoring through `externalMgrEndpoints` and the manager port; StorageClasses
  named `<storagecluster>-ceph-rbd`, `-cephfs`, `-ceph-rgw`,
  `-ceph-rbd-rados-namespace-<ns>` and `-ceph-non-resilient-rbd`; and a
  CephObjectStore with `gateway.externalRgwEndpoints`. The RGW StorageClass
  exists only when a five-second TCP dial to the endpoint succeeds, and an
  unreachable endpoint returns a reconcile error. In 4.22 StorageClass creation
  waits for the CephCluster's fsid (R3:C36, confirmed). The documented set is
  `...-ceph-rbd`, `...-ceph-rgw` (only with RGW), `...-cephfs` (only with
  CephFS) and `openshift-storage.noobaa.io` (R3:C37, confirmed; BucketClass
  names open, O4).
- The operator enables TLS for the gateway only when the Secret
  `ceph-rgw-tls-cert` exists, setting `securePort` and `sslCertificateRef`.
  Without it an HTTPS gateway is attached as cleartext on its TLS port (R3:C36
  and C54, confirmed; v1 hit it, D-04).
- `--rgw-tls-cert-path` is read as UTF-8, right-stripped, used as the script's
  `verify=` bundle and emitted as the Secret's `cert`. Red Hat's text says the
  file "must include all intermediate certificates and private keys", which
  conflicts with the bundle use; the script does not parse it (R3:C49,
  confirmed; what Rook and NooBaa then do with the value is open, O5).

## Readiness

- Phases are `Progressing`, `Connecting`, `Ready`, `Not Ready`, `Error`,
  `Deleting`, `Ignored`, `Expanding Capacity` and `Onboarding`. In external mode
  `Connecting` follows the CephCluster state `Connecting`, `Ready` follows
  `Connected`, and anything else is `Not Ready` (R3:C39; RV confirmed only the
  external-mode mapping, not the full phase list).
- `oc get cephcluster` shows phase `Connected`, message "Cluster connected
  successfully", health `HEALTH_OK` and external `true`. While connecting the
  StorageCluster carries `ExternalClusterConnecting` with `External CephCluster
  is trying to connect: <msg>`, an error state sets `ExternalClusterConnected`
  `False` with `External CephCluster error: <msg>`, and non-OK Ceph health adds
  `Upgradeable=False`. Both external conditions are removed on success
  (R3:C40, confirmed in part; RV did not re-read the condition message texts).
- NooBaa is created only after the CephCluster is `Connected` ("Waiting for the
  External Ceph Cluster to be connected before starting Noobaa"). A NooBaa phase
  of empty, `Verifying`, `Creating`, `Connecting` or `Configuring` sets
  `Progressing=True` with reason `NoobaaInitializing`, and `Rejected` is
  degraded. The StorageCluster is `Ready` with `ReconcileComplete=True` only
  after that, so `Ready` is necessary and not sufficient. The condition types are
  `Available`, `Progressing`, `Degraded`, `Upgradeable`, `ReconcileComplete`,
  `ExternalClusterConnected`, `ExternalClusterConnecting` and `VersionMismatch`.
  A skipped-StorageClass error reports `Progressing` instead of `Error` (R3:C41
  and C42, confirmed).
- v1 saw `Available=False` written one second before Rook finished connecting
  and not re-evaluated for nine hours, while `ReconcileComplete` and
  `Progressing` kept heartbeating and the truth was `Progressing=True`, reason
  `NoobaaInitializing` (observed by the v1 implementation (main-v1); re-verify
  on this tree; D-17). The stale condition was observed only beside a real root
  cause, the NooBaa trust failure below.

## NooBaa and the RGW certificate

- No code in `ocs-operator` 4.20 to 4.22's NooBaa reconciler references the RGW
  certificate. Red Hat advisories list backports for "noobaa-default-backing-store
  is in creating state" with `SELF_SIGNED_CERT_IN_CHAIN` in External Mode
  (DFBUGS-5939, 5940, 5942 and 5943 across 4.16 to 4.19; RV confirmed
  DFBUGS-5942 in RHSA-2026:12279, the other three are R3 advisory titles).
  Whether 4.20 to 4.22
  carry a fix, and from what trust source, is not established (R3:C50,
  confirmed at title level; O5).
- v1 saw NooBaa stay in `Configuring` for nine hours with `x509: certificate
  signed by unknown authority` while CephCluster was `Connected` and `HEALTH_OK`.
  NooBaa's S3 client trusts `/etc/ocp-injected-ca-bundle/ca-bundle.crt`, filled
  from the cluster-wide trusted CA bundle, and the service CA, so the
  `ceph-rgw-tls-cert` entry, which configures Rook, did not help (observed by
  the v1 implementation (main-v1); re-verify on this tree; D-05).
- v1's first fix merged the gateway certificate into the Proxy's trusted CA
  ConfigMap at add-on time. The Machine Config Operator renders that bundle into
  every node trust store, so the first apply rolled the machine config pools
  serially: three reboots on a compact cluster, taking KubeVirt guests with
  them. The final fix placed the certificate in the cluster install input as an
  additional trust bundle, and a certificate already in cluster trust needed
  none of it (observed by v1; re-verify; D-05).

## Reachability

- The documented ports from OpenShift to Ceph are 6789 and 3300 (monitors),
  6800 to 7300 (OSD, manager, MDS) and 9283 (manager Prometheus exporter); the
  table may be truncated. The RGW port is whatever endpoint was passed (R3:C47,
  confirmed in part).
- The five-second dials to the RGW endpoint and the monitoring addresses start
  in the `ocs-operator` pod, not the node host network. The script's own
  checks start on the Ceph host, so the exporter passing there does not prove
  pod reachability (R3:C48, confirmed; O10).

## Removal

- `uninstall.ocs.openshift.io/cleanup-policy` does not apply to external mode.
  `uninstall.ocs.openshift.io/mode` defaults to `graceful`, which waits for PVCs
  and OBCs, and `forced` proceeds. The operator adds both annotations with
  defaults (`graceful`, `delete`) and rewrites unrecognized values; `forced`
  also sets Rook's `allowUninstallWithVolumes` and the NooBaa cleanup
  confirmation (R3:C43, confirmed).
- The documented order deletes snapshots, then PVCs and OBCs from Data
  Foundation classes, then the StorageCluster, then the project, then leftover
  `Released` volumes and 17 CRDs. IBM's support page for internal mode adds
  steps for newer clients and finalizers and was not re-checked (R3:C44,
  confirmed for the Red Hat part).
- On StorageCluster deletion the operator deletes the details Secret, and the
  objects created from the JSON are owned by the StorageCluster and garbage
  collected (R3:C45, confirmed).
- Left on Ceph: the entities `client.healthchecker` (or the `--run-as-user`
  name) and the four `client.csi-*` entities, or their restricted names with
  `.<generation>` suffixes; the RGW user `rgw-admin-ops-user` and its keys; the
  `csi` subvolume group, pinned `distributed 1`, and any `--subvolume-group`; the
  `rbd` application tag; and a RADOS namespace created for the consumer. The
  script has no removal path, no Red Hat or IBM document prescribes removal, and
  ordinary Ceph commands do it. Unrestricted entities and `rgw-admin-ops-user`
  are shared by every consumer that attached without restricted authentication.
  The documents say the RGW endpoint, CephFS details and RBD pool must stay
  unchanged on the Ceph cluster after StorageCluster creation (R3:C46,
  confirmed). Per-consumer principals are Bootwright's design, not an ODF
  contract (R3:C54).

## To re-verify on a live cluster

[B761](../../specs/milestones/m4.md#b761) records the answers it reaches:

- O2: whether the 4.20 and 4.22 consoles create exactly the Secret and the
  StorageCluster above (`oc get storagecluster <name> -n openshift-storage -o
  yaml`, then diff `.spec`).
- O4: the NooBaa BucketClass names and the object bucket provisioner (`oc get
  bucketclass -n openshift-storage`).
- O5: NooBaa's trust of a private RGW CA (`oc logs -n openshift-storage
  deploy/noobaa-operator`, filtered for `x509`, and the `ceph-rgw-tls-cert`
  subject and issuer).
- O8: whether operand pods pull through the global pull secret alone (`oc get
  events -n openshift-storage --field-selector reason=Failed`).
