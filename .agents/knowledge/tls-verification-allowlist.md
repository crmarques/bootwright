# TLS verification opt-out policy and trusted-download patterns

**Allowlist (L14 audit policy):** an unconditional `validate_certs: false`
(TLS verification disabled with no operator opt-in) is confined to a
two-entry allowlist — the staged agent-ISO reachability probes in
`container_cluster_agent_install/tasks/iso/publish_target.yml` and the
artifact-server readiness wait in
`infra_component_artifact_server_http/tasks/main.yml` — both probing
Bootwright's own managed self-signed listeners. Every other site must drive
`validate_certs` from a templated operator-gated expression
(`bmc`/`redfish.validateCerts`, vsphere `disableCertificateVerification`).
The allowlist in `internal/repo/checks/audit_regression_test.go` is enforced
two-way: new literal-false sites fail, and stale allowlist entries must be
dropped so the guard stays honest.

**virtctl download trust (SSL_CERT_FILE pattern):** the virtctl
ConsoleCLIDownload route is served by the host cluster's default ingress
router, whose wildcard cert is signed by the cluster ingress CA — absent
from the controller trust store. Since the host kubeconfig is already held,
the download is verified against that cluster's published ingress CA instead
of skipping TLS. Mechanism: `get_url` has no CA-file parameter, but its SSL
context loads system trust plus whatever `SSL_CERT_FILE` points at, so the
task env is rebuilt explicitly (proxy env + `SSL_CERT_FILE`) rather than
relying on play/task environment merge semantics. The optional disconnected
override `bootwright_virtctl_mirror_base` instead downloads
`<base>/<server-version>/virtctl-linux-amd64.tar.gz` verified against the
controller trust store (an internally trusted host, so no ingress CA).
