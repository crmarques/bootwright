"""A Satellite-shaped BaseOS and AppStream are acquired over TLS that requires the host's entitlement certificate.

The Satellite is an in-process TLS server on 127.0.0.1 that refuses a client
without a certificate its test CA issued; it runs in the controller context for
cryptography, as tests/unit/test_ceph_package_tree_acquisition.py does.
"""

from __future__ import annotations

import datetime
import functools
import http.server
import os
import threading
from types import SimpleNamespace
import urllib.error

from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import ec
from cryptography.x509.oid import ExtendedKeyUsageOID, NameOID
import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_package_sources as sources, native_signatures
from ansible_collections.bootwright.core.tests.unit.test_ceph_package_tree_acquisition import COMMON, Quiet, repodata, serve, serving_identity

RELEASE = "/pulp/content/Default_Organization/Library/content/dist/rhel9/9.8/x86_64"


def client_identity(directory, name):
    key = ec.generate_private_key(ec.SECP256R1())
    subject = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, name)])
    now = datetime.datetime.now(datetime.timezone.utc)
    certificate = (
        x509.CertificateBuilder().subject_name(subject).issuer_name(subject).public_key(key.public_key()).serial_number(x509.random_serial_number())
        .not_valid_before(now - datetime.timedelta(minutes=5)).not_valid_after(now + datetime.timedelta(days=1))
        .add_extension(x509.BasicConstraints(ca=False, path_length=None), critical=True)
        .add_extension(x509.ExtendedKeyUsage([ExtendedKeyUsageOID.CLIENT_AUTH]), critical=False)
        .sign(key, hashes.SHA256()))
    chain, private = directory / (name + ".pem"), directory / (name + "-key.pem")
    chain.write_bytes(certificate.public_bytes(serialization.Encoding.PEM))
    private.write_bytes(key.private_bytes(serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8, serialization.NoEncryption()))
    os.chmod(chain, 0o644)
    os.chmod(private, 0o600)
    return str(chain), str(private)


def root_owned(path):
    return SimpleNamespace(st_mode=os.lstat(path).st_mode, st_uid=0)


@pytest.fixture(name="satellite")
def satellite_fixture(tmp_path, monkeypatch):
    root = tmp_path / "served"
    for variant in ("baseos", "appstream"):
        repodata(root / RELEASE.lstrip("/") / variant / "os", COMMON)
    repodata(tmp_path / "ibm" / "ceph", COMMON)
    entitlement = tmp_path / "entitlement"
    entitlement.mkdir(mode=0o755)
    monkeypatch.setattr(sources, "ENTITLEMENT_DIRECTORY", str(entitlement))
    certificate, key = client_identity(entitlement, "1234")
    stranger = client_identity(tmp_path, "stranger")
    chain, private = serving_identity(tmp_path)
    context = sources.ssl.SSLContext(sources.ssl.PROTOCOL_TLS_SERVER)
    context.load_cert_chain(str(chain), str(private))
    context.verify_mode = sources.ssl.CERT_REQUIRED
    context.load_verify_locations(cafile=certificate)
    seen = []
    handler = type("Recording", (Quiet,), {"seen": seen})
    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), functools.partial(handler, directory=str(root)))
    server.socket = context.wrap_socket(server.socket, server_side=True)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    base = "https://127.0.0.1:%d" % server.server_address[1]
    ibm_seen = []
    ibm, ibm_thread, ibm_base = serve(tmp_path / "ibm", chain, private, ibm_seen)
    source = {"host": "node-1", "profile": "rhel", "certificateRef": "satellite-ca",
              "subscription": {"hostname": "satellite.example.test", "contentBaseURL": base + "/pulp/content"}}
    records = [{"enabled": True, "baseurl": [base + RELEASE + "/" + variant + "/os"], "sslverify": True, "sslclientcert": certificate,
                "sslclientkey": key} for variant in ("baseos", "appstream")]
    yield SimpleNamespace(source=source, bundle=chain.read_text(), base=base, ibm=ibm_base + "/ceph", records=records, seen=seen, ibm_seen=ibm_seen,
                          stranger=stranger)
    for running, joined in ((server, thread), (ibm, ibm_thread)):
        running.shutdown()
        running.server_close()
        joined.join()


def acquisition(satellite, bundle, records):
    return sources.Acquisition(satellite.source, bundle, repository=satellite.ibm, repository_key=satellite.ibm + "/ibm.asc",
                               trust=satellite.bundle, trust_ref="repository-ca", reader=lambda: records, inspect=root_owned)


def test_the_satellite_repositories_are_acquired_with_the_entitlement_certificate(satellite, tmp_path):
    scratch = tmp_path / "stage"
    scratch.mkdir()
    repositories = acquisition(satellite, satellite.bundle, satellite.records).repositories(str(scratch))
    assert [(r["id"], r["baseURL"], r["signer"]) for r in repositories] == [
        ("rhel-baseos", satellite.base + RELEASE + "/baseos/os", native_signatures.RHEL_SIGNER),
        ("rhel-appstream", satellite.base + RELEASE + "/appstream/os", native_signatures.RHEL_SIGNER),
        ("ibm-storage-ceph", satellite.ibm, native_signatures.IBM_CEPH_SIGNER)]
    assert RELEASE + "/baseos/os/repodata/repomd.xml" in satellite.seen and RELEASE + "/appstream/os/repodata/repomd.xml" in satellite.seen
    assert "/ceph/repodata/repomd.xml" in satellite.ibm_seen and not [path for path in satellite.seen if "/ceph/" in path]


def test_the_satellite_refuses_a_host_without_the_entitlement_certificate(satellite, tmp_path):
    stranger, stranger_key = satellite.stranger
    entitlement = tmp_path / "entitlement"
    os.replace(stranger, entitlement / "9999.pem")
    os.replace(stranger_key, entitlement / "9999-key.pem")
    records = [dict(r, sslclientcert=str(entitlement / "9999.pem"), sslclientkey=str(entitlement / "9999-key.pem")) for r in satellite.records]
    scratch = tmp_path / "stage"
    scratch.mkdir()
    # Under load the refusal can arrive as a lost connection, which the bounded retry names a transport loss.
    with pytest.raises((urllib.error.URLError, sources.ssl.SSLError, OSError, sources.AcquisitionLoss)):
        acquisition(satellite, satellite.bundle, records).repositories(str(scratch))
    assert satellite.seen == []


def test_a_satellite_the_bundle_does_not_verify_names_the_satellite_and_the_secret(satellite, tmp_path):
    other, _key = serving_identity(tmp_path, prefix="other")
    scratch = tmp_path / "stage"
    scratch.mkdir()
    with pytest.raises(sources.RepositoryTrustFailure) as failure:
        acquisition(satellite, other.read_text(), satellite.records).repositories(str(scratch))
    assert str(failure.value) == ("TLS verification of the Satellite satellite.example.test content root " + satellite.base + "/pulp/content"
                                  + " failed against CA bundle Secret satellite-ca")
    assert "-----" not in str(failure.value) and satellite.seen == []
