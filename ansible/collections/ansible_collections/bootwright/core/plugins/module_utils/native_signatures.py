"""Verify RPM payloads using only each frozen publisher's selected public key."""

from __future__ import annotations

import os
from pathlib import Path
import stat
import tempfile

IBM_CEPH_SIGNER = "005f07b2f3838aedc2c0a4d91e1850467eaa5b6e"
RHEL_SIGNER = "567e347ad0044ade55ba8a5f199e2f91fd431d51"


def file_identity(info):
    return (
        info.st_dev,
        info.st_ino,
        info.st_mode,
        info.st_uid,
        info.st_gid,
        info.st_nlink,
        info.st_size,
        info.st_mtime_ns,
        info.st_ctime_ns,
    )


def publisher_key(platform):
    if platform["os"] == "fedora":
        return "/etc/pki/rpm-gpg/RPM-GPG-KEY-fedora-43-primary"
    if platform["os"] == "rhel":
        return "/etc/pki/rpm-gpg/RPM-GPG-KEY-redhat-release"
    raise ValueError("native publisher platform")


def read_key(path):
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        info = os.fstat(descriptor)
        if (
            not stat.S_ISREG(info.st_mode)
            or info.st_uid != 0
            or info.st_mode & 0o022
            or not 0 < info.st_size <= 1 << 20
        ):
            raise ValueError("native publisher key authority")
        data = bytearray()
        while len(data) <= info.st_size:
            block = os.read(descriptor, min(65536, info.st_size + 1 - len(data)))
            if not block:
                break
            data.extend(block)
        if len(data) != info.st_size or file_identity(
            os.fstat(descriptor)
        ) != file_identity(info):
            raise ValueError("native publisher key changed")
        return bytes(data)
    finally:
        os.close(descriptor)


def select_key(keys, fingerprint, five):
    selected = [
        key
        for key in keys
        if (key.get_fingerprint() if five else key.fingerprint).lower() == fingerprint
    ]
    if len(selected) != 1:
        raise ValueError("native publisher fingerprint")
    return selected[0]


def key_for(request, signer):
    if request.get("policy") != "ceph-packages-v1":
        return read_key(publisher_key(request["platform"]))
    if request["platform"] != {"os": "rhel", "release": "9.8", "architecture": "amd64"}:
        raise ValueError("Ceph publisher platform")
    if signer == RHEL_SIGNER:
        return read_key(publisher_key(request["platform"]))
    if signer == IBM_CEPH_SIGNER and set(request.get("publisherKeys", {})) == {IBM_CEPH_SIGNER}:
        return read_key(request["publisherKeys"][signer])
    raise ValueError("Ceph publisher authority")


def import_ceph_keys(request):
    """The frozen Ceph prerequisite grant retains each plan signer's exact key.

    Isolated verification of the entire downloaded closure must precede this
    call. Generic controller transactions never acquire this additional trust.
    An already present key is kept: RPM reports its import as success.
    """
    import dnf.crypto
    import dnf.yum.misc
    import rpm

    from ansible_collections.bootwright.core.plugins.module_utils import native_resolution

    native_resolution.selected_roots(request)
    if request.get("policy") != "ceph-packages-v1" or os.geteuid() != 0:
        raise ValueError("Ceph publisher import authority")
    signers = sorted({package["signer"] for package in request["plan"]["packages"]})
    if not 0 < len(signers) <= 8:
        raise ValueError("native publisher closure")
    keys = []
    for signer in signers:
        # DNF 4.19+ reads a key through its descriptor, so the validated bytes get a real one.
        with tempfile.TemporaryFile() as stream:
            stream.write(key_for(request, signer))
            stream.seek(0)
            keys.append(select_key(dnf.crypto.rawkey2infos(stream), signer, False))
    for key in keys:
        transaction = rpm.TransactionSet("/")
        try:
            if transaction.pgpImportPubkey(dnf.yum.misc.procgpgkey(key.raw_key)) != 0:
                raise ValueError("Ceph publisher key import")
        finally:
            transaction.closeDB()


def verify(request, scratch, payloads):
    from ansible_collections.bootwright.core.plugins.module_utils import (
        native_resolution,
    )

    plan = request["plan"]
    signers = sorted({package["signer"] for package in plan["packages"]})
    if not 0 < len(signers) <= 8:
        raise ValueError("native publisher closure")
    for signer in signers:
        keys = key_for(request, signer)
        with tempfile.TemporaryDirectory(prefix="publisher-", dir=scratch) as directory:
            root = Path(directory, "root")
            root.mkdir(mode=0o700)
            key_path = Path(directory, "publisher.asc")
            key_path.write_bytes(keys)
            if plan["solver"] == "dnf5":
                import libdnf5

                base = native_resolution.setup_dnf5(
                    request, directory, repositories=False, installroot=str(root)
                )
                verifier = libdnf5.rpm.RpmSignature(base)
                key = select_key(
                    verifier.parse_key_file(key_path.as_uri()), signer, True
                )
                verifier.import_key(key)
                for package in plan["packages"]:
                    if (
                        package["signer"] == signer
                        and verifier.check_package_signature(
                            payloads[package["source"]["id"]]
                        )
                        != libdnf5.rpm.RpmSignature.CheckResult_OK
                    ):
                        raise ValueError("native approved publisher signature")
            elif plan["solver"] == "dnf4":
                import dnf.crypto
                import dnf.yum.misc
                import rpm

                with key_path.open("rb") as stream:
                    key = select_key(dnf.crypto.rawkey2infos(stream), signer, False)
                transaction = rpm.TransactionSet(str(root))
                transaction.initDB()
                if (
                    transaction.pgpImportPubkey(dnf.yum.misc.procgpgkey(key.raw_key))
                    != 0
                ):
                    raise ValueError("native publisher key import")
                transaction.closeDB()
                base = native_resolution.setup_dnf4(
                    request, directory, repositories=False, installroot=str(root)
                )
                try:
                    sources = [
                        payloads[package["source"]["id"]]
                        for package in plan["packages"]
                        if package["signer"] == signer
                    ]
                    for package in base.add_remote_rpms(sources, strict=True):
                        if base.package_signature_check(package)[0] != 0:
                            raise ValueError("native approved publisher signature")
                finally:
                    base.close()
            else:
                raise ValueError("native publisher solver")
