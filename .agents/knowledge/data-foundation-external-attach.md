# Data Foundation external-cluster attach: two failure modes

The `attach-external-storage` step of `fusion-data-foundation` (and its ADR 0013
twin `openshift-data-foundation`) exports an external Ceph cluster's connection
details with rook's `create-external-cluster-resources.py`, writes them into the
`rook-ceph-external-cluster-details` Secret, and applies a `StorageCluster` with
`externalStorage.enable: true`. Two independent things break it. Both were
observed on the same prd apply on 2026-08-05, one per OpenShift cluster.

## `no matches for kind "StorageCluster" in version "ocs.openshift.io/v1"`

The step's manifests applied before the CRD existed. `follows: operatorReady`
only proves the add-on's own Subscription (`odf-operator`) reached `Succeeded`;
`storageclusters.ocs.openshift.io` is owned by `ocs-operator`, which
`odf-operator` subscribes *from its own running pod* once it starts. The kind is
therefore unresolvable for as long as that second install takes, and the apply
loses unless something burns the time first — the exporter playbook's own
5-minute ConfigMap poll is what masked it on one cluster and not the other.

Fixed by declaring the API in `spec.steps[].requires[]`; see
[addon-apply-engine-gates.md](addon-apply-engine-gates.md). Gating harder on the
add-on's own Subscription cannot close it, and waiting for `ocs-operator` to run
is unsatisfiable: `odf-operator` holds its Deployment at zero replicas until a
`StorageCluster` exists.

## Data Foundation installs with no object storage at all

Symptom: no `ocs-external-storagecluster-ceph-rgw` StorageClass, no
`rgw-admin-ops-user` Secret, and NooBaa's default backingstore is type
`pv-pool` (a 50Gi RBD PVC) rather than `s3-compatible`. Everything reports
healthy — the export step succeeds, the StorageCluster reaches Ready — but the
S3 the `StorageExport` declares does not exist.

The exporter gates **both** the `ceph-rgw` StorageClass entry and the
`rgw-admin-ops-user` Secret entry on `if self.out_map["RGW_ENDPOINT"]:`, and
sets `RGW_ENDPOINT` only when `validate_rgw_endpoint` does not return `"-1"`.
Two of its three failure paths — `endpoint_dial` failing, and `get_rgw_fsid`
failing — return `"-1"` **writing nothing to stderr**, and the script exits 0.
Only an fsid mismatch says anything at all.

The trap that caused it here: the exporter dials the RGW endpoint with
python-requests, which verifies against **certifi**, not the node's trust store,
unless `REQUESTS_CA_BUNDLE` is set. Bootwright used to set that only when the
managers published an *https* Prometheus endpoint — so a cluster whose
`mgmtGateway.exposure` is `http` (prd) got no bundle at all. The trust bundle is
now seeded from `/etc/pki/tls/certs/ca-bundle.crt` and passed on every run; the
cephadm root CA is still appended only when the monitoring endpoint is https.
The export step also refuses outright when a `StorageExport` declares
`objectGatewayRef` and the export carries no `ceph-rgw` entry, because the
exporter itself will not tell you.

**That trust bundle could never have been enough**, which the refusal then
proved on the next prd apply. A `StorageObjectGateway` ingress serves the
certificate its `tls.certificateRef` names, and on prd that `Secret` is
`source.generated` — a **self-signed** certificate Bootwright mints
(`SelfSignedCertificatePEM`, `IsCA: true`, so it is a usable trust anchor for
itself). Nothing publishes it: no node trust store holds it, and it is not in
`ca-bundle.crt` by any route. So the https dial could only ever fail.

The exporter's own flag is the answer, and it does double duty. The step now
passes `--rgw-tls-cert-path` with the staged certificate:

- `validate_rgw_endpoint` sets `cert` from that flag and `endpoint_dial` then
  uses `verify=<cert>` for the https attempt, so the dial verifies against
  exactly the certificate the gateway declares. Measured on OpenSSL 3.5: a cert
  file holding the server's own certificate verifies whether that certificate is
  self-signed **or** signed by a private CA whose chain the server sends — an
  exact trust-store match is accepted as the anchor either way. So the flag is
  right for generated and estate-issued certificates alike.
- the same contents become `RGW_TLS_CERT` and the export's `ceph-rgw-tls-cert`
  Secret entry. ocs-operator gates on that entry existing:
  `_, err := r.retrieveSecret(cephRgwTLSSecretKey, initData); if err == nil {
  tlsEnabled = true }`, and only then sets `gateWay.SSLCertificateRef` and
  `gateWay.SecurePort`. **Without it an `https` gateway is attached as
  cleartext on its TLS port** — a second silent defect that survived even a
  successful dial.

Only one certificate can reach Data Foundation, so the step refuses a gateway
whose ingresses declare more than one. It also prints the exporter's stderr in
the refusal: master's `endpoint_dial` does write `unable to connect to
endpoint: …` before returning `-1`, and `get_rgw_fsid` writes on a timeout and
on a non-`info` response — the exit code is 0 in every case, so that stream is
the only account of what happened.

## NooBaa never leaves `Configuring`: the gateway certificate is not in the cluster's trust

Symptom on prd 2026-08-07: the `StorageCluster` sat `Progressing` for nine hours
and the readiness budget expired twice, once per OpenShift cluster. Everything
Ceph-side was healthy — `CephCluster` `Connected`, `HEALTH_OK`, all five mons
reachable including the dc3 arbiter across a second public CIDR. The blocker was
NooBaa:

```
Will connect to RGW at "https://rgw.<cluster>.<domain>:443"
creating bucket nb.<id>.apps.<cluster>.<domain>
got error ...: tls: failed to verify certificate: x509: certificate signed by
unknown authority
```

`--rgw-tls-cert-path` (above) put the certificate where the **exporter** and
**rook** need it: the exporter dials with `verify=<cert>`, and the export's
`ceph-rgw-tls-cert` entry makes ocs-operator set the CephObjectStore's
`gateWay.SSLCertificateRef`. **NooBaa reads none of that.** Its S3 client trusts
exactly `/etc/ocp-injected-ca-bundle/ca-bundle.crt` and the service CA — the two
paths it logs on every reconcile — and the first of those is the ConfigMap the
Cluster Network Operator fills from the **cluster-wide** trusted CA bundle. A
`source.generated` gateway certificate is self-signed and reaches neither, so
`ReconcileDefaultBackingStore` fails its bucket `PUT` forever, NooBaa holds
`phase: Configuring`, and the StorageCluster can never leave `Progressing`.

The step now closes that gap: it reads `Proxy/cluster.spec.trustedCA`, merges the
staged gateway certificate into whatever ConfigMap that names (defaulting to
`openshift-config/user-ca-bundle`, and pointing the Proxy at it when unset), and
applies the result. It **merges** rather than replaces — the estate CA and the
mirror CA already in that bundle must survive — and skips when the certificate is
already present, so re-running is a no-op. `--force-conflicts` is deliberate:
`user-ca-bundle` is normally owned by the installer's field manager, and taking
ownership of a bundle we just read and preserved is the intended outcome.

An estate-issued gateway certificate needs none of this, because the estate CA is
already in the cluster's trust — that remains the better shape for production.

## `error setting modifier for [client.healthchecker] type=key ...: Malformed input [buffer:3]`

Seen in the `StorageCluster` status as `ExternalClusterConnecting=False`, reason
`ExternalClusterStateError`, wrapping `failed to get external ceph mon version`,
`monclient: keyring not found`, and `[errno 5] RADOS I/O error`. The
`StorageCluster` sits `Progressing` until the readiness budget expires.

This is a **cephx key-type skew, not a corrupted or truncated key**. Decode the
blob the error quotes: a cephx `CryptoKey` is a little-endian `u16` type, an
8-byte `utime_t`, a `u16` secret length, then the secret. A legacy key is
type 1 with a 16-byte secret — 28 bytes, 40 base64 chars, always starting `AQ`.
The failing keys are type 2 (`CEPH_CRYPTO_AES256KRB5`) with a 32-byte secret —
44 bytes, 60 base64 chars, starting `Ag`.

IBM Storage Ceph mints AES256KRB5 by default from build `20.2.1-297` onwards
(ceph-prd-01 runs `20.2.1-324.el9cp`). Data Foundation 4.21 bundles a Ceph
`20.2.0` client, whose keyring parser has no handler for type 2 and rejects it
outright before authentication is ever attempted — no client-side config can
recover it.

**The discriminator is the vendor build, NOT the version number.** Upstream Ceph
userspace — checked at the `tentacle` branch, the `v20.2.1` tag, and `main` —
implements only `CEPH_CRYPTO_NONE` and `CEPH_CRYPTO_AES`, and every key-minting
call site hardcodes `CEPH_CRYPTO_AES`; AES256KRB5 is a downstream/kernel
addition an upstream mon cannot produce. So community Ceph `20.2.2` attaches to
Data Foundation 4.21 fine (confirmed on a lab cluster 2026-07-31) while IBM's
`20.2.1-324.el9cp` does not, even though 20.2.2 is the higher number. Do not
chase the Ceph version when triaging this; ask whether the provider is an IBM
build. Two fingerprints, both absent upstream and present on IBM builds:
`ceph auth get-or-create -h` advertising `--key_type`, and
`ceph config help mon_auth_emergency_allowed_ciphers` resolving. The fastest
test on any cluster is `ceph auth get-key client.healthchecker | head -c 2` —
`AQ` parses, `Ag` does not.

Bootwright is a faithful pipe here: the exporter's stdout reaches the Secret
byte for byte. Since 2026-08-05 the exporter playbook refuses the export when
any entry's `data.userKey` does not start with `AQ`, naming the offending
entries, rather than attaching keys that cannot work.

**The mon-map cipher policy, verified on ceph-prd-01 (`20.2.1-324.el9cp`),
2026-08-05.** `ceph mon dump` carries three fields, and `ceph -h` shows exactly
one command that writes them — `ceph mon set <auth_service_cipher |
auth_allowed_ciphers | auth_preferred_cipher> <value>`:

```
"auth_service_cipher":   { "name": "aes256k", "value": 2 }
"auth_allowed_ciphers": [ { "name": "aes256k", "value": 2 } ]
"auth_preferred_cipher": { "name": "aes256k", "value": 2 }
```

`auth_allowed_ciphers` holding only `aes256k` is why
`ceph auth get-or-create ... --key_type aes` answers
`Error EINVAL: creating key with insecure key type ("aes") not allowed` — the
flag exists (note the **underscore**) and `aes` is the right value; it is
policy, not vocabulary, that refuses. These are **mon map** fields:
`ceph config set` cannot write them, and the only related config option,
`mon_auth_emergency_allowed_ciphers`, is mon-only, `Can update at runtime:
false`, and rejected by the config store — "is special and cannot be stored by
the mon". It is a startup-only lockout escape hatch, not fleet configuration.

`spec.ceph.security.cephx.keyType` declares this (see
`docs/advanced/ceph-topologies.md`). Bootwright renders `auth_allowed_ciphers`
then `auth_preferred_cipher`, in that order — a cipher cannot be preferred
before it is allowed — and **never** `auth_service_cipher`, because rotating the
monitors' own service cipher invalidates every client's service key. `aes`
widens the allowed set rather than replacing it, so keys in use keep working.

Declaring `aes` is a cluster-wide cephx downgrade, so the alternative remains
worth taking first: pair the cluster with a Data Foundation build whose client
parses AES256KRB5. Check the vendor support matrix — an ODF/FDF 4.21 consumer
against an IBM Storage Ceph 9.x provider may be outside it, making a version
realignment the real answer.

Other traps:

- `ceph auth rotate` re-mints with the build default. So does a bare
  `get-or-create` after `auth rm` — the type only changes with `--key_type`, or
  once `auth_preferred_cipher` has been moved.
- The exporter calls `get-or-create` and adopts an existing entity verbatim
  (`check_user_exist`), so re-running the export changes nothing on its own: a
  bad key is never re-minted away. This is why declaring a key type also makes
  the export step delete the mistyped `client.healthchecker` / `client.csi-*`
  entities first — a whitelist, so `client.admin`, mon, mgr and OSD keys are
  never in scope even though they carry the same `Ag` prefix.
- **`data.userKey` in the exporter's JSON does not always hold a cephx key.**
  Six entries carry the field; five are credentials, and
  `rook-ceph-dashboard-link` puts `ROOK_EXTERNAL_DASHBOARD_LINK` — a URL — in
  the same field (exporter `:1940-1944`). A key-shape check over every
  `userKey` therefore flags the dashboard link forever. Worse, it made the
  refusal cover a **superset** of what the repair deletes, so it could never be
  satisfied by the remedy it recommended. Both sides now derive from one
  pattern, `healthchecker|csi-`, matched against `data.userID` for the export
  and against the entity name for the deletion. `userID` also carries a `.N`
  generation suffix once keys have been rotated, so match it as a prefix.
- Never flip the type byte on an existing 44-byte blob: `CryptoAES` validates
  only `length >= 16`, so a 32-byte secret relabelled type 1 loads silently and
  uses the first 16 bytes — a silently wrong credential.
- **Two clusters exporting in the same apply race on the shared entities.** The
  deletion above is destructive and the entities are shared, so if cluster A
  remints its keys and cluster B's `ceph auth rm` then runs, A's Secret carries
  credentials Ceph no longer holds — a silent authentication failure with no
  `Malformed input` to explain it. The window is open only on the first apply
  after `keyType` is declared: once the keys carry the declared type, the stale
  list comes back empty and the second cluster adopts rather than deletes. The
  step now re-reads `ceph auth ls` after the export and refuses when any exported
  `userKey` is no longer one Ceph holds, naming the race and saying to rerun.
  Prevention rather than detection would need the orchestrator to serialize
  per-`StorageExport`, which is a larger change than the one-rerun repair earns.
- Bootwright passes no `--restricted-auth-permission`, so every attached
  OpenShift cluster shares the five entities (`client.healthchecker`,
  `client.csi-rbd-node`, `client.csi-rbd-provisioner`,
  `client.csi-cephfs-node`, `client.csi-cephfs-provisioner`, all at plain names
  with no generation suffix): one repair fixes all of them, one mistake breaks
  all of them.
