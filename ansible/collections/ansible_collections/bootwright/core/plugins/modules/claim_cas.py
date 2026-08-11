from __future__ import annotations

import errno
import fcntl
import hashlib
import os
import secrets
import stat

from ansible.module_utils.basic import AnsibleModule


def _normalized_absolute(value):
    return (
        isinstance(value, str)
        and value.startswith("/")
        and value != "/"
        and os.path.normpath(value) == value
    )


def _normalized_root(value):
    return isinstance(value, str) and value.startswith("/") and os.path.normpath(value) == value


def _digest(value):
    return hashlib.sha256(value).hexdigest()


def _open_directory(path):
    before = os.lstat(path)
    if not stat.S_ISDIR(before.st_mode) or stat.S_ISLNK(before.st_mode):
        raise ValueError(f"claim CAS parent {path} is not a directory that is not a symlink")
    flags = os.O_RDONLY | getattr(os, "O_DIRECTORY", 0) | getattr(os, "O_CLOEXEC", 0) | getattr(os, "O_NOFOLLOW", 0)
    descriptor = os.open(path, flags)
    opened = os.fstat(descriptor)
    if (opened.st_dev, opened.st_ino) != (before.st_dev, before.st_ino):
        os.close(descriptor)
        raise ValueError(f"claim CAS parent {path} changed while it was opened")
    return descriptor


def _require_safe_directory(descriptor, display_path):
    identity = os.fstat(descriptor)
    if (
        not stat.S_ISDIR(identity.st_mode)
        or identity.st_uid != os.geteuid()
        or identity.st_gid != os.getegid()
        or stat.S_IMODE(identity.st_mode) & 0o022
    ):
        raise ValueError(
            f"claim CAS parent {display_path} is not a caller-owned non-writable directory"
        )


def _open_directory_at(parent_descriptor, name, display_path):
    before = os.stat(name, dir_fd=parent_descriptor, follow_symlinks=False)
    if not stat.S_ISDIR(before.st_mode) or stat.S_ISLNK(before.st_mode):
        raise ValueError(
            f"claim CAS parent {display_path} is not a directory that is not a symlink"
        )
    flags = (
        os.O_RDONLY
        | getattr(os, "O_DIRECTORY", 0)
        | getattr(os, "O_CLOEXEC", 0)
        | getattr(os, "O_NOFOLLOW", 0)
    )
    descriptor = os.open(name, flags, dir_fd=parent_descriptor)
    opened = os.fstat(descriptor)
    if (opened.st_dev, opened.st_ino) != (before.st_dev, before.st_ino):
        os.close(descriptor)
        raise ValueError(f"claim CAS parent {display_path} changed while it was opened")
    return descriptor


def _walk_directory(path, trusted_root, create):
    if not _normalized_root(trusted_root):
        raise ValueError(
            f"claim CAS trusted root {trusted_root!r} is not normalized and absolute"
        )
    try:
        within_root = os.path.commonpath([path, trusted_root]) == trusted_root
    except ValueError:
        within_root = False
    if not within_root:
        raise ValueError(f"claim CAS parent {path} is outside trusted root {trusted_root}")
    descriptor = _open_directory(trusted_root)
    try:
        _require_safe_directory(descriptor, trusted_root)
        relative = os.path.relpath(path, trusted_root)
        if relative == ".":
            return descriptor
        current_path = trusted_root
        for name in relative.split(os.sep):
            current_path = os.path.join(current_path, name)
            try:
                child = _open_directory_at(descriptor, name, current_path)
            except FileNotFoundError:
                if not create:
                    raise
                try:
                    os.mkdir(name, 0o700, dir_fd=descriptor)
                    os.fsync(descriptor)
                except FileExistsError:
                    pass
                child = _open_directory_at(descriptor, name, current_path)
            try:
                _require_safe_directory(child, current_path)
            except BaseException:
                os.close(child)
                raise
            os.close(descriptor)
            descriptor = child
        return descriptor
    except BaseException:
        os.close(descriptor)
        raise


def _read_regular(parent_descriptor, name, display_path):
    try:
        before = os.stat(name, dir_fd=parent_descriptor, follow_symlinks=False)
    except FileNotFoundError:
        return None, None
    if not stat.S_ISREG(before.st_mode) or stat.S_ISLNK(before.st_mode):
        raise ValueError(f"{display_path} is not a regular non-symlink file")
    flags = os.O_RDONLY | getattr(os, "O_CLOEXEC", 0) | getattr(os, "O_NOFOLLOW", 0)
    descriptor = os.open(name, flags, dir_fd=parent_descriptor)
    try:
        opened = os.fstat(descriptor)
        if (opened.st_dev, opened.st_ino) != (before.st_dev, before.st_ino):
            raise ValueError(f"{display_path} changed while it was opened")
        with os.fdopen(descriptor, "rb", closefd=False) as stream:
            content = stream.read()
    finally:
        os.close(descriptor)
    after = os.stat(name, dir_fd=parent_descriptor, follow_symlinks=False)
    if (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns) != (
        before.st_dev,
        before.st_ino,
        before.st_size,
        before.st_mtime_ns,
    ):
        raise ValueError(f"{display_path} changed while it was read")
    return content, before


def _same_target(parent_descriptor, name, display_path, expected_stat, expected_content):
    current, current_stat = _read_regular(parent_descriptor, name, display_path)
    if expected_stat is None:
        return current is None
    if current_stat is None:
        return False
    return (
        (current_stat.st_dev, current_stat.st_ino)
        == (expected_stat.st_dev, expected_stat.st_ino)
        and current == expected_content
    )


def _candidate_name():
    return ".bootwright-claim-cas-" + secrets.token_hex(16)


def claim_cas(
    path,
    state,
    desired_content,
    expected_contents,
    allow_absent,
    lock_path,
    create_parents=False,
    trusted_root="/",
):
    for value in [path, lock_path]:
        if not _normalized_absolute(value):
            raise ValueError(f"claim CAS path {value!r} is not normalized and absolute")
    if path == lock_path:
        raise ValueError("claim CAS target and lock paths must be different")
    parent = os.path.dirname(path)
    lock_parent = os.path.dirname(lock_path)
    lock_parent_descriptor = None
    parent_descriptor = None
    lock_descriptor = None
    try:
        lock_parent_descriptor = _walk_directory(
            lock_parent,
            trusted_root,
            create_parents,
        )
    except FileNotFoundError as error:
        raise ValueError(f"claim CAS parent {lock_parent} does not exist") from error
    path_name = os.path.basename(path)
    lock_name = os.path.basename(lock_path)
    common_lock_flags = os.O_RDWR | getattr(os, "O_CLOEXEC", 0) | getattr(os, "O_NOFOLLOW", 0)
    try:
        try:
            lock_descriptor = os.open(
                lock_name,
                common_lock_flags | os.O_CREAT | os.O_EXCL,
                0o600,
                dir_fd=lock_parent_descriptor,
            )
            os.fchmod(lock_descriptor, 0o600)
            os.fsync(lock_descriptor)
            os.fsync(lock_parent_descriptor)
        except FileExistsError:
            lock_descriptor = os.open(
                lock_name,
                common_lock_flags,
                dir_fd=lock_parent_descriptor,
            )
        lock_identity = os.fstat(lock_descriptor)
        lock_path_identity = os.stat(lock_name, dir_fd=lock_parent_descriptor, follow_symlinks=False)
        if (
            not stat.S_ISREG(lock_identity.st_mode)
            or stat.S_ISLNK(lock_path_identity.st_mode)
            or (lock_identity.st_dev, lock_identity.st_ino)
            != (lock_path_identity.st_dev, lock_path_identity.st_ino)
            or lock_identity.st_uid != os.geteuid()
            or lock_identity.st_gid != os.getegid()
            or stat.S_IMODE(lock_identity.st_mode) != 0o600
            or lock_identity.st_nlink != 1
        ):
            raise ValueError(
                f"claim CAS lock {lock_path} is not one caller-owned 0600 regular inode"
            )
        try:
            fcntl.flock(lock_descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except OSError as error:
            if error.errno in [errno.EACCES, errno.EAGAIN]:
                raise ValueError(
                    f"claim CAS lock {lock_path} is held by another mutation"
                ) from error
            raise
        lock_path_identity = os.stat(lock_name, dir_fd=lock_parent_descriptor, follow_symlinks=False)
        if (
            (lock_identity.st_dev, lock_identity.st_ino)
            != (lock_path_identity.st_dev, lock_path_identity.st_ino)
            or lock_path_identity.st_nlink != 1
        ):
            raise ValueError(f"claim CAS lock {lock_path} changed while held")
        try:
            parent_descriptor = _walk_directory(
                parent,
                trusted_root,
                create_parents and state == "present",
            )
        except FileNotFoundError as error:
            if state == "absent" and allow_absent:
                return False, "", ""
            raise ValueError(f"claim CAS parent {parent} does not exist") from error
        current, current_stat = _read_regular(parent_descriptor, path_name, path)
        desired = desired_content.encode("utf-8") if desired_content is not None else None
        expected = {item.encode("utf-8") for item in expected_contents}
        if current is None:
            if state == "absent" and allow_absent:
                return False, "", ""
            if not allow_absent:
                raise ValueError(f"claim CAS target {path} is absent but absence was not authorized")
        elif state == "present" and current == desired:
            digest = _digest(current)
            return False, digest, digest
        elif current not in expected:
            raise ValueError(
                f"claim CAS target {path} has unexpected sha256 {_digest(current)}"
            )
        previous = "" if current is None else _digest(current)
        if state == "absent":
            if not _same_target(parent_descriptor, path_name, path, current_stat, current):
                raise ValueError(f"claim CAS target {path} changed before removal")
            os.unlink(path_name, dir_fd=parent_descriptor)
            os.fsync(parent_descriptor)
            return True, previous, ""
        candidate = _candidate_name()
        flags = os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, "O_CLOEXEC", 0) | getattr(os, "O_NOFOLLOW", 0)
        descriptor = os.open(candidate, flags, 0o600, dir_fd=parent_descriptor)
        try:
            os.fchmod(descriptor, 0o600)
            with os.fdopen(descriptor, "wb", closefd=False) as stream:
                stream.write(desired)
                stream.flush()
                os.fsync(descriptor)
            os.close(descriptor)
            descriptor = -1
            if not _same_target(parent_descriptor, path_name, path, current_stat, current):
                raise ValueError(f"claim CAS target {path} changed before replacement")
            if current_stat is None:
                os.link(
                    candidate,
                    path_name,
                    src_dir_fd=parent_descriptor,
                    dst_dir_fd=parent_descriptor,
                    follow_symlinks=False,
                )
                os.unlink(candidate, dir_fd=parent_descriptor)
            else:
                os.replace(
                    candidate,
                    path_name,
                    src_dir_fd=parent_descriptor,
                    dst_dir_fd=parent_descriptor,
                )
            candidate = ""
            os.fsync(parent_descriptor)
        finally:
            if descriptor >= 0:
                os.close(descriptor)
            if candidate:
                try:
                    os.unlink(candidate, dir_fd=parent_descriptor)
                except FileNotFoundError:
                    pass
        return True, previous, _digest(desired)
    finally:
        if lock_descriptor is not None:
            try:
                fcntl.flock(lock_descriptor, fcntl.LOCK_UN)
            finally:
                os.close(lock_descriptor)
        if lock_parent_descriptor is not None:
            os.close(lock_parent_descriptor)
        if parent_descriptor is not None:
            os.close(parent_descriptor)


def main():
    module = AnsibleModule(
        argument_spec={
            "path": {"type": "path", "required": True},
            "state": {"type": "str", "choices": ["present", "absent"], "required": True},
            "desired_content": {"type": "str", "default": None},
            "expected_contents": {"type": "list", "elements": "str", "default": []},
            "allow_absent": {"type": "bool", "default": False},
            "lock_path": {
                "type": "path",
                "default": "/var/lib/bootwright/shared-services/.claim-cas.lock",
            },
            "create_parents": {"type": "bool", "default": False},
            "trusted_root": {"type": "path", "default": "/"},
        },
        supports_check_mode=False,
    )
    state = module.params["state"]
    desired = module.params["desired_content"]
    if state == "present" and desired is None:
        module.fail_json(msg="claim CAS state=present requires desired_content")
    if state == "absent" and desired is not None:
        module.fail_json(msg="claim CAS state=absent does not accept desired_content")
    if os.geteuid() != 0 or os.getegid() != 0:
        module.fail_json(msg="claim CAS must run as root so its persistent lock is root:root")
    try:
        changed, previous, current = claim_cas(
            module.params["path"],
            state,
            desired,
            module.params["expected_contents"],
            module.params["allow_absent"],
            module.params["lock_path"],
            module.params["create_parents"],
            module.params["trusted_root"],
        )
    except (OSError, ValueError) as error:
        module.fail_json(msg=str(error), changed=False)
    module.exit_json(
        changed=changed,
        previous_sha256=previous,
        current_sha256=current,
    )


if __name__ == "__main__":
    main()
