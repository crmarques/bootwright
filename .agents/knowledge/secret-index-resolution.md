# Secret material resolution: the secret.Index seam

**Seam:** `secret.Index` (`internal/secrets/index.go`) is the single seam
between desired-state `Secret` declarations and path resolution. For each
secret name it answers whether material lives in the per-context store or an
operator file and, for file sources, the on-disk path per `MaterialRole`. The
resolver and every render/preflight consumer depend on `Index`, not on where
secrets happen to be declared, and `NewIndex` never reads material bytes.

**Source arms:** `armContext` (a `contextStore` source or an omitted source),
`armFile` (operator files), `armGenerated` (minted into the store).
`spec.type` fixes which file-source keys populate which role:
`tlsCertificate`: `cert` → primary, `key` → tls-key; `sshKeyPair`:
`privateKey` → primary/ssh-private, `publicKey` (or `<privateKey>.pub` when
omitted) → ssh-public; every other type: `path` → primary, with the primary
file also serving the zero/unknown role.

**Store routing (`useContextPath`):** `contextStore` and `generated` secrets
always resolve to the context store; `Environment.spec.secretStorage.mode:
context` forces `file:`-sourced secrets into the store too.

**Verify-or-renew:** `MaterializeOptions.Renew` regenerates every generated
secret even when present material already matches the desired spec. Without
`Renew`, existing material is verified against the desired spec
(`VerifySelfSignedCertificateBytesMatchRequest`,
`VerifySSHKeyPairPublicBytesMatchRequest`, username match for credentials); a
mismatch or a partially-present pair is an error telling the operator to run
`bootwright secret generate --renew`. `GeneratedSelfSignedRequests` is
deliberately the shared derivation: preflight's generated-self-signed drift
checks (see `self-signed-cert-drift.md`) consume the same request list that
materialization applies, so the two can never disagree about what a declared
generated certificate should look like.

**Encrypted drift inspection:** Generated certificate files are AES-GCM
envelopes, not plaintext PEM. A preflight drift check that reads the resolved
path with `os.ReadFile` parses the envelope JSON as a certificate and reports
`certificate is not PEM-encoded` even immediately after a successful
`secret generate --renew`. Generated-certificate drift inspection must read
the primary material through the context store, then pass the decrypted bytes
to `VerifySelfSignedCertificateBytesMatchRequest`.

**ECDSA SSH public-key encoding:** in `ecdsaSSHKeyPairPEM`
(`internal/secrets/ssh_key.go`), `ecdh.PublicKey.Bytes()` returns the SEC1
uncompressed point encoding (`0x04 || X || Y`), byte-identical to the
deprecated `elliptic.Marshal` — which is why the ECDSA→ECDH conversion is a
safe replacement for the old marshaling when building the SSH public-key blob.
