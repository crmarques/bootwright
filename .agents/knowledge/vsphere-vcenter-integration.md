# vSphere / vCenter integration semantics

**Preflight session probe:** `vsphereVCenterChecks` probes every declared
vCenter with a REST session login (`POST https://<server>:<port>/api/session`
with basic auth from the resolved `user:password` material) so unreachable
endpoints and bad credentials surface before apply instead of mid-convergence.
HTTP `401` is classified as a credentials failure with a
`bootwright secret set --name <credentialsRef>` remediation. Deduplication is
on the full connection identity `server|port|credentialsRef.Name` — a second
provider declaring the same server with different credentials (or another
port) is a distinct session to probe, so a bad second credential still
surfaces at preflight. Unreadable credential material only downgrades the
probe to WARN, because the secret-material checks already fail loudly. A
vCenter with `disableCertificateVerification` set gets a separate WARN naming
that basic-auth credentials transit unverified TLS, mirroring the BMC two-leg
posture.

**Session cleanup:** the probe deletes the session it just opened
(`DELETE /api/session` with the `vmware-api-session-id` header) so each
preflight run does not leak a live authenticated vCenter login until it idles
out. Cleanup is best-effort — a missing session id or failed DELETE never
fails the check. `vsphereSessionID` prefers the `vmware-api-session-id`
response header and falls back to the quoted JSON body vCenter returns from
`POST /api/session`. A failed (`401`) probe must not attempt a release.

**Probe timeout:** `preflightHTTPProbeTimeout` (15s) bounds every preflight
endpoint probe — the vCenter session login and the package-source /
install-source reachability probes — so an unresponsive endpoint fails the
check instead of hanging the run. `preflightHTTPDo` builds the real client
with this timeout when no `Deps.HTTPDo` fake is injected; only vSphere's
`disableCertificateVerification` opts a probe out of TLS verification.

**Manual MAC range:** vCenter rejects a VM create whose NIC carries a
manually assigned MAC outside the vCenter manual range
`00:50:56:00:00:00`–`00:50:56:3f:ff:ff`. Bootwright's generated MACs are
masked into this range in `internal/render/installer/mac.go`, so an authored
out-of-range MAC on a vSphere machine is rejected at validate rather than
failing mid-apply behind a `no_log` vmware module error. The range check must
not be applied to bare-metal providers.
