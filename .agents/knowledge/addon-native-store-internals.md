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
no leading dot). `Environment.spec.resources` is an authored-file allow-list,
but this generated dependency must not disappear from its own context. The
loader therefore auto-selects only the exact
`add-ons/_store/<name>/add-on.yaml` whose sibling marker is a regular file,
whose marker name is `<name>`, and whose only Bootwright object is
`ClusterAddon/<name>`. The supporting manifests remain path-addressed content;
other Bootwright YAML, unmarked directories, mismatched identities, and
multi-object descriptors receive no exception.

**Trap: the catalog reaches a run through two copies, and upgrading refreshes
neither.** The embedded catalog (`add-ons/embed.go`) is compiled into the
binary, but a run reads the CONTEXT SNAPSHOT at
`<context>/input/add-ons/_store/<name>/`, which was copied from the
machine-local store, which `add-ons add` wrote from whatever binary was
current then. `make build` refreshes neither, and `apply` repeats neither — so
a playbook fix that lands in the repo runs nowhere until BOTH
`bootwright add-ons add` and `bootwright context update` are re-run. Observed
2026-08-05 on ceph-prd-01: a Data Foundation exporter fix was merged, the
binary rebuilt, `apply` re-run, and the failure repeated byte-for-byte because
the snapshot predated `2ac12110` — diagnosable only because the Ansible error's
`Origin:` line named line 96 while the repo's assert sat at line 122 (the
26-line `_admin` probe that commit added accounts for the difference exactly).
`validateAddonCatalogCopies` now refuses this: for every ClusterAddon whose
directory carries a marker it compares `DirDigest(dir)` against the marker
(edited/half-written copy) and `ReleaseDigest(marker.Name, marker.Version)`
against the embedded catalog (copy predating this build), naming both refresh
commands. A marker-less directory is an authored add-on and is never judged —
that is what keeps a deliberately customized copy sharing a catalog name legal.
A retired catalog version reports its own finding rather than a digest
mismatch nobody can act on.

**Fallback resolution:** `state/desired resolveRegisteredAddons` resolves
binding/profile `addonRefs` that no authored ClusterAddon matches against the
store; the registered directory loads like an authored add-on dir
(`SourcePath` anchors its shipped playbooks/manifests). After context init
snapshots it, the in-tree copy resolves the reference and the store is NOT
consulted. An absent or unreadable store (a rootless run cannot traverse the
root-owned Bootwright dir) falls through to the normal unresolved-reference
validation error, with a register/run-as-root remedy hint appended only when
the native catalog ships the referenced name.
