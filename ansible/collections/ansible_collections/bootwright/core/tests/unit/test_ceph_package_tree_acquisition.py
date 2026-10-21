"""A real-shaped RHEL 9.8 DVD tree and IBM repository are acquired end to end.

The tree is served from an in-process TLS server on 127.0.0.1 whose certificate
cryptography builds at test time. It runs in the controller context because the
managed-host floor interpreter, which runs the module utility tests, carries no
cryptography; the metadata scan cases are in
tests/unit/plugins/module_utils/test_ceph_package_sources.py.
"""

from __future__ import annotations

import datetime
import functools
import gzip
import hashlib
import http.server
import ipaddress
from pathlib import Path
import threading
from types import SimpleNamespace
import urllib.error

from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import ec
from cryptography.x509.oid import ExtendedKeyUsageOID, NameOID
import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native, ceph_package_sources as sources, native_signatures
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils.test_ceph_package_sources import (
    DVD_COMPS, FILELISTS, MODULES, OTHER, PRIMARY, TREEINFO,
)


def serving_identity(tmp_path, issuer=None, prefix="server"):
    key = ec.generate_private_key(ec.SECP256R1())
    name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, "127.0.0.1")])
    issuer_name, signer = (issuer[0].subject, issuer[1]) if issuer else (name, key)
    now = datetime.datetime.now(datetime.timezone.utc)
    certificate = (
        x509.CertificateBuilder().subject_name(name).issuer_name(issuer_name).public_key(key.public_key()).serial_number(x509.random_serial_number())
        .not_valid_before(now - datetime.timedelta(minutes=5)).not_valid_after(now + datetime.timedelta(days=1))
        .add_extension(x509.SubjectAlternativeName([x509.IPAddress(ipaddress.ip_address("127.0.0.1")), x509.DNSName("localhost")]), critical=False)
        .add_extension(x509.BasicConstraints(ca=False, path_length=None), critical=True)
        .add_extension(x509.KeyUsage(digital_signature=True, content_commitment=False, key_encipherment=False, data_encipherment=False,
                                     key_agreement=False, key_cert_sign=False, crl_sign=False, encipher_only=False, decipher_only=False), critical=True)
        .add_extension(x509.ExtendedKeyUsage([ExtendedKeyUsageOID.SERVER_AUTH]), critical=False))
    if issuer:
        certificate = certificate.add_extension(x509.AuthorityKeyIdentifier.from_issuer_public_key(signer.public_key()), critical=False)
    certificate = certificate.sign(signer, hashes.SHA256())
    chain, private = tmp_path / (prefix + ".pem"), tmp_path / (prefix + ".key")
    chain.write_bytes(certificate.public_bytes(serialization.Encoding.PEM))
    private.write_bytes(key.private_bytes(serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8, serialization.NoEncryption()))
    return chain, private


def repodata(directory, members):
    """A createrepo-shaped repomd.xml and its content-addressed members."""
    (directory / "repodata").mkdir(parents=True)
    rows = []
    for kind, suffix, data, compress in members:
        stored = gzip.compress(data) if compress else data
        digest = hashlib.sha256(stored).hexdigest()
        href = "repodata/%s-%s" % (digest, suffix)
        (directory / href).write_bytes(stored)
        opened = ("\n    <open-checksum type=\"sha256\">%s</open-checksum>" % hashlib.sha256(data).hexdigest()) if compress else ""
        expanded = ("\n    <open-size>%d</open-size>" % len(data)) if compress else ""
        rows.append('  <data type="%s">\n    <checksum type="sha256">%s</checksum>%s\n    <location href="%s"/>\n    <timestamp>1746900000</timestamp>\n'
                    '    <size>%d</size>%s\n  </data>\n' % (kind, digest, opened, href, len(stored), expanded))
    rows.append('  <data type="productid">\n    <checksum type="sha256">%s</checksum>\n    <location href="repodata/productid.gz"/>\n'
                '    <size>1</size>\n  </data>\n' % ("0" * 64))
    (directory / "repodata" / "repomd.xml").write_text('<?xml version="1.0" encoding="UTF-8"?>\n<repomd xmlns="http://linux.duke.edu/metadata/repo" '
                                                       'xmlns:rpm="http://linux.duke.edu/metadata/rpm">\n  <revision>1746900000</revision>\n'
                                                       + "".join(rows) + "</repomd>\n")


class Quiet(http.server.SimpleHTTPRequestHandler):
    seen = None

    def log_message(self, *args):
        return

    def send_head(self):
        if self.seen is not None:
            self.seen.append(self.path)
        return super().send_head()


COMMON = [("primary", "primary.xml.gz", PRIMARY, True), ("filelists", "filelists.xml.gz", FILELISTS, True), ("other", "other.xml.gz", OTHER, True)]


def serve(root, chain, private, seen):
    context = sources.ssl.SSLContext(sources.ssl.PROTOCOL_TLS_SERVER)
    context.load_cert_chain(str(chain), str(private))
    handler = type("Recording", (Quiet,), {"seen": seen})
    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), functools.partial(handler, directory=str(root)))
    server.socket = context.wrap_socket(server.socket, server_side=True)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    return server, thread, "https://127.0.0.1:%d" % server.server_address[1]


@pytest.fixture(name="served")
def served_fixture(tmp_path, monkeypatch):
    root = tmp_path / "served"
    tree = root / "os" / "rhel" / "tree"
    for variant, extra in (("BaseOS", []), ("AppStream", [("modules", "modules.yaml.gz", MODULES, True)])):
        comps = DVD_COMPS.replace(b"<id>core</id>", ("<id>%s</id>" % variant.lower()).encode())
        repodata(tree / variant, COMMON + [("group", "comps-%s.x86_64.xml" % variant, comps, False),
                                           ("group_gz", "comps-%s.x86_64.xml.gz" % variant, comps, True)] + extra)
    repodata(root / "ibm" / "ceph" / "9" / "rhel9" / "x86_64", COMMON)
    (tree / ".bootwright-tree-identity").write_text("c" * 64 + "\n")
    (tree / ".treeinfo").write_bytes(TREEINFO)
    chain, private = serving_identity(tmp_path)
    seen = []
    server, thread, base = serve(root, chain, private, seen)
    monkeypatch.setattr(sources, "IBM_BASE", base + "/ibm/ceph/9/rhel9/x86_64")
    monkeypatch.setattr(sources, "IBM_KEY", base + "/ibm/ceph/RPM-GPG-KEY-IBM-CEPH")
    source = {"host": "node-1", "profile": "rhel", "media": "rhel-dvd.iso", "mediaSHA256": "c" * 64, "mediaSize": 1000000,
              "publicationPath": "/var/lib/bootwright-services/example/artifact-server/artifacts/public/os/rhel/tree",
              "url": base + "/os/rhel/tree", "certificateRef": "serving-tls"}
    yield SimpleNamespace(source=source, chain=chain.read_text(), base=base, seen=seen)
    server.shutdown()
    server.server_close()
    thread.join()


@pytest.fixture(name="served_tree")
def served_tree_fixture(served):
    acquisition = sources.Acquisition(served.source, served.chain, repository=served.base + "/ibm/ceph/9/rhel9/x86_64", repository_key=sources.IBM_KEY)
    # The served IBM stand-in shares the loopback anchor; production trusts the system store.
    acquisition.public = acquisition.repository_opener = acquisition.local
    return acquisition, served.base


@pytest.fixture(name="mirror")
def mirror_fixture(tmp_path):
    """An IBM-shaped repository on a second loopback server whose leaf a test CA signs."""
    key = ec.generate_private_key(ec.SECP256R1())
    name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, "Synthetic repository CA")])
    now = datetime.datetime.now(datetime.timezone.utc)
    authority = (
        x509.CertificateBuilder().subject_name(name).issuer_name(name).public_key(key.public_key()).serial_number(x509.random_serial_number())
        .not_valid_before(now - datetime.timedelta(minutes=5)).not_valid_after(now + datetime.timedelta(days=1))
        .add_extension(x509.BasicConstraints(ca=True, path_length=0), critical=True)
        .add_extension(x509.SubjectKeyIdentifier.from_public_key(key.public_key()), critical=False)
        .add_extension(x509.KeyUsage(digital_signature=False, content_commitment=False, key_encipherment=False, data_encipherment=False,
                                     key_agreement=False, key_cert_sign=True, crl_sign=True, encipher_only=False, decipher_only=False), critical=True)
        .sign(key, hashes.SHA256()))
    root = tmp_path / "mirror"
    repodata(root / "ceph", COMMON)
    (root / "ceph" / "ibm.asc").write_bytes(b"synthetic publisher key")
    chain, private = serving_identity(tmp_path, (authority, key), "mirror")
    seen = []
    server, thread, base = serve(root, chain, private, seen)
    yield SimpleNamespace(base=base + "/ceph", authority=authority.public_bytes(serialization.Encoding.PEM).decode(), seen=seen)
    server.shutdown()
    server.server_close()
    thread.join()


def test_a_real_shaped_dvd_tree_and_ibm_repository_are_acquired_with_their_comps(served_tree, tmp_path):
    acquisition, base = served_tree
    scratch = tmp_path / "stage"
    scratch.mkdir()
    repositories = acquisition.repositories(str(scratch))
    assert [(r["id"], r["baseURL"], r["signer"]) for r in repositories] == [
        ("rhel-baseos", base + "/os/rhel/tree/BaseOS", native_signatures.RHEL_SIGNER),
        ("rhel-appstream", base + "/os/rhel/tree/AppStream", native_signatures.RHEL_SIGNER),
        ("ibm-storage-ceph", base + "/ibm/ceph/9/rhel9/x86_64", native_signatures.IBM_CEPH_SIGNER)]
    for repository, comps in zip(repositories, (2, 2, 0)):
        stored = sorted(path.name for path in (scratch / "repositories" / repository["id"] / "repodata").iterdir())
        assert len([name for name in stored if "-comps-" in name]) == comps and "repomd.xml" in stored
    assert acquisition.expanded > 0


def test_acquisition_trusts_only_the_bound_bundle(served, mirror, tmp_path):
    acquisition = sources.Acquisition(served.source, served.chain, repository=mirror.base,
                                      repository_key=mirror.base + "/ibm.asc", trust=mirror.authority, trust_ref="repository-ca")
    scratch = tmp_path / "stage"
    scratch.mkdir()
    repositories = acquisition.repositories(str(scratch))
    assert len(repositories) == 3
    keys = acquisition.publisher_key(str(scratch))
    assert Path(keys[native_signatures.IBM_CEPH_SIGNER]).read_bytes() == b"synthetic publisher key"
    ceph = repositories[2]
    assert (ceph["id"], ceph["baseURL"], ceph["signer"]) == ("ibm-storage-ceph", mirror.base, native_signatures.IBM_CEPH_SIGNER)
    assert mirror.seen and not [path for path in served.seen if path.startswith("/ibm/")]
    untrusted = sources.Acquisition(served.source, served.chain, repository=mirror.base,
                                    repository_key=mirror.base + "/ibm.asc", trust=served.chain, trust_ref="repository-ca")
    other = tmp_path / "other"
    other.mkdir()
    with pytest.raises(sources.RepositoryTrustFailure) as failure:
        untrusted.repositories(str(other))
    assert str(failure.value) == "TLS verification of the package repository " + mirror.base + " failed against trust bundle Secret repository-ca"
    assert "-----" not in str(failure.value) and failure.value.__cause__ is None and failure.value.__context__ is None


def test_acquisition_never_falls_back_to_the_default_repository(served, tmp_path):
    mirror = served.base + "/mirror/ceph"
    acquisition = sources.Acquisition(served.source, served.chain, repository=mirror,
                                      repository_key=mirror + "/ibm.asc", trust=served.chain, trust_ref="repository-ca")
    scratch = tmp_path / "stage"
    scratch.mkdir()
    with pytest.raises(urllib.error.HTTPError):
        acquisition.repositories(str(scratch))
    assert "/mirror/ceph/repodata/repomd.xml" in served.seen and not [path for path in served.seen if path.startswith("/ibm/")]
    requested = len(served.seen)
    package = {"name": "cephadm", "signer": native_signatures.IBM_CEPH_SIGNER,
               "source": {"id": "native-" + "a" * 32, "url": sources.IBM_BASE + "/cephadm.rpm", "sha256": "f" * 64, "bytes": 64}}
    with pytest.raises(ceph_native.Refusal, match="^packages$"):
        acquisition.payloads(str(scratch), {"packages": [package]})
    assert len(served.seen) == requested
    with pytest.raises(TypeError):
        sources.Acquisition(served.source, served.chain)  # pylint: disable=missing-kwoa


def test_mirror_key_requires_its_bound_anchor_without_host_fallback(served, mirror, tmp_path):
    for trust, ref in ((None, None), (served.chain, "wrong-ca")):
        acquisition = sources.Acquisition(served.source, served.chain, repository=mirror.base,
                                          repository_key=mirror.base + "/ibm.asc", trust=trust, trust_ref=ref)
        scratch = tmp_path / (ref or "host-store")
        scratch.mkdir()
        with pytest.raises(sources.RepositoryTrustFailure, match="publisher key"):
            acquisition.publisher_key(str(scratch))
    assert mirror.seen == []
