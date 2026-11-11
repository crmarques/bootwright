"""The artifact mirror fill proves each file by the stat the role took of it.

A file is present only as a regular file the stat did not follow a link to,
and verified only at the size and SHA-256 its frozen request pins, so a fill
whose download left another file proves no postcondition, and a removal
proves none while any file remains (internal/infrastructureservices/
mirrorfill, evidence.go, reads the same fields).
"""

from __future__ import annotations

from types import SimpleNamespace

import pytest

from ansible_collections.bootwright.core.plugins.action import infrastructureservices_mirror_files_protocol as protocol

DIGEST = "0123456789abcdef" * 4
FILE = {"path": "kubectl/v1.37.2/kubectl", "sha256": DIGEST, "bytes": 7, "source": "https://dl.k8s.io/release/v1.37.2/bin/linux/amd64/kubectl"}
STAT = {"exists": True, "isreg": True, "islnk": False, "size": 7, "checksum": DIGEST}


def complete(stats, **fields):
    arguments = {"digest": DIGEST, "files": [FILE] * len(stats), "stats": stats, "root": {"exists": any(stat.get("exists") for stat in stats)}}
    return protocol.completion(dict(arguments, **fields))


def test_a_file_at_its_pinned_size_and_digest_proves_the_fill():
    evidence, refused = complete([STAT])
    assert refused is None
    assert evidence == {"absent": False, "files": [{"path": FILE["path"], "verified": True}], "postcondition": True,
                        "request": DIGEST, "version": "mirror-files-evidence-v1"}


@pytest.mark.parametrize("stat", [
    dict(STAT, checksum="f" * 64), dict(STAT, size=8), dict(STAT, islnk=True), dict(STAT, isreg=False), {"exists": False},
], ids=["digest", "size", "link", "directory", "absent"])
def test_a_file_the_build_does_not_pin_proves_no_fill(stat):
    evidence, refused = complete([stat])
    assert not evidence["postcondition"] and not any(entry["verified"] for entry in evidence["files"])
    assert refused is not None
    observed, refused = complete([stat], observed=True)
    assert refused is None and observed == evidence


def test_a_removal_proves_nothing_while_a_file_remains():
    evidence, refused = complete([dict(STAT, checksum="f" * 64)], removed=True)
    assert evidence["files"] and not evidence["postcondition"] and refused is not None
    evidence, refused = complete([{"exists": False}], removed=True)
    assert evidence["postcondition"] and evidence["absent"] and not evidence["files"] and refused is None


@pytest.mark.parametrize("arguments", [
    {"files": [FILE], "stats": []}, {"files": [dict(FILE, bytes="7")], "stats": [STAT]},
    {"files": [FILE] * 129, "stats": [STAT] * 129}, {"files": "x", "stats": []},
], ids=["unpaired", "size type", "unbounded", "not a list"])
def test_a_malformed_observation_is_refused(arguments):
    with pytest.raises(ValueError):
        protocol.completion(dict({"digest": DIGEST}, **arguments))


@pytest.mark.parametrize("reason", ["file-digest-mismatch", "file-unreachable"])
def test_each_fill_refusal_is_named_to_the_runner(reason, monkeypatch):
    published = []
    monkeypatch.setattr(protocol, "emit", lambda message, **kwargs: published.append(message))
    module = protocol.ActionModule.__new__(protocol.ActionModule)
    module._task = SimpleNamespace(args={"phase": "refused", "reason": reason})
    assert module.run(task_vars={}) == {"changed": False}
    assert published == [{"phase": "refused", "reason": reason}]
