# Redfish BMC certificate-import mechanics

The operator-facing two-leg TLS model (`bmc.tls` vs `bmc.virtualMedia.tls.{trust,
restoreVerificationAfterBoot,removeCertificateAfterBoot}`) is documented in
docs/concepts/machines.md and docs/troubleshooting.md. The import path below runs
only with `trust: import-certificate`; `trust: established` performs no BMC
security writes at all. This file records the non-obvious mechanics inside
`container_cluster_boot_redfish/tasks/media/`.

**Discovery-then-dispatch:** `import_certificate.yml` discovers which trust-store
mechanism the BMC exposes, then dispatches to `import_certificate/<method>.yml`:

- `standard` — a DMTF Certificates collection on the VirtualMedia member.
- `security_service` — the Redfish manager SecurityService OEM action
  `#SecurityService.ImportRemoteHttpsServerRootCA` (Huawei/xFusion iBMC and
  compatible BMCs).

Adding another OEM mechanism is a new method name plus sibling
`import_certificate/<method>.yml` and `remove_certificate/<method>.yml` files —
no vendor branch in the shared flow. `cert_method` is a discovered capability,
not a configured vendor. When the BMC exposes neither mechanism an assert fails
with a clear message; fall back to `trust: disable-verification` or a BMC-trusted
artifact server certificate with `trust: established`.

**iBMC RootCertId slots are 5..8:** the remote-HTTPS-server root-CA store
accepts `RootCertId` 5 through 8 only (not 1..8); values outside that range are
rejected. Bootwright pins `bootwright_redfish_security_service_root_cert_id: 8`
— a fixed slot makes removal deterministic and re-import overwrite rather than
accumulate, and the top slot avoids colliding with operator-managed
certificates.

**RootCertId and Usage are mutually exclusive:** the import action takes them as
alternative forms (RootCertId writes a specific slot; Usage auto-allocates one).
Sending both is non-conformant and the iBMC rejects the pair with HTTP 403.
Send RootCertId alone.

**Leaf retrieval uses `openssl s_client`, not community.crypto.get_certificate:**
that module needs the `cryptography` Python library on the executing host, and
these tasks run on the controller driving the BMCs (the bare-metal nodes are not
provisioned yet) — a host not guaranteed to ship it; openssl is already a
Bootwright dependency (the artifact server role installs it to mint the very
certificate being fetched). Empty stdin makes s_client return as soon as the
certificate is presented; `timeout` bounds a TCP-open-but-unresponsive endpoint.
s_client exits non-zero on a self-signed/untrusted cert (and on a `timeout`
kill) but still prints the certificate — treat the run as successful whenever
`END CERTIFICATE` appears in stdout, and fail only when a non-zero exit left no
certificate to import.

**Surfacing refusals under no_log:** the import/PATCH uri tasks protect
`url_password` with `no_log`, which buries the response body ("output has been
hidden due to no_log"), and a `status_code` mismatch raises only the bare
`Status code was 403 ...` with no BMC detail. Pattern: set `failed_when: false`
on the uri call, then follow with a credential-safe `assert` that interpolates
only status/json/content/msg and the resource path — never `url_password` — so
the BMC's own refusal message is visible. On the VerifyCertificate disable
PATCH specifically: tolerate 501/400/405 (a BMC that keeps the property
read-only, e.g. Huawei/xFusion iBMC 501s it), but re-surface 401/403 — the
credentials authenticated yet the account's BMC role lacks the privilege the
write needs; swallowing it hides the real problem.

**Capture VerifyCertificate once per boot:** the pre-PATCH probe reflects the
live value, which the same attempt then PATCHes to false. Recapturing on a
retry records the already-disabled value, so the role's always-block restore is
skipped and BMC certificate verification stays permanently OFF after a
fail-then-retry. Capture only on the first attempt; the value is stable across
retries.

**Cleanup and restore live in the role's always block:** `remove_certificate.yml`
dispatches to the same discovered method the import used, gated on the import
having recorded a handle (Certificates-collection ref, or the RootCertId slot)
and tolerant of 400/404 (already gone), so it is a no-op when the import was
skipped or failed. The VerifyCertificate / HttpsTransferCertVerification
restore tasks must warn, never fail — a hard failure there would mask the
original boot error — and must not be no_log'd: a no_log debug would censor the
very warning telling the operator verification may still be off.
