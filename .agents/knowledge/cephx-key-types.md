# CephX key types and the Data Foundation client

Which key types a Ceph cluster mints, which clients parse them, and how a type
mismatch shows up when a Data Foundation consumer attaches. The Ceph side of the
attach is [B86](../../specs/milestones/m4.md#b86) and the package
[B85](../../specs/milestones/m4.md#b85). The admission of a declared key type is
M2's [B628](../../specs/milestones/m2.md#b628), the root of the Data Foundation
client question. The attach is in
[data-foundation-external-mode.md](data-foundation-external-mode.md).

Provenance: `R3:C<n>` is a claim id of the M4 definition's research of
2026-10-20 (Fusion Data Foundation) and `RV-R3` its adversarial review, with the
verdict (confirmed, corrected or uncertain). Build labels name where a fact was
read or observed; they are not supported or required versions, and no version
rule is recorded (D163). The ids name research notes this repository does not
track; each statement here carries its fact, and an id only records where it was
read. "Observed by the v1 implementation (main-v1); re-verify on this tree"
marks a v1 fact the research did not confirm; the v1 lesson ids `D-nn` name
untracked review notes of that branch.

## What the research confirmed

- The exporter takes `--cephx-key-type {aes,aes256k}` and passes it as
  `key_type` in the `auth get-or-create` monitor command. If keys already exist
  the flag is a no-op. With no flag Ceph uses its configured preferred type. The
  script recommends `aes256k` when the Linux kernel on the Rook nodes is 7.0 or
  later. `--cephx-key-rotate rotate|revert` creates entities with a
  `.<generation>` suffix (RV-R3 C32, confirmed).
- Rook's cephx documentation says Ceph supports key rotation and the AES256K
  type from Squid 19.2.6, Tentacle 20.2.4 and Umbrella 21.2.0 (as read), keeps
  `aes` for clients and kernels that do not support the other type, and says to
  keep the CSI key type `aes` until every Kubernetes node runs a Linux kernel of
  7.0 or later, or 7.2 with FIPS enforcing (RV-R3 C32, confirmed).
- OSSN-0108 describes `aes256k` as AES256-CTS-HMAC-SHA384-192 (RFC 8009). `ceph
  mon dump` filtered for `allowed_ciphers` shows `auth_allowed_ciphers aes,
  aes256k` when both are accepted. The kernel RBD and CephFS clients need Linux
  7.0 or later; `librbd`, `ceph-fuse` and NFS-Ganesha do not (RV-R3 C32,
  confirmed).
- The external CephCluster that `ocs-operator` builds sets no cephx block; that
  belongs to internal mode (RV-R3 C32, confirmed in `newExternalCephCluster`).
- The exporter's `get-or-create` adopts an existing entity and returns its
  stored key, so a re-run never re-mints a bad key and never repairs one
  (R3:C23 and C55, confirmed).

## What no public source confirmed

R3:C54 could not confirm these three claims, which come from v1:

- the Data Foundation client of the day rejects type 2 keys before
  authentication;
- some IBM Storage Ceph builds mint type 2 keys by default;
- an IBM build reports only `aes256k` in `auth_allowed_ciphers`.

The evidence-based statement is narrower: upstream Ceph from the releases above
can create `aes256k` keys, and the Rook and ODF scripts expose `aes` and
`aes256k` selection. The guard "refuse a key whose type is not 1" and
`spec.security.cephx.keyType: aes` are Bootwright-side constructs, not an
ODF contract.

## Observed by v1 on one IBM build

Observed by the v1 implementation (main-v1) on IBM Storage Ceph
`20.2.1-324.el9cp`; re-verify on this tree (v1 lessons D-06 and D-07). The
discriminator was the vendor build, not the version number:

- A cephx `CryptoKey` is a little-endian `u16` type, an 8-byte `utime_t`, a
  `u16` secret length and the secret. Type 1 (AES, 16-byte secret) is 28 bytes
  and 40 base64 characters starting `AQ`. Type 2 (`CEPH_CRYPTO_AES256KRB5`,
  32-byte secret) is 44 bytes and 60 characters starting `Ag`.
- That build minted type 2 keys by default (v1 records "from build
  20.2.1-297 onwards"), and the Data Foundation 4.21 client (Ceph 20.2.0
  libraries) had no handler for type 2 and rejected it before authentication.
  Upstream Ceph implemented only NONE and AES in the releases v1 read, and a
  community 20.2.2 cluster attached.
- The StorageCluster showed `ExternalClusterConnecting=False` with reason
  `ExternalClusterStateError`, and messages `Malformed input [buffer:3]`,
  `failed to get external ceph mon version`, `monclient: keyring not found` and
  `[errno 5] RADOS I/O error`; the Rook log read `error setting modifier for
  [client.healthchecker] type=key`. The StorageCluster stayed `Progressing`.
- Fingerprints: `ceph auth get-or-create -h` advertises `--key_type`;
  `ceph config help mon_auth_emergency_allowed_ciphers` resolves; the fastest
  test is `ceph auth get-key client.healthchecker | head -c 2`, where `AQ`
  parses and `Ag` does not. The `rook-ceph-dashboard-link` entry puts a URL in
  `userKey`, so a key-prefix test skips it.
- `ceph mon dump` showed `auth_service_cipher`, `auth_allowed_ciphers` and
  `auth_preferred_cipher`, all `aes256k` (value 2). With only `aes256k`
  allowed, `ceph auth get-or-create ... --key_type aes` failed with `Error
  EINVAL: creating key with insecure key type ("aes") not allowed`. The three
  fields are mon-map fields written only by `ceph mon set <field> <value>`;
  `ceph config set` cannot write them, and `mon_auth_emergency_allowed_ciphers`
  is monitor-only and startup-only.
- To allow `aes`, v1 widened `auth_allowed_ciphers` to `aes,aes256k` and then
  made `aes` preferred, writing `auth_allowed_ciphers` before
  `auth_preferred_cipher` and never touching `auth_service_cipher`, because
  rotating the service cipher invalidates every client's service key. v1's own
  documents call this "a cluster-wide security downgrade", since every client
  then gets AES-128 keys.
- Traps: `ceph auth rotate` re-mints with the build default, and a bare
  `get-or-create` after `auth rm` does too. The type changes only with
  `--key_type` or once `auth_preferred_cipher` has moved; policy alone does not
  rewrite existing keys.
- Never relabel a type word. `CryptoAES` validates only that the secret is at
  least 16 bytes, so a 32-byte secret relabelled type 1 loads and silently uses
  its first 16 bytes.
- Shared exporter identities made a type repair race across consumers: one
  cluster's delete and re-mint followed by another's `auth rm` left a Secret
  holding keys Ceph no longer had, with no `Malformed input` to explain it
  (D-08). The restricted-authentication entities the exporter offers are the
  nearest native mechanism, and it creates no per-consumer RGW user (R3:C54).

## To re-verify

R3's open question O6 gives the commands: the node kernel (`oc debug node/<node>
-- chroot /host uname -r`) and, on Ceph, `ceph mon dump | grep -i cipher`,
`ceph config get mon auth_preferred_cipher` and `ceph auth get-or-create -h`.
[B761](../../specs/milestones/m4.md#b761) and B628 own the live answers.
