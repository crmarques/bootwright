"""The fixed remote worker for one Bootwright-owned Linux login."""

from __future__ import annotations

import ctypes
import fcntl
import os
import pwd
import re
import stat
import subprocess
import sys


def execute(*args):
    subprocess.run(args, check=True, stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL,
                   stderr=subprocess.DEVNULL, timeout=15, env={"PATH": "/usr/sbin:/usr/bin", "LC_ALL": "C"})


def directory(path, *, parent=None, owner=0):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=parent)
    attributes = os.fstat(fd)
    if attributes.st_uid != owner or attributes.st_mode & 0o022:
        os.close(fd)
        raise ValueError("unsafe directory")
    return fd


def content(path, parent, owner):
    try:
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, dir_fd=parent)
    except FileNotFoundError:
        return None
    with os.fdopen(fd, "rb") as stream:
        attributes = os.fstat(stream.fileno())
        if not stat.S_ISREG(attributes.st_mode) or attributes.st_uid not in owner or attributes.st_nlink != 1 or attributes.st_mode & 0o022:
            raise ValueError("unsafe file")
        data = stream.read(32769)
        if len(data) > 32768:
            raise ValueError("oversized file")
        return data


def publish(name, expected, parent, owner, group, mode):
    actual = content(name, parent, (owner,))
    if actual == expected:
        return
    if actual is not None:
        raise ValueError("foreign file")
    fd = os.open(".", os.O_WRONLY | os.O_TMPFILE | os.O_CLOEXEC, mode, dir_fd=parent)
    with os.fdopen(fd, "wb") as stream:
        os.fchown(stream.fileno(), owner, group)
        os.fchmod(stream.fileno(), mode)
        stream.write(expected)
        stream.flush()
        os.fsync(stream.fileno())
        # Publish the synced inode at an absent name. A kill leaves either
        # the full grant or no reachable inode and no staged name.
        linkat = ctypes.CDLL(None, use_errno=True).linkat
        if linkat(stream.fileno(), b"", parent, os.fsencode(name), 0x1000) != 0:
            raise OSError(ctypes.get_errno(), "grant publication failed")
    os.fsync(parent)


def login(user, mark, home):
    try:
        entry = pwd.getpwnam(user)
    except KeyError:
        return None
    if entry.pw_uid < 1000 or entry.pw_dir != home or entry.pw_gecos != mark:
        raise ValueError("foreign account")
    return entry


def owned_directory(mode, name, entry, parent=None):
    if mode == "apply":
        try:
            os.mkdir(name, 0o700, dir_fd=parent)
        except FileExistsError:
            pass
    try:
        fd = os.open(name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=parent)
    except FileNotFoundError:
        return None
    try:
        attributes = os.fstat(fd)
        if mode == "apply" and attributes.st_uid == 0 and not os.listdir(fd):
            os.fchown(fd, entry.pw_uid, entry.pw_gid)
            attributes = os.fstat(fd)
        if mode != "apply" and attributes.st_uid == 0 and not os.listdir(fd):
            os.close(fd)
            return None
        if attributes.st_uid != entry.pw_uid or attributes.st_mode & 0o022:
            raise ValueError("foreign home")
        return fd
    except BaseException:
        os.close(fd)
        raise


def grants(mode, entry, expected, home):
    home_fd = owned_directory(mode, home, entry)
    if home_fd is None:
        return False
    try:
        ssh_fd = owned_directory(mode, ".ssh", entry, parent=home_fd)
        if ssh_fd is None:
            return False
        try:
            actual = content("authorized_keys", ssh_fd, (0, entry.pw_uid))
            if mode == "observe":
                return actual is not None and set(expected.splitlines()) <= set(actual.splitlines())
            if actual is not None and actual != expected:
                raise ValueError("foreign authorized keys")
            if mode == "apply":
                if actual is None:
                    publish("authorized_keys", expected, ssh_fd, entry.pw_uid, entry.pw_gid, 0o600)
                if os.path.isfile("/usr/sbin/restorecon"):
                    execute("/usr/sbin/restorecon", home, home + "/.ssh", home + "/.ssh/authorized_keys")
            elif mode == "destroy" and actual is not None:
                os.unlink("authorized_keys", dir_fd=ssh_fd)
                os.fsync(ssh_fd)
            return actual is not None
        finally:
            os.close(ssh_fd)
    finally:
        os.close(home_fd)


def quarantine_home(home, entry):
    parent = directory("/home")
    try:
        archive = retired_directory(parent)
        try:
            name = os.path.basename(home)
            archived = child_attributes(archive, name)
            original = child_attributes(parent, name)
            if archived is not None:
                if not owned_home(archived, entry) or original is not None:
                    raise ValueError("conflicting archived home")
            elif original is not None:
                if not owned_home(original, entry):
                    raise ValueError("foreign home")
                os.rename(name, name, src_dir_fd=parent, dst_dir_fd=archive)
            os.fsync(archive)
            os.fsync(parent)
        finally:
            os.close(archive)
    finally:
        os.close(parent)


def retired_directory(parent):
    try:
        os.mkdir(".bootwright-retired", 0o700, dir_fd=parent)
    except FileExistsError:
        pass
    archive = directory(".bootwright-retired", parent=parent)
    if os.fstat(archive).st_mode & 0o077:
        os.close(archive)
        raise ValueError("archive is accessible to other users")
    return archive


def child_attributes(parent, name):
    try:
        return os.stat(name, dir_fd=parent, follow_symlinks=False)
    except FileNotFoundError:
        return None


def owned_home(attributes, entry):
    return stat.S_ISDIR(attributes.st_mode) and attributes.st_uid in (0, entry.pw_uid)


def reconcile(mode, user, mark, keys, parent):
    home = "/home/" + user + "-" + mark[-16:]
    name = "90-bootwright-" + user
    rule = ("Defaults:%s !requiretty\n%s ALL=(root) NOPASSWD: ALL\n" % (user, user)).encode("ascii")
    entry = login(user, mark, home)
    actual = content(name, parent, (0,))
    if actual is not None and (actual != rule or entry is None):
        raise ValueError("foreign sudo rule")
    if entry is None:
        if mode != "apply":
            if os.path.lexists(home + "/.ssh/authorized_keys"):
                raise ValueError("remaining authorized keys")
            return "absent"
        if os.path.lexists(home):
            raise ValueError("foreign home")
        execute("/usr/sbin/useradd", "-M", "-U", "-p", "!", "-d", home, "-s", "/bin/bash", "-c", mark, "--", user)
        entry = login(user, mark, home)
    ready = grants(mode, entry, keys, home)
    if mode == "apply":
        publish(name, rule, parent, 0, 0, 0o440)
        return "ready"
    if mode == "destroy":
        if actual is not None:
            os.unlink(name, dir_fd=parent)
            os.fsync(parent)
        quarantine_home(home, entry)
        execute("/usr/sbin/userdel", "--", user)
        if login(user, mark, home) is not None or os.path.lexists(home) or content(name, parent, (0,)) is not None:
            raise ValueError("removal unproved")
        return "absent"
    return "ready" if ready and actual is not None else "partial"


def main(argv):
    mode, user, mark, *keys = argv
    if (os.geteuid() != 0 or mode not in ("apply", "observe", "destroy")
            or not re.fullmatch(r"[a-z_][a-z0-9_-]{0,31}", user)
            or not re.fullmatch(r"bootwright-[0-9a-f]{64}", mark) or not 1 <= len(keys) <= 2
            or any(not re.fullmatch(r"[a-zA-Z0-9@._-]+ [a-zA-Z0-9+/=]{1,16000}", key) for key in keys)):
        raise ValueError("invalid account request")
    os.close(directory("/home"))
    parent = directory("/etc/sudoers.d")
    try:
        fcntl.flock(parent, fcntl.LOCK_EX | fcntl.LOCK_NB)
        print(reconcile(mode, user, mark, ("\n".join(keys) + "\n").encode("ascii"), parent))
    finally:
        os.close(parent)


if __name__ == "__main__":
    try:
        main(sys.argv[1:])
    except (OSError, ValueError, subprocess.SubprocessError):
        sys.exit(1)
