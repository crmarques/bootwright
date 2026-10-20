"""The Ceph SMB block publishes a completion only when its evidence proves the operation's postcondition.

The role publishes completed with observed true on observe and removed true on
destroy (roles/storage_ceph_smb/tasks/*.yml); an apply must prove ready, a
destroy removed, and an observation publishes what it found. Every evidence
record here is a synthetic fixture in the closed shape ceph_core.smb_evidence
admits.
"""

from __future__ import annotations

from types import SimpleNamespace

import pytest

from ansible_collections.bootwright.core.plugins.action import ceph_smb_protocol as protocol

DIGEST = "d" * 64


def evidence(**changes):
    value = {"daemons": [], "exists": False, "fsid": "4c0a4300-aa43-458c-86d7-ac2256d1fc00", "owned": True, "owner": "apply-123", "partial": False,
             "ready": False, "removed": False, "request": DIGEST, "resource": "", "version": "ceph-smb-evidence-v1"}
    value.update(changes)
    return value


def run(args, monkeypatch):
    published = []
    monkeypatch.setattr(protocol, "emit", lambda message, **kwargs: published.append(message))
    module = protocol.ActionModule.__new__(protocol.ActionModule)
    module._task = SimpleNamespace(args=args)
    return module.run(task_vars={}), published


def completed(value, observed, removed):
    return {"phase": "completed", "outcome": "unchanged", "digest": DIGEST, "observed": observed, "removed": removed, "evidence": value}


@pytest.mark.parametrize("value, observed, removed", [
    (evidence(exists=True, ready=True, resource="ceph.smb.cluster.files"), False, False),
    (evidence(removed=True), False, True),
    (evidence(), True, False),
    (evidence(exists=True, partial=True, resource="ceph.smb.cluster.files"), True, False),
])
def test_a_proved_postcondition_or_an_observation_is_published(value, observed, removed, monkeypatch):
    result, published = run(completed(value, observed, removed), monkeypatch)
    assert result == {"changed": False}
    assert published == [{"phase": "completed", "outcome": "unchanged", "evidence": value}]


@pytest.mark.parametrize("value, removed", [
    (evidence(exists=True, partial=True, resource="ceph.smb.cluster.files"), False),
    (evidence(removed=True), False),
    (evidence(exists=True, ready=True, resource="ceph.smb.cluster.files"), True),
])
def test_an_unproved_postcondition_is_never_published(value, removed, monkeypatch):
    result, published = run(completed(value, False, removed), monkeypatch)
    assert result == {"failed": True, "msg": "the Ceph SMB operation has not proved its postcondition"}
    assert not published


@pytest.mark.parametrize("args", [
    completed(evidence(ready=True), "true", False),
    completed(dict(evidence(ready=True), password="x"), False, False),
    dict(completed(evidence(ready=True), False, False), digest="e" * 64),
], ids=["observed-not-bool", "extra-member", "other-request"])
def test_a_completion_it_cannot_validate_is_never_published(args, monkeypatch):
    result, published = run(args, monkeypatch)
    assert result == {"failed": True, "msg": "the Ceph SMB capability result could not be published"}
    assert not published
