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
import threading

from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import ec
from cryptography.x509.oid import ExtendedKeyUsageOID, NameOID
import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_package_sources as sources, native_signatures
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils.test_ceph_package_sources import (
    DVD_COMPS, FILELISTS, MODULES, OTHER, PRIMARY, TREEINFO,
)


def serving_identity(tmp_path):
    key = ec.generate_private_key(ec.SECP256R1())
    name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, "127.0.0.1")])
    now = datetime.datetime.now(datetime.timezone.utc)
    certificate = (
        x509.CertificateBuilder().subject_name(name).issuer_name(name).public_key(key.public_key()).serial_number(x509.random_serial_number())
        .not_valid_before(now - datetime.timedelta(minutes=5)).not_valid_after(now + datetime.timedelta(days=1))
        .add_extension(x509.SubjectAlternativeName([x509.IPAddress(ipaddress.ip_address("127.0.0.1")), x509.DNSName("localhost")]), critical=False)
        .add_extension(x509.BasicConstraints(ca=False, path_length=None), critical=True)
        .add_extension(x509.KeyUsage(digital_signature=True, content_commitment=False, key_encipherment=False, data_encipherment=False,
                                     key_agreement=False, key_cert_sign=False, crl_sign=False, encipher_only=False, decipher_only=False), critical=True)
        .add_extension(x509.ExtendedKeyUsage([ExtendedKeyUsageOID.SERVER_AUTH]), critical=False)
        .sign(key, hashes.SHA256()))
    chain, private = tmp_path / "server.pem", tmp_path / "server.key"
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
    def log_message(self, *args):
        return


@pytest.fixture(name="served_tree")
def served_tree_fixture(tmp_path, monkeypatch):
    root = tmp_path / "served"
    tree = root / "os" / "rhel" / "tree"
    common = [("primary", "primary.xml.gz", PRIMARY, True), ("filelists", "filelists.xml.gz", FILELISTS, True), ("other", "other.xml.gz", OTHER, True)]
    for variant, extra in (("BaseOS", []), ("AppStream", [("modules", "modules.yaml.gz", MODULES, True)])):
        comps = DVD_COMPS.replace(b"<id>core</id>", ("<id>%s</id>" % variant.lower()).encode())
        repodata(tree / variant, common + [("group", "comps-%s.x86_64.xml" % variant, comps, False),
                                           ("group_gz", "comps-%s.x86_64.xml.gz" % variant, comps, True)] + extra)
    repodata(root / "ibm" / "ceph" / "9" / "rhel9" / "x86_64", common)
    (tree / ".bootwright-tree-identity").write_text("c" * 64 + "\n")
    (tree / ".treeinfo").write_bytes(TREEINFO)
    chain, private = serving_identity(tmp_path)
    context = sources.ssl.SSLContext(sources.ssl.PROTOCOL_TLS_SERVER)
    context.load_cert_chain(str(chain), str(private))
    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), functools.partial(Quiet, directory=str(root)))
    server.socket = context.wrap_socket(server.socket, server_side=True)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    base = "https://127.0.0.1:%d" % server.server_address[1]
    monkeypatch.setattr(sources, "IBM_BASE", base + "/ibm/ceph/9/rhel9/x86_64")
    monkeypatch.setattr(sources, "IBM_KEY", base + "/ibm/ceph/RPM-GPG-KEY-IBM-CEPH")
    source = {"host": "node-1", "profile": "rhel", "media": "rhel-dvd.iso", "mediaSHA256": "c" * 64, "mediaSize": 1000000,
              "publicationPath": "/var/lib/bootwright-services/example/artifact-server/artifacts/public/os/rhel/tree",
              "url": base + "/os/rhel/tree", "certificateRef": "serving-tls"}
    acquisition = sources.Acquisition(source, chain.read_text())
    # The served IBM stand-in shares the loopback anchor; production trusts the system store.
    acquisition.public = acquisition.local
    yield acquisition, base
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
        ("ibm-storage-ceph-9", base + "/ibm/ceph/9/rhel9/x86_64", native_signatures.IBM_CEPH_SIGNER)]
    for repository, comps in zip(repositories, (2, 2, 0)):
        stored = sorted(path.name for path in (scratch / "repositories" / repository["id"] / "repodata").iterdir())
        assert len([name for name in stored if "-comps-" in name]) == comps and "repomd.xml" in stored
    assert acquisition.expanded > 0
