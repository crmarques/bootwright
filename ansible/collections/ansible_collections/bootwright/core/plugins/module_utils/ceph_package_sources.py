"""Verified, bounded acquisition for the owned DVD and IBM Ceph repositories."""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

import hashlib
import configparser
import bz2
import gzip
import os
from pathlib import Path, PurePosixPath
import re
import ssl
import time
import urllib.parse
import urllib.request
import xml.etree.ElementTree as ET

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native, native_signatures

IBM_BASE = "https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/9/rhel9/x86_64"
IBM_KEY = "https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/RPM-GPG-KEY-IBM-CEPH"
ROOT_HASHES = {
    "cephadm": "7acf50c99cf3263c64a103476dcf67a3fd93a7b918472db7df660541ea987287",
    "ceph-common": "dc4d5846497d5ec849e1d08f73f9ba6b4d4b202107a884df2fc26a518dd200dc",
    "ibm-storage-ceph-license": "bf6e3a92ac2c38de9cb0a747bfb4afac49b8b227222394396d71a9128c21f65f",
}
NAMESPACE = "{http://linux.duke.edu/metadata/repo}"
METADATA_TYPES = {"primary", "filelists", "modules", "group", "group_gz", "updateinfo", "other"}
# Every createrepo comps member names its external DTD; only that exact prolog is admitted.
COMPS_PROLOG = re.compile(rb"""\A(?:<\?xml\s{1,8}version=(["'])1\.0\1(?:\s{1,8}encoding=(["'])(?i:UTF-8)\2)?"""
                          rb"""(?:\s{1,8}standalone=(["'])(?:yes|no)\3)?\s{0,8}\?>)?"""
                          rb"""\s{0,64}<!DOCTYPE\s{1,64}comps\s{1,64}PUBLIC\s{1,64}"""
                          rb"""(?:"[ \r\na-zA-Z0-9'()+,./:=?;!*#@$_%-]{0,128}"|'[ \r\na-zA-Z0-9()+,./:=?;!*#@$_%-]{0,128}')"""
                          rb"""\s{1,64}(?:"comps\.dtd"|'comps\.dtd')\s{0,64}>""")


def source_for(request, host):
    ceph_native.validate(request)
    package = request.get("packages")
    ceph_native.require(isinstance(package, dict) and set(package) == {"version", "licenseAccepted", "sources"}, "packages")
    ceph_native.require(package["version"] == "ceph-packages-v1" and package["licenseAccepted"] is True, "packages")
    sources = package["sources"]
    ceph_native.require(isinstance(sources, list) and len(sources) == len(request["hosts"]), "packages")
    names = []
    selected = None
    for source in sources:
        ceph_native.require(isinstance(source, dict)
                            and set(source) == {"host", "profile", "media", "mediaSHA256", "mediaSize", "publicationPath", "url", "certificateRef"}, "packages")
        for field in ("host", "profile", "certificateRef"):
            ceph_native.require(ceph_native.named(source[field]), "packages")
        ceph_native.require(isinstance(source["media"], str) and re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,254}\.iso", source["media"]) is not None,
                            "packages")
        ceph_native.require(ceph_native.digest(source["mediaSHA256"]) and type(source["mediaSize"]) is int and 0 < source["mediaSize"] <= 64 << 30, "packages")
        ceph_native.require(isinstance(source["publicationPath"], str) and source["publicationPath"].startswith("/var/lib/bootwright-services/")
                            and source["publicationPath"].endswith("/public/os/" + source["profile"] + "/tree") and len(source["publicationPath"]) <= 1024,
                            "packages")
        safe_url(source["url"])
        ceph_native.require(urllib.parse.urlsplit(source["url"]).path.endswith("/os/" + source["profile"] + "/tree"), "packages")
        names.append(source["host"])
        if source["host"] == host:
            selected = source
    ceph_native.require(names == sorted(h["name"] for h in request["hosts"]) and selected is not None, "packages")
    ceph_native.require(request["cephadm"]["packageVersion"] == "20.2.1-405.el9cp", "packages")
    return selected


def safe_url(value):
    ceph_native.require(isinstance(value, str) and len(value) <= 2048 and not any(ord(c) <= 32 for c in value), "packages")
    parsed = urllib.parse.urlsplit(value)
    ceph_native.require(parsed.scheme == "https" and parsed.hostname and parsed.username is None and parsed.password is None and not parsed.query
                        and not parsed.fragment, "packages")
    decoded = urllib.parse.unquote(parsed.path)
    ceph_native.require("\\" not in decoded and all(part not in (".", "..") for part in decoded.split("/")), "packages")
    return value


def child_url(base, relative):
    ceph_native.require(isinstance(relative, str) and relative and not relative.startswith("/") and not urllib.parse.urlsplit(relative).scheme, "packages")
    value = safe_url(base.rstrip("/") + "/" + relative)
    ceph_native.require(value.startswith(base.rstrip("/") + "/"), "packages")
    return value


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise ceph_native.Refusal("packages")


class Acquisition:
    """Never use ambient proxies, redirect credentials or unverified HTTPS."""

    def __init__(self, source, certificate, budget=1800):
        ceph_native.require(isinstance(certificate, str) and 0 < len(certificate.encode()) <= 1 << 20, "material")
        self.source = source
        self.deadline = time.monotonic() + budget
        self.total = 0
        self.expanded = 0
        local = ssl.create_default_context(cadata=certificate)
        local.minimum_version = ssl.TLSVersion.TLSv1_2
        self.local = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect(), urllib.request.HTTPSHandler(context=local))
        public = ssl.create_default_context()
        public.minimum_version = ssl.TLSVersion.TLSv1_2
        self.public = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect(), urllib.request.HTTPSHandler(context=public))

    def stream(self, url, output, limit, expected=None, size=None):
        safe_url(url)
        prefix = self.source["url"].rstrip("/") + "/"
        ceph_native.require(url.startswith(prefix) or url.startswith(IBM_BASE + "/") or url == IBM_KEY, "packages")
        remaining = self.deadline - time.monotonic()
        ceph_native.require(remaining > 0, "package-timeout")
        opener = self.local if url.startswith(prefix) else self.public
        request = urllib.request.Request(url, headers={"Accept-Encoding": "identity"})
        hasher, count = hashlib.sha256(), 0
        with opener.open(request, timeout=min(60, remaining)) as response:
            ceph_native.require(response.status == 200 and response.geturl() == url, "packages")
            ceph_native.require(response.headers.get("Content-Encoding", "identity") == "identity", "packages")
            while True:
                ceph_native.require(time.monotonic() < self.deadline, "package-timeout")
                block = response.read(1 << 20)
                if not block:
                    break
                count += len(block)
                self.total += len(block)
                ceph_native.require(count <= limit and self.total <= 4 << 30, "package-limit")
                hasher.update(block)
                output.write(block)
        ceph_native.require(count > 0 and (size is None or count == size) and (expected is None or hasher.hexdigest() == expected), "packages")
        return hasher.hexdigest()

    def bytes(self, url, limit):
        import io

        output = io.BytesIO()
        self.stream(url, output, limit)
        return output.getvalue()

    def file(self, url, path, limit, expected=None, size=None):
        descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
        with os.fdopen(descriptor, "wb") as output:
            return self.stream(url, output, limit, expected, size)

    def repository(self, scratch, identifier, base, signer):
        data = self.bytes(child_url(base, "repodata/repomd.xml"), 1 << 20)
        ceph_native.require(b"<!DOCTYPE" not in data.upper() and b"<!ENTITY" not in data.upper(), "packages")
        tree = ET.fromstring(data)
        ceph_native.require(tree.tag == NAMESPACE + "repomd", "packages")
        records = tree.findall(NAMESPACE + "data")
        ceph_native.require(0 < len(records) <= 32, "packages")
        path = Path(scratch, "repositories", identifier)
        path.mkdir(parents=True, mode=0o700)
        (path / "repodata").mkdir(mode=0o700)
        ceph_native.private_write(str(path / "repodata" / "repomd.xml"), data)
        seen, types = set(), set()
        for record in records:
            kind = record.get("type")
            if kind not in METADATA_TYPES:
                continue
            ceph_native.require(kind not in types, "packages")
            types.add(kind)
            checksum, location, size = (record.find(NAMESPACE + name) for name in ("checksum", "location", "size"))
            ceph_native.require(checksum is not None and location is not None and size is not None, "packages")
            checksum = checksum.text if checksum.get("type") == "sha256" else None
            relative = location.get("href")
            ceph_native.require(ceph_native.digest(checksum) and isinstance(size.text, str) and re.fullmatch(r"[0-9]{1,12}", size.text) is not None, "packages")
            count = int(size.text)
            ceph_native.require(0 < count <= 128 << 20 and isinstance(relative, str) and re.fullmatch(r"repodata/[A-Za-z0-9_.-]{1,240}", relative) is not None
                                and relative not in seen, "packages")
            seen.add(relative)
            member = str(path / PurePosixPath(relative))
            self.file(child_url(base, relative), member, 128 << 20, checksum, count)
            self.metadata_member(member, record, checksum, count)
        ceph_native.require({"primary", "filelists"} <= types, "packages")
        return {"id": identifier, "baseURL": base, "localPath": str(path), "metadataSHA256": hashlib.sha256(data).hexdigest(), "signer": signer}

    def metadata_member(self, path, record, checksum, size):
        opener = open
        # The fixed provider profile accepts codecs with bounded decoder windows.
        # XZ can request an attacker-sized dictionary before emitting any output.
        for suffix, decoder in ((".gz", gzip.open), (".bz2", bz2.open)):
            if path.endswith(suffix):
                opener = decoder
                break
        if opener is not open:
            expected = record.find(NAMESPACE + "open-checksum")
            expanded = record.find(NAMESPACE + "open-size")
            ceph_native.require(expected is not None and expanded is not None and expected.get("type") == "sha256" and ceph_native.digest(expected.text)
                                and re.fullmatch(r"[0-9]{1,12}", expanded.text or "") is not None, "packages")
            checksum, size = expected.text, int(expanded.text)
        else:
            ceph_native.require(path.endswith((".xml", ".yaml")), "packages")
        ceph_native.require(0 < size <= 256 << 20 and self.expanded + size <= 512 << 20, "package-limit")
        count, digest, tail = 0, hashlib.sha256(), b""
        with opener(path, "rb") as source:
            while True:
                ceph_native.require(time.monotonic() < self.deadline, "package-timeout")
                block = source.read(1 << 20)
                if not block:
                    break
                visible = block
                if count == 0 and record.get("type") in ("group", "group_gz"):
                    prolog = COMPS_PROLOG.match(block)
                    visible = block[prolog.end():] if prolog else block
                count += len(block)
                ceph_native.require(count <= size, "package-limit")
                scanned = (tail + visible).upper()
                ceph_native.require(b"<!DOCTYPE" not in scanned and b"<!ENTITY" not in scanned, "packages")
                tail = scanned[-16:]
                digest.update(block)
        ceph_native.require(count == size and digest.hexdigest() == checksum, "packages")
        self.expanded += count

    def repositories(self, scratch):
        identity = self.bytes(child_url(self.source["url"], ".bootwright-tree-identity"), 65)
        ceph_native.require(identity.decode("ascii").strip() == self.source["mediaSHA256"], "packages")
        metadata = self.bytes(child_url(self.source["url"], ".treeinfo"), 1 << 20)
        tree = configparser.ConfigParser(interpolation=None, strict=True)
        tree.read_string(metadata.decode("utf-8"))
        ceph_native.require(tree.get("general", "family") == "Red Hat Enterprise Linux" and tree.get("general", "version") in ("9.8", "9.8.0")
                            and tree.get("general", "arch") == "x86_64", "packages")
        ceph_native.require(tree.get("tree", "arch") == "x86_64"
                            and {"BaseOS", "AppStream"} <= {variant.strip() for variant in tree.get("tree", "variants").split(",")}, "packages")
        repositories = {}
        for variant in ("BaseOS", "AppStream"):
            relative = tree.get("variant-" + variant, "repository")
            ceph_native.require(re.fullmatch(r"[A-Za-z0-9_-]{1,128}(?:/[A-Za-z0-9_-]{1,128}){0,3}", relative) is not None, "packages")
            repositories[variant] = child_url(self.source["url"], relative)
        ceph_native.require(repositories["BaseOS"] != repositories["AppStream"], "packages")
        return [
            self.repository(scratch, "rhel-baseos", repositories["BaseOS"], native_signatures.RHEL_SIGNER),
            self.repository(scratch, "rhel-appstream", repositories["AppStream"], native_signatures.RHEL_SIGNER),
            self.repository(scratch, "ibm-storage-ceph-9", IBM_BASE, native_signatures.IBM_CEPH_SIGNER),
        ]

    def payloads(self, scratch, plan):
        result = {}
        for package in plan["packages"]:
            source = package["source"]
            if package["name"] in ROOT_HASHES:
                ceph_native.require(package["signer"] == native_signatures.IBM_CEPH_SIGNER and source["sha256"] == ROOT_HASHES[package["name"]], "packages")
            ceph_native.require(re.fullmatch(r"native-[0-9a-f]{32}", source["id"]) is not None, "packages")
            path = str(Path(scratch, source["id"] + ".rpm"))
            self.file(source["url"], path, 256 << 20, source["sha256"], source["bytes"])
            result[source["id"]] = path
        return result

    def publisher_key(self, scratch):
        path = str(Path(scratch, "ibm-ceph-publisher.asc"))
        self.file(IBM_KEY, path, 1 << 20)
        return {native_signatures.IBM_CEPH_SIGNER: path}
