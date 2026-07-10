# Artifact-server TLS: certificate rotation and directive rendering

## Stale SAN list after editing tls.dnsNames/ipAddresses/commonName

**Symptom:** clients fail hostname verification against the managed artifact
server even though the desired `tls.dnsNames`/`tls.ipAddresses`/
`tls.commonName` were updated and apply succeeded.

**Root cause:** the OpenSSL request config used to be rendered only when
certificate material was absent, so an edited SAN list kept serving the old
certificate.

**Fix:** `artifacts-openssl.cnf.j2` (SANs/CN) renders UNCONDITIONALLY on
every apply so a change to the desired names surfaces as a template content
change. Certificate generation preserves existing material unless it is
absent OR `bootwright_artifacts_tls_openssl_cnf is changed`, which rotates
the certificate; the container recreates on config or TLS changes. (Distinct
from `self-signed-cert-drift.md`, which covers `bootwright secret generate`
material.)

## ssl_ciphers is a verbatim render — injection guarded

**Constraint:** `artifactServer` `tls.ciphers` renders VERBATIM into the
nginx `ssl_ciphers` directive, so validation rejects any value that could
terminate the directive or open a new one (config-injection guard).

## TLS relaxation renders only when set

**Constraint:** `ssl_protocols` and `ssl_ciphers` directives render only
when the component sets `bootwright_component.tls.protocols` /
`tls.ciphers` (guarded by `| default('')` length checks in
`artifacts-nginx.conf.j2`); the default keeps the server's built-in TLS
configuration. The relaxation exists for legacy Redfish BMC virtual-media
clients (e.g. Huawei iBMC) that cannot negotiate modern defaults and abort
the InsertMedia fetch with a generic connection failure — `minVersion`
renders an `ssl_protocols` span from it up to TLSv1.3 (`tlsVersionsAscending`),
and `ciphers` values like `DEFAULT:@SECLEVEL=0` admit legacy ciphers without
weakening every deployment.
