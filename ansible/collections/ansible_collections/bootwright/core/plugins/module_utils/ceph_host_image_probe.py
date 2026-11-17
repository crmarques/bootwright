"""Reads the frozen Ceph image's own artifacts and parser for the host profile; the source runs alone inside the image."""

from __future__ import annotations

import base64
import hashlib
import json
import os
import pwd
import stat
import sys
import tempfile


def file(path, payload=False):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC)
    with os.fdopen(fd, "rb") as handle:
        before = os.fstat(handle.fileno())
        if (not stat.S_ISREG(before.st_mode) or before.st_uid != 0 or
                before.st_mode & 0o022 or before.st_nlink != 1 or not 0 < before.st_size <= 64 << 20):
            raise ValueError("artifact")
        digest, data, count = hashlib.sha256(), bytearray(), 0
        while True:
            chunk = handle.read(65536)
            if not chunk:
                break
            count += len(chunk)
            if count > before.st_size or payload and count > 2 << 20:
                raise ValueError("artifact")
            digest.update(chunk)
            if payload:
                data.extend(chunk)
        fields = ("st_dev", "st_ino", "st_uid", "st_gid", "st_mode", "st_nlink", "st_size", "st_mtime_ns", "st_ctime_ns")
        expected = tuple(getattr(before, key) for key in fields)
        if count != before.st_size or any(tuple(getattr(info, key) for key in fields) != expected
                                          for info in (os.fstat(handle.fileno()), os.stat(path, follow_symlinks=False))):
            raise ValueError("artifact")
        result = {"sha256": digest.hexdigest(), "bytes": count, "mode": stat.S_IMODE(before.st_mode),
                  "uid": before.st_uid, "gid": before.st_gid}
        return result, base64.b64encode(data).decode() if payload else None


def collect():
    import asyncssh
    account = pwd.getpwnam("ceph")
    if account.pw_uid <= 0 or account.pw_uid != account.pw_gid or account.pw_uid >= 65536:
        raise ValueError("account")
    artifacts, payload = {}, None
    paths = ("/usr/sbin/cephadm", "/usr/share/ceph/mgr/cephadm/ssh.py", "/usr/share/ceph/mgr/cephadm/module.py",
             "/usr/share/ceph/mgr/cephadm/serve.py", "/usr/bin/ceph", "/usr/bin/ceph-authtool")
    for path in paths:
        artifact, body = file(path, payload=path == "/usr/sbin/cephadm")
        artifacts[path] = artifact
        if body is not None:
            payload = body
    with tempfile.TemporaryDirectory(prefix="bw-parser-", dir="/tmp") as directory:
        path = directory + "/ssh_config"
        with open(path, "x") as destination:
            destination.write("Host *\n  ProxyCommand /bin/false\n")
        options = asyncssh.SSHClientConnectionOptions(host="192.0.2.1", username="bootwright",
                                                      client_keys=[], known_hosts=None, config=[path])
        if options.proxy_command != ["/bin/false"]:
            raise ValueError("parser")
    policy_path = os.path.realpath("/etc/crypto-policies/back-ends/openssh.config")
    if not policy_path.startswith(("/etc/crypto-policies/", "/usr/share/crypto-policies/")):
        raise ValueError("policy")
    fd = os.open(policy_path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC)
    with os.fdopen(fd, "rb") as handle:
        info = os.fstat(handle.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022 or info.st_size > 65536:
            raise ValueError("policy")
        policy = handle.read(65537)
        if len(policy) != info.st_size or len(policy) > 65536:
            raise ValueError("policy")
        policy = policy.decode("ascii")
    return {"version": "ceph-host-image-v1", "uid": os.getuid(), "gid": os.getgid(),
            "cephUID": account.pw_uid, "cephGID": account.pw_gid, "machine": os.uname().machine,
            "artifacts": artifacts, "cephadm": payload, "cryptoPolicy": policy}


def main():
    try:
        sys.stdout.write(json.dumps(collect(), sort_keys=True, separators=(",", ":")) + "\n")
    except Exception:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
