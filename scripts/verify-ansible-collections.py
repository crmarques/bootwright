#!/usr/bin/env python3
"""Verify installed Ansible collections against requirements.lock.yml.

Runs from the COLLECTIONS_STAMP make step, right after ``ansible-galaxy
collection install``, so a tampered or unexpected download fails the build the
moment it is unpacked -- before the bundle is packed and embedded, rather than
only afterwards. For each locked collection it compares the installed
``MANIFEST.json`` ``file_manifest_file.chksum_sha256`` against the lock's
``filesManifestSha256`` (the same value the post-pack Go test checks) and
confirms the installed version. Fails closed on any mismatch or missing file.

Stdlib only -- no PyYAML dependency -- so it runs anywhere ``make python-test``
does. The lock is a small machine-generated file with a fixed shape, so a
line-oriented parser is sufficient and avoids the dependency.
"""
from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

_LOCK_FIELDS = ("version", "source", "filesManifestSha256")


def parse_lock(text):
    """Parse the collections list out of requirements.lock.yml without PyYAML."""
    entries = []
    current = None
    for raw in text.splitlines():
        stripped = raw.strip()
        if stripped.startswith("- name:"):
            if current is not None:
                entries.append(current)
            current = {"name": stripped.split(":", 1)[1].strip()}
        elif current is not None and any(
            stripped.startswith(field + ":") for field in _LOCK_FIELDS
        ):
            key, value = stripped.split(":", 1)
            current[key.strip()] = value.strip()
    if current is not None:
        entries.append(current)
    return entries


def verify(collections_dir, lock_entries):
    """Return a list of human-readable integrity errors (empty == all match)."""
    errors = []
    for entry in lock_entries:
        name = entry.get("name", "")
        if "." not in name:
            errors.append(f"lock entry has invalid name {name!r}")
            continue
        namespace, collection = name.split(".", 1)
        manifest_path = (
            Path(collections_dir)
            / "ansible_collections"
            / namespace
            / collection
            / "MANIFEST.json"
        )
        if not manifest_path.is_file():
            errors.append(f"{name}: missing {manifest_path}")
            continue
        try:
            manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
        except (OSError, ValueError) as err:
            errors.append(f"{name}: cannot read {manifest_path}: {err}")
            continue
        info = manifest.get("collection_info", {})
        file_manifest = manifest.get("file_manifest_file", {})
        want_version = entry.get("version", "")
        if info.get("version") != want_version:
            errors.append(
                f"{name}: installed version {info.get('version')!r}, "
                f"lock pins {want_version!r}"
            )
        want_sha = entry.get("filesManifestSha256", "")
        if (
            file_manifest.get("chksum_type") != "sha256"
            or file_manifest.get("chksum_sha256") != want_sha
        ):
            errors.append(
                f"{name}: file manifest checksum "
                f"{file_manifest.get('chksum_type')}:{file_manifest.get('chksum_sha256')}, "
                f"lock pins sha256:{want_sha}"
            )
    return errors


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--collections",
        required=True,
        help="installed collections root (the directory containing ansible_collections/)",
    )
    parser.add_argument("--lock", required=True, help="path to requirements.lock.yml")
    args = parser.parse_args(argv)

    lock_entries = parse_lock(Path(args.lock).read_text(encoding="utf-8"))
    if not lock_entries:
        print(
            "verify-ansible-collections: no entries parsed from lock", file=sys.stderr
        )
        return 1

    errors = verify(args.collections, lock_entries)
    if errors:
        print(
            "verify-ansible-collections: collection integrity check FAILED:",
            file=sys.stderr,
        )
        for error in errors:
            print(f"  - {error}", file=sys.stderr)
        return 1

    print(
        f"verify-ansible-collections: {len(lock_entries)} collection(s) "
        "match requirements.lock.yml"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
