"""Fixed controller artifact acquisition and immutable publication."""

from __future__ import annotations

import contextlib
import ctypes
import functools
import gzip
import hashlib
import io
import ipaddress
import json
import os
import posixpath
import re
import signal
import ssl
import stat
import tarfile
import time
import urllib.parse

MAX_SOURCE = 1 << 30
MAX_EXPANDED = 2 << 30
MAX_ENTRIES = 4096
MAX_BUNDLE = 8 << 30
MAX_DEADLINE = 7200
CHUNK = 64 << 10
DOWNLOAD_SECONDS = 300
AT_EMPTY_PATH = 0x1000
# A released oc names its release in its bytes: the version, NUL-terminated,
# overwrites the head of this marker. An unstamped oc carries it whole.
RELEASE_MARKER = b"\x00_RELEASE_VERSION_LOCATION_\x00" + b"X" * 64 + b"\x00"
DIRECTORY_FLAGS = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC
FILE_FLAGS = os.O_RDONLY | os.O_NONBLOCK | os.O_NOFOLLOW | os.O_CLOEXEC
REDIRECT_HOSTS = frozenset(
    (
        "github.com",
        "release-assets.githubusercontent.com",
        "objects.githubusercontent.com",
        "get.helm.sh",
        "mirror.openshift.com",
        "dl.k8s.io",
        "cdn.dl.k8s.io",
        "files.pythonhosted.org",
        "cdn-ubi.redhat.com",
        "dl.fedoraproject.org",
    )
)


class Refused(ValueError):
    """A bounded operation lacks the required authority or integrity proof."""


class Unreleased(Refused):
    """The release-stamp check found an oc that does not name its frozen
    release. It is the one refusal Go remedies by name, because the same
    release and mirror reuse the retained source and refuse again."""


def canonical(value):
    return json.dumps(
        value, sort_keys=True, separators=(",", ":"), ensure_ascii=True
    ).encode()


def sha256(data):
    return hashlib.sha256(data).hexdigest()


def relative(value):
    if not isinstance(value, str) or not value or len(value) > 4096:
        raise Refused("relative path")
    parts = value.split("/")
    if len(parts) > 32 or any(
        part in ("", ".", "..") or len(part) > 255 for part in parts
    ):
        raise Refused("relative path")
    if any(ord(character) < 32 or ord(character) >= 127 for character in value):
        raise Refused("relative path")
    return parts


def endpoint(value, redirect=False, proxy=False):
    if (
        not isinstance(value, str)
        or len(value) > 4096
        or any(ord(c) <= 32 or ord(c) >= 127 for c in value)
    ):
        raise Refused("HTTPS source")
    parsed = urllib.parse.urlsplit(value)
    if (
        parsed.scheme not in (("http", "https") if proxy else ("https",))
        or not parsed.hostname
    ):
        raise Refused("HTTPS source")
    if (
        parsed.username is not None
        or parsed.password is not None
        or parsed.fragment
        or parsed.query
        and not redirect
    ):
        raise Refused("HTTPS source")
    if (
        not proxy
        and parsed.port not in (None, 443)
        or proxy
        and parsed.path not in ("", "/")
    ):
        raise Refused("HTTPS source")
    return parsed


def source_identity(source):
    if not isinstance(source, dict) or set(source) != {"id", "url", "sha256", "bytes"}:
        raise Refused("source identity")
    if (
        not isinstance(source["id"], str)
        or re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._+~:-]{0,255}", source["id"]) is None
    ):
        raise Refused("source identity")
    endpoint(source["url"])
    if (
        not isinstance(source["sha256"], str)
        or re.fullmatch(r"[a-f0-9]{64}", source["sha256"]) is None
    ):
        raise Refused("source identity")
    if (
        not isinstance(source["bytes"], int)
        or isinstance(source["bytes"], bool)
        or not 0 < source["bytes"] <= MAX_SOURCE
    ):
        raise Refused("source identity")


def bypass_matches(rule, target):
    if not isinstance(rule, str) or not rule or len(rule) > 1024:
        raise Refused("proxy bypass")
    if rule == "*":
        return True
    host = target.hostname.lower()
    try:
        network = ipaddress.ip_network(rule, strict=False)
    except ValueError:
        network = None
    if network is not None:
        try:
            return ipaddress.ip_address(host) in network
        except ValueError:
            return False
    port = None
    if ":" in rule:
        if rule.startswith("["):
            match = re.fullmatch(r"\[([^]]+)\]:(\d+)", rule)
        else:
            match = re.fullmatch(r"([^:]+):(\d+)", rule)
        if match is None:
            raise Refused("proxy bypass")
        rule, port = match.groups()
    subdomains = rule.startswith((".", "*."))
    rule = rule.removeprefix("*").removeprefix(".").lower()
    if not rule or rule.endswith(".") or re.fullmatch(r"[a-z0-9.:-]+", rule) is None:
        raise Refused("proxy bypass")
    return (port is None or str(target.port or 443) == port) and (
        (not subdomains and host == rule) or host.endswith("." + rule)
    )


def proxy_for(egress, target):
    if not isinstance(egress, dict) or set(egress) != {
        "httpProxy",
        "httpsProxy",
        "noProxy",
    }:
        raise Refused("egress")
    proxy = egress["httpsProxy"]
    if not isinstance(proxy, str) or not isinstance(egress["httpProxy"], str):
        raise Refused("egress")
    if proxy:
        endpoint(proxy, proxy=True)
    elif egress["httpProxy"]:
        raise Refused("HTTPS proxy")
    rules = egress["noProxy"]
    if not isinstance(rules, list) or len(rules) > 128 or not proxy and rules:
        raise Refused("proxy bypass")
    matches = [bypass_matches(rule, target) for rule in rules]
    return None if any(matches) else proxy or None


def trusted_roots():
    descriptor = os.open("/", DIRECTORY_FLAGS)
    try:
        for name in ("etc", "pki", "ca-trust", "extracted", "pem"):
            child = os.open(name, DIRECTORY_FLAGS, dir_fd=descriptor)
            os.close(descriptor)
            descriptor = child
            observed = os.fstat(descriptor)
            if observed.st_uid != 0 or observed.st_mode & 0o022:
                raise Refused("system TLS trust")
        handle = os.open("tls-ca-bundle.pem", FILE_FLAGS, dir_fd=descriptor)
        try:
            before = os.fstat(handle)
            if (
                not stat.S_ISREG(before.st_mode)
                or before.st_uid != 0
                or before.st_mode & 0o022
                or not 0 < before.st_size <= 8 << 20
            ):
                raise Refused("system TLS trust")
            data = read_exact(handle, before.st_size)
            if identity(before) != identity(os.fstat(handle)):
                raise Refused("system TLS trust changed")
        finally:
            os.close(handle)
    finally:
        os.close(descriptor)
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
    context.minimum_version = ssl.TLSVersion.TLSv1_2
    context.load_verify_locations(cadata=data.decode("ascii"))
    return context


@contextlib.contextmanager
def alarm(seconds):
    """Bound DNS, TLS, headers and slow streaming together for seconds.

    Action workers execute in a dedicated process. Any earlier Ansible alarm is
    preserved: the sooner of the two fires, and the earlier one is re-armed with
    what remains of it."""
    started = time.monotonic()
    prior_handler = signal.getsignal(signal.SIGALRM)
    prior_timer = signal.getitimer(signal.ITIMER_REAL)

    def expired(_number, _frame):
        raise Refused("acquisition deadline")

    try:
        signal.signal(signal.SIGALRM, expired)
        signal.setitimer(
            signal.ITIMER_REAL,
            min(seconds, prior_timer[0]) if prior_timer[0] else seconds,
        )
        yield
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)
        signal.signal(signal.SIGALRM, prior_handler)
        if prior_timer[0]:
            signal.setitimer(
                signal.ITIMER_REAL,
                max(0.001, prior_timer[0] - (time.monotonic() - started)),
                prior_timer[1],
            )


def download(source, egress, descriptor):
    """Stream only exact approved bytes into descriptor, each chunk written
    before the next is read, using explicit routing and system trust.

    What the descriptor holds is proved only when this returns; a refused
    download leaves its caller a partial file to discard."""
    try:
        with alarm(DOWNLOAD_SECONDS):
            _download(source, egress, descriptor)
    except Exception:
        raise Refused("approved source acquisition was refused or incomplete") from None


def _download(source, egress, descriptor):
    deadline = time.monotonic() + DOWNLOAD_SECONDS

    def store(response):
        digest = stream_copy(
            functools.partial(response.read, decode_content=False),
            source["bytes"],
            deadline,
            descriptor,
        )
        if digest != source["sha256"]:
            raise Refused("source integrity")

    respond(source, egress, deadline, store)


def respond(source, egress, deadline, consume):
    """Route one approved source and hand its checked response to consume.

    Every redirect, header, status and declared length is checked here, once,
    for a download and a streamed acquisition alike."""
    source_identity(source)
    current = source["url"]
    original = endpoint(current)
    proxy_for(egress, original)
    import urllib3  # Included in the immutable execution closure.

    trust = trusted_roots()
    for _redirect_index in range(6):
        if time.monotonic() > deadline:
            raise Refused("acquisition deadline")
        parsed = endpoint(current, redirect=True)
        if (
            parsed.hostname != original.hostname
            and parsed.hostname not in REDIRECT_HOSTS
        ):
            raise Refused("redirect origin")
        route = proxy_for(egress, parsed)
        arguments = dict(
            ssl_context=trust,
            retries=False,
            timeout=urllib3.Timeout(connect=15, read=30),
        )
        manager = (
            urllib3.ProxyManager(route, proxy_ssl_context=trust, **arguments)
            if route
            else urllib3.PoolManager(**arguments)
        )
        response = None
        try:
            response = manager.request(
                "GET",
                current,
                preload_content=False,
                redirect=False,
                headers={
                    "Accept-Encoding": "identity",
                    "User-Agent": "Bootwright-Controller/1",
                },
            )
            if (
                sum(
                    len(str(key)) + len(str(value))
                    for key, value in response.headers.items()
                )
                > 65536
            ):
                raise Refused("response headers")
            if response.status in (301, 302, 303, 307, 308):
                current = urllib.parse.urljoin(
                    current, response.headers.get("Location", "")
                )
                continue
            if response.status != 200 or response.headers.get("Content-Encoding"):
                raise Refused("source response")
            declared = response.headers.get("Content-Length")
            if declared is not None and (
                not declared.isdecimal() or int(declared) != source["bytes"]
            ):
                raise Refused("source size")
            return consume(response)
        finally:
            if response is not None:
                response.close()
            manager.clear()
    raise Refused("redirect count")


def stream_copy(read, size, deadline=None, descriptor=None, observers=()):
    """Copy exactly size bytes from read, one chunk at a time, and digest them.

    Each chunk is hashed, observed and written before the next read, and no read
    or write exceeds CHUNK, so nothing larger than one chunk is ever held."""
    digest = hashlib.sha256()
    copied = 0
    while copied <= size:
        if deadline is not None and time.monotonic() > deadline:
            raise Refused("acquisition deadline")
        block = read(min(CHUNK, size + 1 - copied))
        if not block:
            break
        copied += len(block)
        if copied > size:
            raise Refused("stream size")
        digest.update(block)
        for observe in observers:
            observe(block)
        if descriptor is not None:
            write_all(descriptor, block)
    if copied != size:
        raise Refused("stream size")
    return digest.hexdigest()


def write_all(descriptor, block):
    remaining = memoryview(block)
    while remaining:
        count = os.write(descriptor, remaining[:CHUNK])
        if count <= 0:
            raise Refused("artifact write")
        remaining = remaining[count:]


class PatternCount:
    """Count a pattern in a stream fed chunk by chunk, each occurrence once.

    The last len(pattern) - 1 bytes carry over, so an occurrence split across
    chunks is found, and one that ends in the carried bytes cannot fit in them
    whole, so it is never found twice."""

    def __init__(self, pattern):
        self.pattern = pattern
        self.count = 0
        self.carried = b""

    def update(self, block):
        window = self.carried + block
        found = window.find(self.pattern)
        while found >= 0:
            self.count += 1
            found = window.find(self.pattern, found + 1)
        keep = len(self.pattern) - 1
        self.carried = window[len(window) - keep:] if keep < len(window) else window


class ReleaseStamp:
    """Read the release an oc executable names without executing it."""

    def __init__(self, version):
        if not isinstance(version, str) or len(version) >= len(RELEASE_MARKER) - 1:
            raise Refused("openshift client release")
        stamp = version.encode("ascii") + b"\x00" + RELEASE_MARKER[len(version) + 1:]
        self.stamped = PatternCount(stamp)
        self.unstamped = PatternCount(RELEASE_MARKER)

    def update(self, block):
        self.stamped.update(block)
        self.unstamped.update(block)

    def verify(self):
        if self.stamped.count != 1 or self.unstamped.count:
            raise Unreleased("openshift client release")


def acquire(bundle, source, egress, seconds):
    """Stream one approved tool source into the bundle under its own deadline.

    Both the alarm and the transfer check use this source's deadline, never the
    fixed one download keeps. Only a complete, digest-verified source gains a
    name; an interrupted one stays an unlinked file the kernel reclaims."""
    source_identity(source)
    name = "sources/" + source["id"]
    bundle.writable()
    bundle.capacity(name, source["bytes"])
    deadline = time.monotonic() + seconds

    def store(response):
        with bundle.staged(name, 0o600) as staged:
            digest = stream_copy(
                functools.partial(response.read, decode_content=False),
                source["bytes"],
                deadline,
                staged[0],
            )
            if digest != source["sha256"]:
                raise Refused("source integrity")
            bundle.link(name, staged, source["bytes"], digest)

    with alarm(seconds):
        respond(source, egress, deadline, store)


def identity(value):
    return (
        value.st_dev,
        value.st_ino,
        value.st_mode,
        value.st_uid,
        value.st_gid,
        value.st_nlink,
        value.st_size,
        value.st_mtime_ns,
        value.st_ctime_ns,
    )


def read_exact(descriptor, size):
    data = bytearray()
    while len(data) <= size:
        block = os.read(descriptor, min(65536, size + 1 - len(data)))
        if not block:
            break
        data.extend(block)
    if len(data) != size:
        raise Refused("file size")
    return bytes(data)


def link_at(descriptor, parent, leaf):
    """Give an unlinked file its first name, which must not exist yet."""
    library = ctypes.CDLL(None, use_errno=True)
    linkat = library.linkat
    linkat.argtypes = (
        ctypes.c_int,
        ctypes.c_char_p,
        ctypes.c_int,
        ctypes.c_char_p,
        ctypes.c_int,
    )
    linkat.restype = ctypes.c_int
    if linkat(descriptor, b"", parent, leaf.encode("ascii"), AT_EMPTY_PATH) != 0:
        error = ctypes.get_errno()
        raise OSError(error, os.strerror(error))


class Bundle:
    """Directory-fd authority for complete, exclusive artifact publication."""

    def __init__(self, location):
        if not isinstance(location, dict) or set(location) != {
            "path",
            "device",
            "inode",
            "writable",
            "sealed",
        }:
            raise Refused("bundle authority")
        if not isinstance(location["writable"], bool) or not isinstance(
            location["sealed"], bool
        ):
            raise Refused("bundle authority")
        if not isinstance(location["path"], str) or not location["path"].startswith(
            "/"
        ):
            raise Refused("bundle path")
        relative(location["path"][1:])
        self.location = dict(location)
        self.fd = self.open_path()
        observed = os.fstat(self.fd)
        if (
            observed.st_dev != location["device"]
            or observed.st_ino != location["inode"]
        ):
            self.close()
            raise Refused("bundle identity")
        self.owner = (observed.st_uid, observed.st_gid, observed.st_dev)

    def open_path(self):
        descriptor = os.open("/", DIRECTORY_FLAGS)
        root_uid = os.fstat(descriptor).st_uid
        try:
            for name in relative(self.location["path"][1:]):
                child = os.open(name, DIRECTORY_FLAGS, dir_fd=descriptor)
                os.close(descriptor)
                descriptor = child
                observed = os.fstat(descriptor)
                sticky_root = (
                    observed.st_uid == root_uid and observed.st_mode & stat.S_ISVTX
                )
                if (
                    observed.st_uid not in (root_uid, os.geteuid())
                    or observed.st_mode & 0o022
                    and not sticky_root
                ):
                    raise Refused("bundle ancestor")
            observed = os.fstat(descriptor)
            if (
                observed.st_uid != os.geteuid()
                or stat.S_IMODE(observed.st_mode) != 0o700
            ):
                raise Refused("bundle ownership")
            return descriptor
        except BaseException:
            os.close(descriptor)
            raise

    def close(self):
        if self.fd is not None:
            os.close(self.fd)
            self.fd = None

    def verify(self):
        current = self.open_path()
        try:
            before, after = os.fstat(self.fd), os.fstat(current)
            if (before.st_dev, before.st_ino) != (after.st_dev, after.st_ino):
                raise Refused("bundle replaced")
        finally:
            os.close(current)

    def writable(self):
        self.verify()
        if not self.location["writable"] or self.location["sealed"]:
            raise Refused("sealed or read-only bundle")

    def parent(self, name, create=False):
        parts = relative(name)
        descriptor = os.dup(self.fd)
        try:
            for part in parts[:-1]:
                try:
                    child = os.open(part, DIRECTORY_FLAGS, dir_fd=descriptor)
                except FileNotFoundError:
                    if not create:
                        raise
                    self.writable()
                    os.mkdir(part, 0o700, dir_fd=descriptor)
                    os.fsync(descriptor)
                    child = os.open(part, DIRECTORY_FLAGS, dir_fd=descriptor)
                os.close(descriptor)
                descriptor = child
                value = os.fstat(descriptor)
                if (
                    value.st_uid,
                    value.st_gid,
                    value.st_dev,
                ) != self.owner or stat.S_IMODE(value.st_mode) != 0o700:
                    raise Refused("bundle directory")
            return descriptor, parts[-1]
        except BaseException:
            os.close(descriptor)
            raise

    @contextlib.contextmanager
    def opened(self, name, maximum, mode):
        """Yield one file's descriptor and size, or None when it is absent.

        The file must be a private regular file with a single name. Its identity
        must hold across everything done with the descriptor, so a file changed
        while it was read is refused when the block ends."""
        self.verify()
        try:
            parent, leaf = self.parent(name)
        except FileNotFoundError:
            parent = None
        if parent is None:
            yield None
            return
        try:
            try:
                descriptor = os.open(leaf, FILE_FLAGS, dir_fd=parent)
            except FileNotFoundError:
                descriptor = None
            if descriptor is None:
                yield None
                return
            try:
                before = os.fstat(descriptor)
                if (
                    not stat.S_ISREG(before.st_mode)
                    or stat.S_IMODE(before.st_mode) != mode
                    or before.st_nlink != 1
                    or (before.st_uid, before.st_gid, before.st_dev) != self.owner
                    or not 0 <= before.st_size <= maximum
                ):
                    raise Refused("bundle file metadata")
                yield descriptor, before.st_size
                after = os.stat(leaf, dir_fd=parent, follow_symlinks=False)
                if identity(before) != identity(os.fstat(descriptor)) or identity(
                    before
                ) != identity(after):
                    raise Refused("bundle file changed")
            finally:
                os.close(descriptor)
            self.verify()
        finally:
            os.close(parent)

    def present(self, name, maximum, mode):
        with self.opened(name, maximum, mode) as found:
            return found is not None

    def digest(self, name, maximum, mode):
        """The streamed digest and size of one file, or None when it is absent."""
        with self.opened(name, maximum, mode) as found:
            if found is None:
                return None
            size = found[1]
            return stream_copy(functools.partial(os.read, found[0]), size), size

    @contextlib.contextmanager
    def staged(self, name, mode):
        """Yield an unlinked file in the bundle's own directory; only link
        names it, and only link creates name's directories.

        However the block ends, the file is closed, so one never linked leaves
        nothing behind, not even a directory."""
        if mode not in (0o600, 0o700):
            raise Refused("publication bounds")
        relative(name)
        self.writable()
        temporary = os.open(
            ".", os.O_TMPFILE | os.O_RDWR | os.O_CLOEXEC, mode, dir_fd=self.fd
        )
        try:
            os.fchmod(temporary, mode)
            yield temporary, name, mode
        finally:
            os.close(temporary)

    def link(self, name, staged, size, digest):
        """Name one complete staged file, creating its directories now, then
        prove the name by its digest."""
        temporary, staged_name, mode = staged
        if staged_name != name:
            raise Refused("publication name")
        os.fsync(temporary)
        created = os.fstat(temporary)
        if (
            not stat.S_ISREG(created.st_mode)
            or stat.S_IMODE(created.st_mode) != mode
            or created.st_nlink != 0
            or created.st_size != size
            or (created.st_uid, created.st_gid, created.st_dev) != self.owner
        ):
            raise Refused("temporary artifact metadata")
        self.writable()
        parent, leaf = self.parent(name, create=True)
        try:
            link_at(temporary, parent, leaf)
            os.fsync(parent)
        finally:
            os.close(parent)
        if self.digest(name, size, mode) != (digest, size):
            raise Refused("published artifact integrity")

    def capacity(self, name, size):
        totals = {"bytes": 0, "sources": 0, "targets": 0, "entries": 0}

        def walk(descriptor, prefix="", depth=0):
            if depth > 32:
                raise Refused("bundle depth")
            names = sorted(os.listdir(descriptor))
            totals["entries"] += len(names)
            if totals["entries"] > 32768:
                raise Refused("bundle entry count")
            for part in names:
                relative(part)
                value = os.stat(part, dir_fd=descriptor, follow_symlinks=False)
                if (value.st_uid, value.st_gid, value.st_dev) != self.owner:
                    raise Refused("bundle entry ownership")
                path = prefix + part
                if stat.S_ISDIR(value.st_mode):
                    if stat.S_IMODE(value.st_mode) != 0o700:
                        raise Refused("bundle directory mode")
                    child = os.open(part, DIRECTORY_FLAGS, dir_fd=descriptor)
                    try:
                        if (value.st_dev, value.st_ino) != (
                            os.fstat(child).st_dev,
                            os.fstat(child).st_ino,
                        ):
                            raise Refused("bundle directory changed")
                        walk(child, path + "/", depth + 1)
                    finally:
                        os.close(child)
                else:
                    if (
                        not stat.S_ISREG(value.st_mode)
                        or stat.S_IMODE(value.st_mode) not in (0o600, 0o700)
                        or value.st_nlink != 1
                        or not 0 <= value.st_size <= MAX_SOURCE
                    ):
                        raise Refused("bundle entry metadata")
                    totals["bytes"] += value.st_size
                    totals["sources"] += (
                        value.st_size if path.startswith("sources/tool-") else 0
                    )
                    totals["targets"] += (
                        value.st_size if path.startswith("tools/") else 0
                    )
            if names != sorted(os.listdir(descriptor)):
                raise Refused("bundle entries changed")

        walk(self.fd)
        totals["bytes"] += size
        totals["sources"] += size if name.startswith("sources/tool-") else 0
        totals["targets"] += size if name.startswith("tools/") else 0
        if (
            totals["bytes"] > MAX_BUNDLE
            or totals["sources"] > MAX_EXPANDED
            or totals["targets"] > MAX_EXPANDED
            or totals["entries"] + len(relative(name)) > 32768
        ):
            raise Refused("bundle capacity")
        self.verify()


class LimitedReader:
    def __init__(self, stream):
        self.stream = stream
        self.remaining = MAX_EXPANDED

    def read(self, size):
        data = self.stream.read(min(size, self.remaining + 1))
        self.remaining -= len(data)
        if self.remaining < 0:
            raise Refused("expanded archive bound")
        return data


def tool_files(tool):
    if not isinstance(tool, dict) or set(tool) != {
        "kind",
        "version",
        "compatibility",
        "source",
        "archive",
        "files",
    }:
        raise Refused("tool identity")
    kind, version, compatibility = tool["kind"], tool["version"], tool["compatibility"]
    if (
        not isinstance(version, str)
        or re.fullmatch(r"[0-9v][A-Za-z0-9._+-]{0,95}", version) is None
    ):
        raise Refused("tool version")
    source_identity(tool["source"])
    if (
        re.fullmatch(
            "tool-" + re.escape(kind) + r"-[a-f0-9]{32}-" + re.escape(version),
            tool["source"]["id"],
        )
        is None
    ):
        raise Refused("tool source identity")
    members = {
        "helm": ["linux-amd64/helm"],
        "govc": ["govc"],
        "kubectl": ["kubectl"],
        "openshift-clients": ["oc", "kubectl"],
        "openshift-install": ["openshift-install"],
        "virtctl": ["virtctl"],
    }.get(kind)
    if members is None or not isinstance(compatibility, str):
        raise Refused("tool vocabulary")
    allowed = (
        ("openshift", "okd")
        if kind.startswith("openshift-")
        else ("kubevirt",) if kind == "virtctl" else ("",)
    )
    if compatibility not in allowed:
        raise Refused("tool compatibility")
    if (
        compatibility != "okd"
        and re.fullmatch(
            r"v?(?:0|[1-9][0-9]{0,8})\.(?:0|[1-9][0-9]{0,8})\.(?:0|[1-9][0-9]{0,8})",
            version,
        )
        is None
    ):
        raise Refused("stable tool release")
    archive = "binary" if kind in ("kubectl", "virtctl") else "tar.gz"
    prefix = "/".join(part for part in ("tools", kind, compatibility, version) if part)
    files = [
        {"member": member, "path": prefix + "/" + posixpath.basename(member)}
        for member in members
    ]
    if tool["archive"] != archive or tool["files"] != files:
        raise Refused("tool projection")
    return files


class Staging:
    """The unlinked member files of one projection, open until it ends.

    Their bytes count against the bundle's capacity although no walk sees them
    before they are linked."""

    def __init__(self, bundle, stack):
        self.bundle = bundle
        self.stack = stack
        self.pending = 0

    def stage(self, path, size):
        self.bundle.writable()
        self.bundle.capacity(path, self.pending + size)
        staged = self.stack.enter_context(self.bundle.staged(path, 0o700))
        self.pending += size
        return staged

    def copy(self, path, member):
        """Stage a member's own copy for another name, never a second link:
        a file with two names is refused by every later read and walk."""
        source = member["staged"][0]
        os.lseek(source, 0, os.SEEK_SET)
        staged = self.stage(path, member["bytes"])
        digest = stream_copy(
            functools.partial(os.read, source), member["bytes"], descriptor=staged[0]
        )
        if digest != member["digest"]:
            raise Refused("staged member changed")
        return staged

    def link(self, path, staged, member):
        self.bundle.link(path, staged, member["bytes"], member["digest"])
        self.pending -= member["bytes"]


def stream_member(staging, path, read, size, stamp):
    """Digest one member from its stream, staging it for path when staging is
    given and scanning it for the release stamp when stamp is."""
    staged = staging.stage(path, size) if staging is not None else None
    digest = stream_copy(
        read,
        size,
        descriptor=staged[0] if staged is not None else None,
        observers=(stamp.update,) if stamp is not None else (),
    )
    return {"digest": digest, "bytes": size, "staged": staged, "stamp": stamp}


def archive_members(tool, files, source, staging):
    """Stream a tar.gz source's selected regular members and read its links."""
    selected = {file["member"]: file["path"] for file in files}
    regular, links, seen = {}, {}, set()
    expanded = 0
    with gzip.GzipFile(fileobj=source, mode="rb") as compressed:
        with tarfile.open(fileobj=LimitedReader(compressed), mode="r|") as archive:
            for member in archive:
                name = member.name.removesuffix("/") if member.isdir() else member.name
                relative(name)
                if name in seen or len(seen) >= MAX_ENTRIES or member.mode & 0o7000:
                    raise Refused("archive member")
                seen.add(name)
                if member.isdir():
                    if member.size:
                        raise Refused("archive directory")
                elif member.type in (tarfile.REGTYPE, tarfile.AREGTYPE):
                    if (
                        not 0 <= member.size <= MAX_SOURCE
                        or expanded + member.size > MAX_EXPANDED
                    ):
                        raise Refused("archive member bound")
                    expanded += member.size
                    if name in selected:
                        if not member.size or not member.mode & 0o111:
                            raise Refused("archive executable")
                        stamp = None
                        if tool["kind"] == "openshift-clients":
                            stamp = ReleaseStamp(tool["version"])
                        regular[name] = stream_member(
                            staging,
                            selected[name],
                            archive.extractfile(member).read,
                            member.size,
                            stamp,
                        )
                elif member.issym() or member.islnk():
                    relative(member.linkname)
                    target = (
                        posixpath.join(posixpath.dirname(name), member.linkname)
                        if member.issym()
                        else member.linkname
                    )
                    relative(target)
                    if name not in selected or target not in selected:
                        raise Refused("archive link")
                    links[name] = target
                else:
                    raise Refused("archive member type")
    return regular, links


def resolve(files, links, regular):
    """Each file's path to whether it is its regular member's own name, and to
    that member, following the archive's links."""
    resolved = {}
    for file in files:
        name = file["member"]
        depth = 0
        while name in links:
            if depth >= 16:
                raise Refused("archive link cycle")
            name = links[name]
            depth += 1
        if name not in regular:
            raise Refused("archive required executable")
        resolved[file["path"]] = (name == file["member"], regular[name])
    return resolved


def settle_members(bundle, staging, resolved, inspect_only):
    """Prove every published member by its streamed digest, then name each
    missing one. Nothing is named until every existing member is proved."""
    missing = []
    for path, (owned, member) in sorted(resolved.items()):
        published = bundle.digest(path, member["bytes"], 0o700)
        if published is None and staging is not None:
            missing.append((path, owned, member))
        elif published != (member["digest"], member["bytes"]):
            if inspect_only or published is None:
                raise Refused("target executable postcondition")
            raise Refused("existing bundle file differs")
    for path, owned, member in missing:
        staged = member["staged"] if owned else staging.copy(path, member)
        staging.link(path, staged, member)
    return bool(missing)


def project(bundle, tool, inspect_only):
    """Project a retained source's fixed members, streaming, and return their
    manifest and whether any was published.

    The source is digested and then read again for its members under one
    identity check. Only after both passes is an oc release proved and any
    member named; inspection stages and writes nothing."""
    files = tool_files(tool)
    source = tool["source"]
    stage = not inspect_only and not all(
        bundle.present(file["path"], MAX_SOURCE, 0o700) for file in files
    )
    with contextlib.ExitStack() as stack:
        staging = Staging(bundle, stack) if stage else None
        with bundle.opened("sources/" + source["id"], source["bytes"], 0o600) as found:
            if found is None:
                raise Refused("required retained source is missing")
            descriptor, size = found
            digest = stream_copy(functools.partial(os.read, descriptor), size)
            if (digest, size) != (source["sha256"], source["bytes"]):
                raise Refused("source integrity")
            os.lseek(descriptor, 0, os.SEEK_SET)
            with io.FileIO(descriptor, "r", closefd=False) as reader:
                if tool["archive"] == "binary":
                    member = stream_member(staging, files[0]["path"], reader.read, size, None)
                    regular, links = {files[0]["member"]: member}, {}
                else:
                    regular, links = archive_members(tool, files, reader, staging)
        resolved = resolve(files, links, regular)
        if tool["kind"] == "openshift-clients":
            oc = next(file["path"] for file in files if file["member"] == "oc")
            resolved[oc][1]["stamp"].verify()
        changed = settle_members(bundle, staging, resolved, inspect_only)
    manifest = [
        {"path": path, "sha256": member["digest"], "bytes": member["bytes"]}
        for path, (_owned, member) in sorted(resolved.items())
    ]
    return manifest, changed


def prepare_tool(location, tool, egress, deadline, inspect_only=False):
    """Acquire one frozen tool's source when it is missing, then project it.

    A missing source streams under the acquisition deadline its request froze,
    in seconds; no downloaded tool is ever executed."""
    files = tool_files(tool)
    if (
        not isinstance(deadline, int)
        or isinstance(deadline, bool)
        or not 0 < deadline <= MAX_DEADLINE
    ):
        raise Refused("acquisition deadline")
    proxy_for(egress, endpoint(tool["source"]["url"]))
    bundle = Bundle(location)
    changed = False
    try:
        name = "sources/" + tool["source"]["id"]
        if not bundle.present(name, tool["source"]["bytes"], 0o600):
            if inspect_only:
                raise Refused("required retained source is missing")
            bundle.writable()
            for file in files:
                if bundle.present(file["path"], MAX_SOURCE, 0o700):
                    raise Refused("unattributed target without retained source")
            acquire(bundle, tool["source"], egress, deadline)
            changed = True
        manifest, published = project(bundle, tool, inspect_only)
        return {
            "changed": changed or published,
            "evidence": {
                "source": tool["source"]["id"],
                "sha256": tool["source"]["sha256"],
                "files": sha256(canonical(manifest)),
            },
        }
    finally:
        bundle.close()
