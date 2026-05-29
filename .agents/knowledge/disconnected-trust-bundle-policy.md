# Disconnected install: TLS fails, agent never reaches SSH

**Symptom:** A disconnected OpenShift install with a self-signed mirror registry fails with x509 errors on image pulls. The RHCOS node boots but the agent never connects back via SSH.

**Root cause:** `additionalTrustBundlePolicy` defaults to `Proxyonly`. That policy only injects the CA into nodes when a proxy is also configured. A disconnected install with no proxy but a self-signed mirror certificate never gets the CA propagated into the booting node — image pulls from the local mirror fail TLS and the agent process dies before it can call home.

**Fix:** Always set `additionalTrustBundlePolicy: Always` when `additionalTrustBundle` is present. See `InstallerConfig` in `internal/render/installer.go`.

**Invariant:** Bootwright unconditionally sets `Always` whenever any environment,
mirror, or install trust reference renders `additionalTrustBundle`.
