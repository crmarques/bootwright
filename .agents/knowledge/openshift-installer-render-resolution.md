# Installer render: mirror host resolution and secret placeholders

**Constraint (mirror registry host resolution):** `mirrorRegistryHost` in
`internal/render/installer/installer_helpers.go` resolves the cluster-facing
host of the managed mirror registry by preferring a declared `endpoints[]`
address on the registry machine (parity with `artifactServer`; a named
endpoint wins, otherwise a sole endpoint) or an explicit, routable bind
address. Only when nothing is declared does it fall back to the machine's
SSH/route address.

**Why:** A bastion reached through a loopback SSH alias would otherwise
silently resolve the mirror to the network gateway — the rendered
`install-config.yaml` mirror entries would point cluster nodes at an address
that is not the registry.

**Constraint (both placeholder dialects must be recognized):** The installer
manifest render (`internal/render/installer/installer_manifests.go`)
recognizes two placeholder dialects when deciding whether a redacted cert/key
goes into a manifest's `stringData` (verbatim) rather than the base64 `data`
block: the context placeholder render's `<bootwright-<kind>-ref:<name>>`
sentinels and the portable (`render --input-dir`) render's `{{ secret ... }}`
tokens (`strings.HasPrefix(value, "<bootwright-") || strings.HasPrefix(value,
"{{ secret ")`). Base64-encoding a placeholder token would corrupt it — the
substitution pass later cannot find or replace it. Any new placeholder
dialect must be added to this recognition, or portable renders emit corrupted
secret manifests.

**Semantics (pull-secret placeholder stays parseable JSON):** The placeholder
`install-config.yaml` encodes the pull-secret's SecretRef name as the
auths-key of a JSON pull-secret, so the placeholder file is still parseable
JSON — tools (and openshift-install's own validation of structure) can read
it before secret material is resolved. `InstallerConfig` renders the
placeholder form; `InstallerConfigWithSecrets` is the resolved form passed
straight to `openshift-install`.
