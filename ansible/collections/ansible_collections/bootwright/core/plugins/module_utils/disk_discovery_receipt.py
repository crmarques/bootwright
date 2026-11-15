"""Private, attempt-owned Unix HTTP receiver behind the artifact HTTPS listener."""

from __future__ import annotations

from contextlib import contextmanager
import ctypes
import errno
import hashlib
import hmac
import http.server
import os
import re
from pathlib import Path
import socket
import stat

from ansible_collections.bootwright.core.plugins.module_utils import absolute_deadline
from ansible_collections.bootwright.core.plugins.module_utils import private_records as records

MAX_REPORT = 48 << 10


@contextmanager
def socket_context(label):
    """Label only the new socket peer, without changing process/domain policy."""
    if not Path("/sys/fs/selinux/enforce").exists():
        if label:
            raise ValueError("discovery receipt SELinux state")
        yield
        return
    if re.fullmatch(r"[a-z_]+:system_r:container_t:s0(?::c[0-9]{1,4}(?:,c[0-9]{1,4})*)?", label) is None:
        raise ValueError("discovery receipt SELinux label")
    # libselinux is part of the supported RHEL host. Its API preserves the
    # previous thread-local socket context, even when socket creation refuses.
    library = ctypes.CDLL("libselinux.so.1", use_errno=True)
    library.getsockcreatecon_raw.argtypes = [ctypes.POINTER(ctypes.c_char_p)]
    library.setsockcreatecon_raw.argtypes = [ctypes.c_char_p]
    library.freecon.argtypes = [ctypes.c_void_p]
    previous = ctypes.c_char_p()
    if library.getsockcreatecon_raw(ctypes.byref(previous)) != 0:
        raise OSError(ctypes.get_errno(), "discovery receipt socket context")
    try:
        if library.setsockcreatecon_raw(label.encode("ascii")) != 0:
            raise OSError(ctypes.get_errno(), "discovery receipt socket context")
        try:
            yield
        finally:
            if library.setsockcreatecon_raw(previous) != 0:
                raise OSError(ctypes.get_errno(), "discovery receipt socket context reset")
    finally:
        library.freecon(previous)


class Handler(http.server.BaseHTTPRequestHandler):
    """No request, credential, report or exception is written to a log."""

    def log_message(self, format, *args):
        pass

    def answer(self, code, body=b""):
        self.send_response(code)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_POST(self):
        headers = self.headers
        lengths = headers.get_all("Content-Length", [])
        auth = headers.get_all("Authorization", [])
        if (self.path != "/report" or len(auth) != 1
                or not hmac.compare_digest(auth[0], "Bearer " + self.server.token)):
            return self.answer(403)
        if (headers.get_all("Transfer-Encoding") or len(lengths) != 1
                or not lengths[0].isascii() or not lengths[0].isdigit()
                or len(lengths[0]) > 5 or not 0 < int(lengths[0]) <= MAX_REPORT):
            return self.answer(400)
        data = self.rfile.read(int(lengths[0]))
        if len(data) != int(lengths[0]):
            return self.answer(400)
        try:
            # The owner validates the closed binding and canonical bounded wire
            # before an immutable, fsynced write. An identical retry is harmless.
            self.server.owner.validate_report(data)
            existing = self.server.owner.report()
            if existing is None:
                records.write(self.server.owner.root / "report.json", data)
            elif existing != data:
                return self.answer(409)
            # A previous write may have linked the immutable receipt and then
            # failed its directory fsync. Replay must complete that durability
            # boundary too, before any acknowledgement crosses the proxy.
            held = os.open(self.server.owner.root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
            try:
                os.fsync(held)
            finally:
                os.close(held)
        except ValueError:
            return self.answer(400)
        self.answer(200, hashlib.sha256(data).hexdigest().encode("ascii"))


class Receiver(http.server.HTTPServer):
    """Synchronous service, held through verified worker power-off for ACK retry."""

    address_family = socket.AF_UNIX
    allow_reuse_address = False

    def __init__(self, owner):
        self.owner = owner
        state = owner.load()
        self.token = state["reportToken"]
        self.path = Path(owner.request["receipt"]["socketRoot"]) / state["token"][:32]
        records.directory(self.path.parent, False, 0o711)
        if len(os.fsencode(self.path)) >= 104:
            raise ValueError("discovery receipt socket path")
        self.reap_stale()
        with socket_context(owner.receipt_label()):
            super().__init__(str(self.path), Handler, bind_and_activate=False)
        try:
            previous = os.umask(0o077)
            try:
                self.socket.bind(str(self.path))
            finally:
                os.umask(previous)
            self.inode = self.path.lstat().st_ino
            os.chmod(self.path, 0o660)
            # The read-only container mount labels its runtime directory; a
            # freshly created socket must carry that same private MCS label.
            owner.label_receipt(self.path)
            self.socket.listen(8)
            self.timeout = 1
        except BaseException:
            self.server_close()
            raise

    def reap_stale(self):
        try:
            info = self.path.lstat()
        except FileNotFoundError:
            return
        if (not stat.S_ISSOCK(info.st_mode) or info.st_uid != 0 or info.st_gid != 0
                or stat.S_IMODE(info.st_mode) not in (0o700, 0o660) or info.st_nlink != 1):
            raise ValueError("foreign discovery receipt socket")
        # The caller holds this durable attempt's exclusive custody lock. Still
        # refuse a live socket; never displace another process's receiver.
        with socket.socket(socket.AF_UNIX) as probe:
            probe.settimeout(1)
            try:
                probe.connect(str(self.path))
            except OSError as error:
                if error.errno != errno.ECONNREFUSED:
                    raise
            else:
                raise ValueError("discovery receipt socket is active")
        if self.path.lstat() != info:
            raise ValueError("discovery receipt socket changed")
        self.path.unlink()

    def get_request(self):
        connection, _address = self.socket.accept()
        connection.settimeout(10)
        return connection, ("local", 0)

    def poll(self, seconds=10):
        try:
            with absolute_deadline.limited(seconds):
                self.handle_request()
        except TimeoutError:
            pass

    def handle_error(self, request, client_address):
        pass

    def server_close(self):
        super().server_close()
        if getattr(self, "inode", None) is not None:
            info = self.path.lstat()
            if info.st_ino != self.inode or not stat.S_ISSOCK(info.st_mode):
                raise ValueError("discovery receipt socket changed")
            self.path.unlink()


def remove_stale(owner):
    """Removal never needs to create a listener or revisit installed machines."""
    state = owner.load()
    path = Path(owner.request["receipt"]["socketRoot"]) / state["token"][:32]
    if not path.parent.exists():
        return
    records.directory(path.parent, False, 0o711)
    receiver = Receiver.__new__(Receiver)
    receiver.path = path
    receiver.reap_stale()
