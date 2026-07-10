# Native add-on store: marker file, rewrite-on-install, snapshot mechanics

**Store shape:** The registered add-ons store is the Bootwright root's
`add-ons` dir (`/var/lib/bootwright`), a sibling of `contexts/` and `media/`,
machine-local like managed media: a dir-listing store with no registry file,
holding at most one registered version per add-on name — bindings reference
add-ons by name only, so re-registering a different version is the upgrade
path. `EntryVersion.Channel`/`Notes` are informational for `add-ons list`
output only, never substituted into content.

**Marker file:** The provenance marker written into each registered add-on
directory is named `.bootwright-addon` and is extensionless on purpose: the
desired-state loader decodes YAML files by extension, and the marker must
never be picked up and decoded as desired state.

**Install/remove semantics:** Install rewrites the whole directory
(`os.RemoveAll` first) so no stale file of the prior version survives, and
rejects embedded file paths that escape the add-on directory.
`InstalledAddons` skips directories without a marker (not
Bootwright-registered) and reports Modified when the recomputed content digest
differs from the marker's. `Remove` refuses a directory carrying no marker:
`was not registered by bootwright add-ons add; remove it manually`.

**Version is an assertion:** `add-ons delete` accepts the same
`<name>:<version>` shorthand that `add` teaches, but the store holds exactly
one version per name, so the version part is an ASSERTION, not a selector: a
mismatch fails with `registered at version X, not Y`. The colon shorthand and
an explicit `--version` flag are mutually exclusive on `add`, and
re-registering an existing add-on requires confirmation.

**Context snapshot:** `nativecatalog.ReferencedStoreAddons` detects
ClusterAddons resolved from the store by checking whether their `SourcePath`
lies under `StoreDir()`, mapping name → store directory so context
init/update can snapshot each referenced registered add-on into the context
input tree. `workspace.ReplaceInputDirWithAddons` copies them under
`add-ons/_store/<name>` inside the same staging tree, keeping the
all-or-nothing swap — the context stays self-contained even if the store
entry is later deleted or upgraded. Add-on names come from validated
ClusterAddon `metadata.name` and must be plain path segments (no separators,
no leading dot).

**Fallback resolution:** `state/desired resolveRegisteredAddons` resolves
binding/profile `addonRefs` that no authored ClusterAddon matches against the
store; the registered directory loads like an authored add-on dir
(`SourcePath` anchors its shipped playbooks/manifests). After context init
snapshots it, the in-tree copy resolves the reference and the store is NOT
consulted. An absent or unreadable store (a rootless run cannot traverse the
root-owned Bootwright dir) falls through to the normal unresolved-reference
validation error, with a register/run-as-root remedy hint appended only when
the native catalog ships the referenced name.
