#!/usr/bin/env python3
"""Build the embedded Ansible bundle archive deterministically."""

from __future__ import annotations

import argparse
import os
from pathlib import Path
import stat
import zipfile


ZIP_EPOCH = (1980, 1, 1, 0, 0, 0)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--source", required=True, type=Path)
    parser.add_argument("--collections", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()

    files: dict[str, Path] = {}
    add_tree(files, args.source, Path())
    if args.collections.exists():
        add_tree(files, args.collections, Path("collections"))

    args.output.parent.mkdir(parents=True, exist_ok=True)
    tmp = args.output.with_name(args.output.name + ".tmp")
    if tmp.exists():
        tmp.unlink()
    with zipfile.ZipFile(tmp, "w", compression=zipfile.ZIP_DEFLATED) as archive:
        for archive_name in sorted(files):
            write_file(archive, archive_name, files[archive_name])
    os.replace(tmp, args.output)
    return 0


def add_tree(files: dict[str, Path], root: Path, prefix: Path) -> None:
    for path in sorted(root.rglob("*")):
        rel = path.relative_to(root)
        if should_skip(rel, path):
            continue
        if path.is_dir():
            continue
        archive_name = (prefix / rel).as_posix()
        files[archive_name] = path


def should_skip(rel: Path, path: Path) -> bool:
    parts = rel.parts
    if "__pycache__" in parts:
        return True
    if rel.name == ".stamp":
        return True
    if path.is_file() and rel.name.startswith("test_") and rel.suffix == ".py":
        return True
    if path.is_file() and rel.suffix == ".pyc":
        return True
    return False


def write_file(archive: zipfile.ZipFile, archive_name: str, source: Path) -> None:
    mode = stat.S_IMODE(source.stat().st_mode)
    perms = 0o755 if mode & 0o111 else 0o644
    info = zipfile.ZipInfo(archive_name, ZIP_EPOCH)
    info.compress_type = zipfile.ZIP_DEFLATED
    info.external_attr = (stat.S_IFREG | perms) << 16
    archive.writestr(info, source.read_bytes())


if __name__ == "__main__":
    raise SystemExit(main())
