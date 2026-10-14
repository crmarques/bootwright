"""Signed inherited crypto-policy facts for the guest Ceph package transaction.

Signed public file facts are retained before package effects and re-proved
against the installed files after the transaction; neither an operator path
nor the image's package database constructs them.
"""
from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

import contextlib
import hashlib
import os
import re
import stat

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native, native_signatures

GENERATOR = "/usr/share/crypto-policies/python/update-crypto-policies.py"
TARGETS = frozenset("/usr/share/crypto-policies/" + name + "/opensshserver.txt" for name in ("DEFAULT", "FUTURE", "FIPS", "LEGACY"))
FILES = TARGETS | {GENERATOR}
PACKAGES = frozenset(("crypto-policies", "crypto-policies-scripts"))
MAX_FILE = 1 << 20
# A signed package may hardlink identical policy files; its header states each link count.
MAX_LINKS = 8
VERSION = "ceph-crypto-policy-v1"
OWNER = 0


def require(condition):
    ceph_native.require(condition, "native-tunnel-unproved")


def scope(request, host, source):
    return {"cluster": ceph_native.marker_identity(request), "host": host, "source": source}


def selected(plan):
    result = [value for value in plan["packages"] if value["name"] in PACKAGES]
    require(len(result) == 2 and {value["name"] for value in result} == PACKAGES)
    require(all(value["signer"] == native_signatures.RHEL_SIGNER for value in result))
    return sorted(result, key=lambda value: value["name"])


def header_files(output):
    require(isinstance(output, bytes) and 0 < len(output) <= 256 << 10)
    lines = output.decode("ascii").splitlines()
    require(lines[0] == "algo|8" and len(lines) <= 16384)
    result = {}
    for line in lines[1:]:
        values = line.split("|")
        if values[0] not in FILES:
            continue
        require(len(values) == 7 and values[0] not in result and re.fullmatch(r"[0-9a-f]{64}", values[1]))
        require(values[2].isdigit() and values[3:5] == ["root", "root"] and values[5].isdigit() and values[6].isdigit())
        mode, flags, links = int(values[2]), int(values[5]), int(values[6])
        require(stat.S_ISREG(mode) and mode & 0o022 == 0 and not flags & (1 | 64) and 0 < links <= MAX_LINKS)
        result[values[0]] = {"sha256": values[1], "mode": mode, "uid": 0, "gid": 0, "links": links}
    return result


def capture(plan, payloads, headers, original_scope, binding, query):
    """Called only after exact payload digest and isolated signer verification."""
    files, packages = {}, []
    for package in selected(plan):
        identifier = package["source"]["id"]
        require(package["source"]["url"].startswith(original_scope["source"]["url"].rstrip("/") + "/"))
        require(ceph_native.digest(headers[identifier]))
        output = query(["-qp", "--nosignature", "--qf",
                        "algo|%{FILEDIGESTALGO}\\n[%{FILENAMES}|%{FILEDIGESTS}|%{FILEMODES}|%{FILEUSERNAME}|%{FILEGROUPNAME}|%{FILEFLAGS}|%{FILENLINKS}\\n]",
                        "--", payloads[identifier]])
        owned = header_files(output)
        require(not files.keys() & owned.keys())
        files.update(owned)
        packages.append({"identity": {name: package[name] for name in ("name", "epoch", "version", "release", "architecture")},
                         "sourceID": identifier, "headerSHA256": headers[identifier]})
    require(set(files) == FILES)
    return {"version": VERSION, "scope": original_scope, "binding": binding,
            "planDigest": plan["digest"], "packages": packages, "files": files}


def validate(record, plan, headers, original_scope, binding):
    require(isinstance(record, dict) and set(record) == {"version", "scope", "binding", "planDigest", "packages", "files"})
    require(record["version"] == VERSION and ceph_native.canonical(record["scope"]) == ceph_native.canonical(original_scope))
    require(record["binding"] == binding and record["planDigest"] == plan["digest"])
    expected = [{"identity": {name: p[name] for name in ("name", "epoch", "version", "release", "architecture")},
                 "sourceID": p["source"]["id"], "headerSHA256": headers[p["source"]["id"]]} for p in selected(plan)]
    require(ceph_native.canonical(record["packages"]) == ceph_native.canonical(expected))
    require(isinstance(record["files"], dict) and set(record["files"]) == FILES)
    for value in record["files"].values():
        require(isinstance(value, dict) and set(value) == {"sha256", "mode", "uid", "gid", "links"})
        require(type(value["links"]) is int and 0 < value["links"] <= MAX_LINKS)
        require(ceph_native.digest(value["sha256"]) and type(value["mode"]) is int and stat.S_ISREG(value["mode"]) and value["mode"] & 0o022 == 0)
        require(type(value["uid"]) is int and value["uid"] == 0 and type(value["gid"]) is int and value["gid"] == 0)


def identity(info):
    return tuple(getattr(info, name) for name in ("st_dev", "st_ino", "st_uid", "st_gid", "st_mode", "st_nlink", "st_size", "st_mtime_ns", "st_ctime_ns"))


def root_descriptor():
    return os.open("/", os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)


@contextlib.contextmanager
def parent(path):
    require(path in FILES)
    with contextlib.ExitStack() as stack:
        descriptor = root_descriptor()
        stack.callback(os.close, descriptor)
        bindings = []
        for name in path.strip("/").split("/")[:-1]:
            info = os.fstat(descriptor)
            require(stat.S_ISDIR(info.st_mode) and info.st_uid == OWNER and info.st_gid == OWNER and info.st_mode & 0o022 == 0)
            child = os.open(name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=descriptor)
            stack.callback(os.close, child)
            bindings.append((descriptor, name, child, identity(os.fstat(child))))
            descriptor = child
        info = os.fstat(descriptor)
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == OWNER and info.st_gid == OWNER and info.st_mode & 0o022 == 0)

        def prove():
            require(all(identity(os.fstat(child)) == before and identity(os.stat(name, dir_fd=owner, follow_symlinks=False)) == before
                        for owner, name, child, before in bindings))
        prove()
        yield descriptor, prove
        prove()


@contextlib.contextmanager
def file_source(path, expected):
    # The wrapped context closes its descriptors on any exit; the proof after the yield is deliberately success-only.
    # pylint: disable-next=contextmanager-generator-missing-cleanup
    with parent(path) as (directory, prove):
        name = path.rsplit("/", 1)[-1]
        fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, dir_fd=directory)
        with os.fdopen(fd, "rb") as handle:
            info, data = os.fstat(fd), None
            before = identity(info)
            require(stat.S_ISREG(info.st_mode) and info.st_uid == OWNER and info.st_gid == OWNER and info.st_nlink == expected["links"])
            require(info.st_mode == expected["mode"] and 0 < info.st_size <= MAX_FILE)
            prove()
            require(identity(os.stat(name, dir_fd=directory, follow_symlinks=False)) == before)
            data = handle.read(MAX_FILE + 1)
            require(len(data) == info.st_size and hashlib.sha256(data).hexdigest() == expected["sha256"])
            yield data
            prove()
            require(identity(os.fstat(fd)) == before and identity(os.stat(name, dir_fd=directory, follow_symlinks=False)) == before)


def read_file(path, expected):
    with file_source(path, expected) as data:
        return data


def installed(record, query):
    for package in record["packages"]:
        nevra = "{name}-{epoch}:{version}-{release}.{architecture}".format(**package["identity"])
        require(query(["-q", "--qf", "%{SHA256HEADER}\\n", "--", nevra]) == (package["headerSHA256"] + "\n").encode())
        require(query(["--verify", "--noscripts", "--noconfig", "--noghost", "--", nevra]) == b"")
    for path, value in record["files"].items():
        read_file(path, value)
