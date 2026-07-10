# Portable render secret placeholders and leak invariants

**Sentinel:** `PlaceholderSecretsDir` (`internal/secrets/paths.go`) is a
NUL-wrapped sentinel secrets-directory value
(`"\x00bootwright-placeholder-secrets\x00"` — it can never collide with a real
path or be created on disk) that switches `ResolveMaterialPath` and every
`Resolve*` wrapper into placeholder mode: they return a portable
`{{ secret <name>[.<suffix>] }}` token instead of a filesystem path. The
primary role has no suffix; typed roles use their role string (`tls-key`,
`ssh-public`, `ssh-private`); `username`/`password` suffixes exist for split
credentials. The context-free render (`bootwright render --input-dir`) threads
it through the inventory `PathOptions` so the bundle carries no context-bound
paths and no secret material.

**Two invariants:** it is render-only — `NewResolver`, `ReadMaterial`, and
`MaterializeForContext` must never be invoked with it — and the placeholder
check short-circuits BEFORE the context/external-source split, so a portable
bundle never leaks the operator's local source-file path either (pinned by
`TestResolveMaterialPathPlaceholderBypassesExternalSource`).

**Whole-tree leak blocklist:** a portable bundle is scanned against
`portableForbidden` (`internal/render/portable_test.go`): the NUL-wrapped
placeholder sentinel itself, the context `<bootwright-...>` placeholder
dialect, real PEM material, and the context-only Ansible extra-var
`{{ bootwright_clusters_dir }}` (a path no context-free consumer can resolve).
Rendered context output has its own leak markers: installer placeholders must
carry `bootwright-secret-ref` markers and never PEM keys or base64-style auth
values.

**Two placeholder dialects in manifests:** manifest `Secret` rendering
(`internal/render/installer/installer_manifests.go`) must recognize BOTH
dialects — the context placeholder render's `<bootwright-...-ref:>` sentinels
and the portable render's `{{ secret ... }}` tokens — so a redacted cert/key
lands in `stringData` verbatim rather than the base64 `data` block, which
would base64-encode and thereby corrupt the token.

**The managed SSH trust store has no portable form:** it is a context artifact,
not a named secret, so in placeholder mode (`secret.IsPlaceholderSecretsDir`)
`DirForSecrets`/`StorePathForSecrets`/`KnownHostsPathForSecrets`
(`internal/sshtrust/sshtrust.go`) return `""` and callers must omit the managed
`known_hosts`/trust-dir from the rendered inventory entirely rather than emit a
path derived from the sentinel. An explicit `spec.access.ssh.knownHostsRef`
secret still tokenizes normally via `ResolveMaterialPath`. Pinned by
`placeholder_test.go`.
