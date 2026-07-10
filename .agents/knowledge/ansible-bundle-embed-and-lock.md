# Embedded Ansible bundle: slimming, lock verification, determinism

**Exact pins only:** `ansible/collections/requirements.yml` lists trusted,
exact-pinned tarball URLs
(`galaxy.ansible.com/download/<name>-<version>.tar.gz`). When bumping any
pin, keep `requirements.lock.yml` aligned with the embedded collection
manifests or the build fails.

**Bundle slimming:** after `ansible-galaxy collection install`, the Makefile
`COLLECTIONS_STAMP` step deletes `tests`, `docs`, `examples`, `changelogs`,
`.github`, `.azure-pipelines`, and `ci` trees from the installed collections
before packing — they bloat the embedded binary without contributing to
runtime module execution. Collections plus the authored `ansible/` tree are
packed into `internal/converge/bundle/ansible_bundle.zip` so disconnected
hosts never reach Galaxy at runtime.

**Two-layer supply-chain defence:**

1. `scripts/verify-ansible-collections.py` runs in the same Makefile step,
   right after install: it compares each installed collection's
   `MANIFEST.json` `file_manifest_file.chksum_sha256` against the lock's
   `filesManifestSha256` plus the pinned version, failing closed — a
   tampered or unexpected download fails the build the moment it is
   unpacked, before the bundle is packed and embedded.
2. `TestAnsibleCollectionLockMatchesEmbeddedManifest`
   (`internal/repo/bundlecheck`, run by `make sync-bundle`) checks the same
   value on the shipped artifact as a second line of defence.

The verify script is stdlib-only (no PyYAML) by design: the lock is a small
machine-generated file with a fixed shape, so a line-oriented parser avoids
the dependency and runs anywhere `make python-test` does.

**Byte-deterministic zip:** `scripts/sync-ansible-bundle.py` writes fixed
`ZIP_EPOCH` (1980) timestamps, sorted entry order, and permissions
normalized to 0644/0755 so bundle-equality tests and rebuilds are stable.

**Symlink policy:** an authored symlink anywhere under `ansible/` raises
`BundleError` `refusing to bundle symlink` (fail closed), while symlinks
inside the downloaded collections tree are skipped WITHOUT following their
targets so no out-of-tree file can leak into the bundle. Both behaviors are
pinned by `scripts/test_sync_ansible_bundle.py`.
