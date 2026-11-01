"""A selected package artifact whose connection is lost after its bounded attempts is named by artifact and transport loss.

The diagnostic names the publisher key, the repository metadata or the RPM, never a
source URL, and stays distinct from TLS trust, status, resolution, digest and signer
refusals, which keep their own projection.
"""

from __future__ import annotations

import copy
import hashlib
import http.client
import io
import json
import os
import socket
import ssl
import urllib.error

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import (
    ceph_native, ceph_package_sources as sources, ceph_package_worker, ceph_packages as packages, native_apply, native_resolution, native_signatures,
)
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_packages as fixtures
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils.test_ceph_packages import package_request  # pylint: disable=unused-import

MIRROR = "https://mirror-7.private.example.test:8443/internal/ceph-9"
KEY = "https://mirror-7.private.example.test:8443/internal/keys/RPM-GPG-KEY-IBM-CEPH"
TREE = "https://192.0.2.10:8443/os/rhel/tree"
CLOSED = "Remote end closed connection without response"


class Response:
    def __init__(self, url, data):
        self.status, self.url, self.data, self.headers, self.offset = 200, url, data, {}, 0

    def geturl(self):
        return self.url

    def read(self, amount):
        block = self.data[self.offset:self.offset + amount]
        self.offset += len(block)
        return block

    def __enter__(self):
        return self

    def __exit__(self, *args):
        return False


class Opener:
    """Fail every request whose URL ends with a scripted suffix; serve any other one."""

    def __init__(self, faults):
        self.faults, self.calls = faults, []

    def open(self, request, timeout):
        self.calls.append(request.full_url)
        for suffix, fault in self.faults.items():
            if request.full_url.endswith(suffix):
                raise fault() if callable(fault) else fault
        return Response(request.full_url, b"payload")


@pytest.fixture(autouse=True)
def pauses(monkeypatch):
    slept = []
    monkeypatch.setattr(sources.time, "sleep", slept.append)
    return slept


def acquisition(faults):
    value = object.__new__(sources.Acquisition)
    value.source = {"url": TREE, "mediaSHA256": "c" * 64}
    value.repository_base, value.repository_key = MIRROR, KEY
    value.repository_opener = value.public = value.local = Opener(faults)
    value.anchor, value.total, value.expanded = "the host trust store", 0, 0
    value.deadline = sources.time.monotonic() + 600
    return value


def package(name, epoch=0, architecture="x86_64"):
    url = MIRROR + "/" + name + ".rpm"
    return {"name": name, "epoch": epoch, "version": "20.2.1", "release": "405.el9cp", "architecture": architecture,
            "signer": native_signatures.IBM_CEPH_SIGNER,
            "source": {"id": "native-" + hashlib.sha256(name.encode()).hexdigest()[:32], "url": url, "sha256": hashlib.sha256(b"payload").hexdigest(),
                       "bytes": 7}}


LOSSES = {
    "closed-without-response": lambda: http.client.RemoteDisconnected(CLOSED),
    "reset": lambda: ConnectionResetError(104, "Connection reset by peer"),
    "read-timeout": lambda: socket.timeout("timed out"),
    "connect-timeout": lambda: urllib.error.URLError(socket.timeout("timed out")),
    "tls-eof": lambda: ssl.SSLEOFError(8, "EOF occurred in violation of protocol"),
}


@pytest.mark.parametrize("loss", sorted(LOSSES))
def test_a_lost_publisher_key_connection_names_the_publisher_key(tmp_path, loss):
    value = acquisition({"RPM-GPG-KEY-IBM-CEPH": LOSSES[loss]})
    with pytest.raises(sources.AcquisitionLoss) as failure:
        value.publisher_key(str(tmp_path))
    assert failure.value.artifact == "IBM Ceph publisher key" and failure.value.cause == str(LOSSES[loss]())
    assert isinstance(failure.value.__cause__, type(LOSSES[loss]())) and len(value.repository_opener.calls) == sources.TRANSPORT_ATTEMPTS


@pytest.mark.parametrize("loss", sorted(LOSSES))
def test_a_lost_rpm_connection_names_that_rpm(tmp_path, loss):
    plan = {"packages": [package("ceph-common"), package("cephadm", epoch=2, architecture="noarch")]}
    value = acquisition({"/cephadm.rpm": LOSSES[loss]})
    with pytest.raises(sources.AcquisitionLoss) as failure:
        value.payloads(str(tmp_path), plan)
    assert failure.value.artifact == "RPM cephadm-2:20.2.1-405.el9cp.noarch"


@pytest.mark.parametrize("loss", sorted(LOSSES))
def test_a_lost_repository_metadata_connection_names_the_repository(tmp_path, loss):
    value = acquisition({"/repodata/repomd.xml": LOSSES[loss]})
    with pytest.raises(sources.AcquisitionLoss) as failure:
        value.repository(str(tmp_path), "ibm-storage-ceph", MIRROR, native_signatures.IBM_CEPH_SIGNER)
    assert failure.value.artifact == "repository metadata ibm-storage-ceph"


def test_a_lost_installation_tree_connection_names_the_tree_metadata(tmp_path):
    value = acquisition({"/.bootwright-tree-identity": LOSSES["closed-without-response"]})
    with pytest.raises(sources.AcquisitionLoss) as failure:
        value.repositories(str(tmp_path))
    assert failure.value.artifact == "installation tree metadata"


@pytest.mark.parametrize("fault, raised", [
    (urllib.error.URLError(ssl.SSLCertVerificationError(1, "certificate verify failed")), sources.RepositoryTrustFailure),
    (ssl.SSLError(1, "wrong version number"), ssl.SSLError),
    (urllib.error.HTTPError(KEY, 503, "Service Unavailable", {}, io.BytesIO()), urllib.error.HTTPError),
    (urllib.error.URLError(socket.gaierror(-2, "Name or service not known")), urllib.error.URLError),
], ids=["tls-verification", "tls-protocol", "status", "name-resolution"])
def test_trust_status_and_resolution_failures_are_not_transport_loss(tmp_path, fault, raised):
    value = acquisition({"RPM-GPG-KEY-IBM-CEPH": fault})
    with pytest.raises(raised) as failure:
        value.publisher_key(str(tmp_path))
    assert not isinstance(failure.value, sources.AcquisitionLoss)


def test_a_digest_refusal_is_not_transport_loss(tmp_path):
    plan = {"packages": [dict(package("ceph-common"), source=dict(package("ceph-common")["source"], sha256="0" * 64))]}
    with pytest.raises(ceph_native.Refusal, match="^packages$"):
        acquisition({}).payloads(str(tmp_path), plan)


@pytest.fixture(name="prepared")
def prepared_fixture(monkeypatch):
    monkeypatch.setattr(packages, "actual_platform", lambda: {"os": "rhel", "release": "9.8", "architecture": "amd64"})
    plan = fixtures.plan_fixture()
    for item in plan["packages"]:
        item["source"].update(sha256=hashlib.sha256(b"payload").hexdigest(), bytes=7)
    plan["digest"] = native_resolution.digest({name: item for name, item in plan.items() if name != "digest"})
    monkeypatch.setattr(ceph_native, "supported_transport", lambda request: True)
    monkeypatch.setattr(os, "geteuid", lambda: 0)
    monkeypatch.setattr(packages, "host_admission", lambda value: None)
    monkeypatch.setattr(native_resolution, "solve4", lambda *args: copy.deepcopy(plan))
    monkeypatch.setattr(native_resolution, "inspect", lambda *args: {"inventorySHA256": plan["beforeSHA256"], "rootsReady": False})
    monkeypatch.setattr(native_apply, "verified_payloads", lambda *args: None)
    verified = []
    monkeypatch.setattr(native_signatures, "verify", lambda *args: verified.append(True))
    return plan, verified


def prepare(package_request, faults, base=sources.IBM_BASE, key=sources.IBM_KEY, resolved=False):
    value = acquisition(faults)
    value.repository_base, value.repository_key = base, key
    if resolved:
        value.repositories = lambda scratch: [value.repository(scratch, "ibm-storage-ceph", base, native_signatures.IBM_CEPH_SIGNER)]
    else:
        value.repositories = lambda scratch: []
    journal = fixtures.Journal()
    with pytest.raises(ceph_native.NativeFailure) as failure:
        packages.prepare(package_request, "node-1", "public-certificate", acquisition=value, journal=journal)
    return failure.value, journal


def test_a_lost_publisher_key_fails_package_acquisition_naming_the_key(package_request, prepared):
    failure, journal = prepare(package_request, {"RPM-GPG-KEY-IBM-CEPH": LOSSES["closed-without-response"]})
    assert str(failure) == "package-acquisition" and failure.native_error == {"coordinate": "IBM Ceph publisher key", "cause": CLOSED}
    assert prepared[1] == [] and journal.writes == ["decision.json"]


def test_a_lost_rpm_fails_package_acquisition_naming_the_rpm_and_its_site(package_request, prepared):
    first = prepared[0]["packages"][0]
    failure, unused = prepare(package_request, {"/" + first["name"] + ".rpm": LOSSES["read-timeout"]})
    coordinate = "RPM %s-%s-%s.%s" % (first["name"], first["version"], first["release"], first["architecture"])
    assert str(failure) == "package-acquisition" and failure.native_error == {"coordinate": coordinate, "cause": "timed out"}
    published = ceph_package_worker.failure(failure)
    assert published["reason"] == "package-acquisition" and published["cause"].startswith("package-acquisition@ceph_package_sources.")
    assert published["nativeError"]["coordinate"] == coordinate and prepared[1] == []


def test_lost_repository_metadata_fails_package_acquisition_before_any_decision(package_request, prepared):
    failure, journal = prepare(package_request, {"/repodata/repomd.xml": LOSSES["connect-timeout"]}, resolved=True)
    assert str(failure) == "package-acquisition" and failure.native_error["coordinate"] == "repository metadata ibm-storage-ceph"
    assert journal.writes == []


def test_a_genuine_signer_refusal_stays_package_signature(package_request, prepared, monkeypatch):
    def foreign(*args):
        raise RuntimeError("payload signed by a foreign key")

    monkeypatch.setattr(native_signatures, "verify", foreign)
    failure, unused = prepare(package_request, {})
    assert str(failure) == "package-signature"


@pytest.mark.parametrize("site", ["key", "rpm", "metadata"])
def test_package_acquisition_discloses_no_source_url(package_request, prepared, site):
    plan = prepared[0]
    for item in plan["packages"]:
        if item["source"]["url"].startswith(sources.IBM_BASE):
            item["source"]["url"] = MIRROR + "/" + item["name"] + ".rpm"
    plan["digest"] = native_resolution.digest({name: item for name, item in plan.items() if name != "digest"})
    faults = {"key": {"RPM-GPG-KEY-IBM-CEPH": LOSSES["reset"]}, "rpm": {".rpm": LOSSES["reset"]}, "metadata": {"repomd.xml": LOSSES["reset"]}}[site]
    failure, unused = prepare(package_request, faults, base=MIRROR, key=KEY, resolved=site == "metadata")
    published = json.dumps(ceph_package_worker.failure(failure))
    assert '"reason": "package-acquisition"' in published
    assert all(value not in published for value in ("mirror-7", "private", "internal", "https://", "192.0.2.10"))


def test_the_package_worker_publishes_package_acquisition():
    assert "package-acquisition" in ceph_package_worker.REASONS
