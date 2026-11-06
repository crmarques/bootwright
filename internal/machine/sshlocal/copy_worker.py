"""Descriptor-relative filesystem worker for one explicit machine copy."""

from __future__ import annotations

import json
import os
import secrets
import signal
import stat
import sys

LIMIT = 8 * 1024 * 1024
MAX_ENTRIES = 8192
NOFOLLOW = os.O_NOFOLLOW | os.O_CLOEXEC
DIRECTORY = os.O_RDONLY | os.O_DIRECTORY | NOFOLLOW
INPUT = sys.stdin.buffer
OUTPUT = sys.stdout.buffer


def require(condition, reason):
    if not condition:
        raise ValueError(reason)


def emit(value):
    data = json.dumps(value, ensure_ascii=True, separators=(",", ":")).encode()
    require(len(data) <= LIMIT, "manifest-too-large")
    OUTPUT.write(data + b"\n")
    OUTPUT.flush()


def absolute(value):
    require(type(value) is str and value and len(value.encode("utf-8")) <= 4096
            and "\x00" not in value, "invalid-path")
    return os.path.abspath(value)


def relative(value):
    require(type(value) is str and len(value.encode("utf-8")) <= 4096,
            "invalid-entry")
    require(value == "" or all(part not in ("", ".", "..")
                              for part in value.split("/")), "invalid-entry")
    require(not value.startswith("/") and "\x00" not in value, "invalid-entry")
    return value


def token(info):
    return [info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns,
            info.st_ctime_ns, info.st_mode]


def kind(info):
    if stat.S_ISDIR(info.st_mode):
        return "directory"
    require(stat.S_ISREG(info.st_mode), "symlink-or-special-file")
    return "file"


def parent(value, create=()):
    value = absolute(value)
    fd = os.open("/", DIRECTORY)
    walked = ""
    try:
        for part in value.split("/")[1:-1]:
            walked += "/" + part
            try:
                child = os.open(part, DIRECTORY, dir_fd=fd)
            except FileNotFoundError:
                if walked not in create:
                    raise
                safe_parent(fd)
                try:
                    os.mkdir(part, 0o700, dir_fd=fd)
                except FileExistsError:
                    pass
                child = os.open(part, DIRECTORY, dir_fd=fd)
            os.close(fd)
            fd = child
        return fd, os.path.basename(value) or "."
    except BaseException:
        os.close(fd)
        raise


def read_object(parent_fd, name):
    handle = os.open(name, os.O_PATH | NOFOLLOW, dir_fd=parent_fd)
    try:
        before = os.fstat(handle)
        kind(before)
        fd = os.open("/proc/self/fd/" + str(handle),
                     os.O_RDONLY | os.O_NONBLOCK | os.O_CLOEXEC)
        try:
            require(token(os.fstat(fd)) == token(before), "source-changed")
            return fd
        except BaseException:
            os.close(fd)
            raise
    finally:
        os.close(handle)


def safe_parent(fd):
    info = os.fstat(fd)
    require(info.st_uid in (0, os.geteuid()) and
            (not info.st_mode & 0o022 or info.st_mode & stat.S_ISVTX),
            "unsafe-destination-directory")


def inspect(value):
    try:
        fd, name = parent(value)
    except FileNotFoundError:
        return "missing"
    try:
        try:
            return kind(os.stat(name, dir_fd=fd, follow_symlinks=False))
        except FileNotFoundError:
            return "missing"
    finally:
        os.close(fd)


class Worker:
    def __init__(self):
        self.source = None
        self.entries = {}
        self.root = None
        self.directories = set()
        self.overwrite = False
        self.stage = None
        self.created = []

    def scan(self, value):
        require(self.source is None, "already-scanned")
        self.source = absolute(value)
        require(os.path.basename(self.source) not in ("", ".", ".."), "source-root")
        fd, name = parent(self.source)
        result = []
        try:
            self.walk(fd, name, "", result, None)
        finally:
            os.close(fd)
        self.entries = {entry["path"]: entry for entry in result}
        return {"entries": result, "path": self.source}

    def walk(self, parent_fd, name, rel, result, device):
        require(len(result) < MAX_ENTRIES and rel.count("/") < 64, "tree-too-large")
        relative(rel)
        fd = read_object(parent_fd, name)
        try:
            info = os.fstat(fd)
            entry_kind = kind(info)
            require(device is None or info.st_dev == device, "filesystem-crossing")
            result.append({"path": rel, "kind": entry_kind,
                           "mode": stat.S_IMODE(info.st_mode) & 0o777,
                           "size": info.st_size if entry_kind == "file" else 0,
                           "token": token(info)})
            if entry_kind == "directory":
                for child in sorted(os.listdir(fd)):
                    relative(child)
                    self.walk(fd, child, rel + "/" + child if rel else child,
                              result, info.st_dev)
                require(token(os.fstat(fd)) == token(info), "source-changed")
        finally:
            os.close(fd)

    def read(self, rel):
        require(self.source is not None and rel in self.entries, "unknown-source")
        entry = self.entries[rel]
        require(entry["kind"] == "file", "not-file")
        value = self.source + "/" + rel if rel else self.source
        fd, name = parent(value)
        try:
            source = read_object(fd, name)
        finally:
            os.close(fd)
        try:
            require(token(os.fstat(source)) == entry["token"], "source-changed")
            emit({"ok": True})
            remaining = entry["size"]
            while remaining:
                data = os.read(source, min(remaining, 32768))
                if not data:
                    raise EOFError("source-short-read")
                OUTPUT.write(data)
                remaining -= len(data)
            OUTPUT.flush()
            require(token(os.fstat(source)) == entry["token"], "source-changed")
        finally:
            os.close(source)
        return {}

    def prepare(self, request):
        require(self.root is None, "already-prepared")
        self.root = absolute(request["path"])
        self.overwrite = request["overwrite"]
        require(type(self.overwrite) is bool, "invalid-overwrite")
        entries = request["entries"]
        require(type(entries) is list and 0 < len(entries) <= MAX_ENTRIES,
                "invalid-manifest")
        for entry in entries:
            relative(entry["path"])
            require(entry["kind"] in ("directory", "file")
                    and type(entry["size"]) is int and 0 <= entry["size"] < 2**63
                    and type(entry["mode"]) is int and 0 <= entry["mode"] <= 0o777,
                    "invalid-manifest")
            require(entry["path"] not in self.entries, "duplicate-entry")
            self.entries[entry["path"]] = entry
        require("" in self.entries, "missing-root")
        parents = [absolute(p) for p in request["parents"]]
        self.directories = set(parents)
        self.directories.update(self.destination(rel) for rel, entry in self.entries.items()
                                if entry["kind"] == "directory")
        for value in sorted(self.directories, key=len):
            self.check(value, "directory")
        for rel, entry in self.entries.items():
            self.check(self.destination(rel), entry["kind"])
        return {"path": self.root, "parents": parents}

    def destination(self, rel):
        return self.root + "/" + rel if rel else self.root

    def check(self, value, expected):
        try:
            fd, name = parent(value)
        except FileNotFoundError:
            probe = os.path.dirname(value)
            while inspect(probe) == "missing":
                require(probe in self.directories, "missing-parent-directory")
                probe = os.path.dirname(probe)
            require(inspect(probe) == "directory", "parent-not-directory")
            anchor, name = parent(probe)
            try:
                child = os.open(name, DIRECTORY, dir_fd=anchor)
                try:
                    safe_parent(child)
                finally:
                    os.close(child)
            finally:
                os.close(anchor)
            return
        try:
            safe_parent(fd)
            try:
                actual = kind(os.stat(name, dir_fd=fd, follow_symlinks=False))
            except FileNotFoundError:
                return
            require(actual == expected, "destination-type-conflict")
            require(expected == "directory" or self.overwrite, "destination-exists")
        finally:
            os.close(fd)

    def directory(self, value):
        require(value in self.directories, "unknown-directory")
        fd, name = parent(value, self.directories)
        try:
            safe_parent(fd)
            try:
                os.mkdir(name, 0o700, dir_fd=fd)
                child = os.open(name, DIRECTORY, dir_fd=fd)
                self.created.append((value, token(os.fstat(child))[:2]))
                os.close(child)
            except FileExistsError:
                child = os.open(name, DIRECTORY, dir_fd=fd)
                os.close(child)
        finally:
            os.close(fd)
        return {}

    def begin(self, rel):
        require(self.stage is None and rel in self.entries, "unknown-destination")
        entry = self.entries[rel]
        require(entry["kind"] == "file", "not-file")
        fd, name = parent(self.destination(rel), self.directories)
        stage = ".bootwright-cp-" + secrets.token_hex(16)
        private = None
        created = False
        try:
            safe_parent(fd)
            os.mkdir(stage, 0o700, dir_fd=fd)
            created = True
            private = os.open(stage, DIRECTORY, dir_fd=fd)
            info = os.fstat(private)
            require(info.st_uid == os.geteuid() and stat.S_IMODE(info.st_mode) == 0o700,
                    "unsafe-staging-directory")
            target = os.open("payload", os.O_WRONLY | os.O_CREAT | os.O_EXCL | NOFOLLOW,
                             0o600, dir_fd=private)
        except BaseException:
            try:
                if private is not None:
                    self.retire_stage(fd, private, stage)
                elif created:
                    info = os.stat(stage, dir_fd=fd, follow_symlinks=False)
                    if stat.S_ISDIR(info.st_mode) and info.st_uid == os.geteuid():
                        os.rmdir(stage, dir_fd=fd)
            finally:
                if private is not None:
                    os.close(private)
                os.close(fd)
            raise
        self.stage = (fd, private, target, stage, name, entry)
        emit({"ok": True})
        remaining = entry["size"]
        while remaining:
            data = INPUT.read(min(remaining, 32768))
            if not data:
                raise EOFError("short-input")
            offset = 0
            while offset < len(data):
                offset += os.write(target, data[offset:])
            remaining -= len(data)
        os.fchmod(target, entry["mode"])
        os.fsync(target)
        return {}

    def commit(self):
        require(self.stage is not None, "no-stage")
        fd, private, target, stage, name, entry = self.stage
        try:
            current = os.stat(name, dir_fd=fd, follow_symlinks=False)
        except FileNotFoundError:
            current = None
        if current is not None:
            require(kind(current) == "file" and self.overwrite, "destination-exists")
        if self.overwrite:
            os.replace("payload", name, src_dir_fd=private, dst_dir_fd=fd)
        else:
            os.link("payload", name, src_dir_fd=private, dst_dir_fd=fd, follow_symlinks=False)
            os.unlink("payload", dir_fd=private)
        self.retire_stage(fd, private, stage)
        self.stage = None
        os.close(target)
        os.close(private)
        os.close(fd)
        return {}

    def finish(self):
        for value, identity in reversed(self.created):
            parent_fd, name = parent(value)
            try:
                fd = os.open(name, DIRECTORY, dir_fd=parent_fd)
                try:
                    require(token(os.fstat(fd))[:2] == identity, "destination-changed")
                    rel = os.path.relpath(value, self.root)
                    entry = self.entries.get("" if rel == "." else rel)
                    os.fchmod(fd, entry["mode"] if entry else 0o700)
                finally:
                    os.close(fd)
            finally:
                os.close(parent_fd)
        return {}

    def retire_stage(self, parent_fd, private, name):
        try:
            named = os.stat(name, dir_fd=parent_fd, follow_symlinks=False)
        except FileNotFoundError:
            return
        held = os.fstat(private)
        if named.st_dev == held.st_dev and named.st_ino == held.st_ino:
            os.rmdir(name, dir_fd=parent_fd)

    def close(self):
        if self.stage is not None:
            fd, private, target, stage, _, _ = self.stage
            try:
                os.unlink("payload", dir_fd=private)
                self.retire_stage(fd, private, stage)
            except FileNotFoundError:
                pass
            finally:
                os.close(target)
                os.close(private)
                os.close(fd)


def main():
    os.umask(0o077)
    worker = Worker()
    try:
        while True:
            data = INPUT.readline(LIMIT + 2)
            if not data:
                return
            require(len(data) <= LIMIT and data.endswith(b"\n"), "invalid-frame")
            request = json.loads(data)
            operation = request["op"]
            if operation == "inspect":
                response = {"kind": inspect(request["path"])}
            elif operation == "scan":
                response = worker.scan(request["path"])
            elif operation == "prepare":
                response = worker.prepare(request)
            elif operation == "directory":
                response = worker.directory(request["path"])
            elif operation == "read":
                response = worker.read(request["path"])
            elif operation == "begin":
                response = worker.begin(request["path"])
            elif operation == "commit":
                response = worker.commit()
            elif operation == "finish":
                response = worker.finish()
            else:
                raise ValueError("invalid-operation")
            emit(dict(response, ok=True))
    except (ValueError, OSError, EOFError, KeyError, TypeError) as error:
        reason = str(error) if type(error) is ValueError else type(error).__name__
        emit({"ok": False, "error": reason})
    finally:
        worker.close()


def stopped(_number, _frame):
    raise SystemExit(1)


signal.signal(signal.SIGTERM, stopped)
signal.signal(signal.SIGHUP, stopped)
main()
