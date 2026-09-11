"""Fixed controller artifact acquisition and immutable publication."""

from __future__ import annotations

import ctypes
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


def verify_source(source, data):
    source_identity(source)
    if len(data) != source["bytes"] or sha256(data) != source["sha256"]:
        raise Refused("source integrity")


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


def download(source, egress):
    """Read only exact approved bytes using explicit routing and system trust."""
    # Action workers execute in a dedicated process. Preserve any earlier
    # Ansible alarm while bounding DNS, TLS, headers and slow streaming together.
    started = time.monotonic()
    prior_handler = signal.getsignal(signal.SIGALRM)
    prior_timer = signal.getitimer(signal.ITIMER_REAL)

    def expired(_number, _frame):
        raise Refused("acquisition deadline")

    try:
        signal.signal(signal.SIGALRM, expired)
        signal.setitimer(
            signal.ITIMER_REAL, min(300, prior_timer[0]) if prior_timer[0] else 300
        )
        return _download(source, egress)
    except Exception:
        raise Refused("approved source acquisition was refused or incomplete") from None
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)
        signal.signal(signal.SIGALRM, prior_handler)
        if prior_timer[0]:
            signal.setitimer(
                signal.ITIMER_REAL,
                max(0.001, prior_timer[0] - (time.monotonic() - started)),
                prior_timer[1],
            )


def _download(source, egress):
    source_identity(source)
    current = source["url"]
    original = endpoint(current)
    proxy_for(egress, original)
    import urllib3  # Included in the immutable execution closure.

    trust = trusted_roots()
    deadline = time.monotonic() + 300
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
            data = bytearray()
            while len(data) <= source["bytes"]:
                if time.monotonic() > deadline:
                    raise Refused("acquisition deadline")
                block = response.read(
                    min(65536, source["bytes"] + 1 - len(data)), decode_content=False
                )
                if not block:
                    break
                data.extend(block)
            verify_source(source, data)
            return bytes(data)
        finally:
            if response is not None:
                response.close()
            manager.clear()
    raise Refused("redirect count")


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

    def read(self, name, maximum, mode):
        self.verify()
        try:
            parent, leaf = self.parent(name)
        except FileNotFoundError:
            return None
        try:
            try:
                descriptor = os.open(leaf, FILE_FLAGS, dir_fd=parent)
            except FileNotFoundError:
                return None
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
                data = read_exact(descriptor, before.st_size)
                after = os.stat(leaf, dir_fd=parent, follow_symlinks=False)
                if identity(before) != identity(os.fstat(descriptor)) or identity(
                    before
                ) != identity(after):
                    raise Refused("bundle file changed")
            finally:
                os.close(descriptor)
            self.verify()
            return data
        finally:
            os.close(parent)

    def publish(self, name, data, mode):
        if len(data) > MAX_SOURCE or mode not in (0o600, 0o700):
            raise Refused("publication bounds")
        previous = self.read(name, len(data), mode)
        if previous is not None:
            if previous != data:
                raise Refused("existing bundle file differs")
            return False
        self.writable()
        self.capacity(name, len(data))
        parent, leaf = self.parent(name, create=True)
        temporary = None
        try:
            temporary = os.open(
                ".", os.O_TMPFILE | os.O_RDWR | os.O_CLOEXEC, mode, dir_fd=parent
            )
            os.fchmod(temporary, mode)
            remaining = memoryview(data)
            while remaining:
                count = os.write(temporary, remaining[:65536])
                if count <= 0:
                    raise Refused("artifact write")
                remaining = remaining[count:]
            os.fsync(temporary)
            created = os.fstat(temporary)
            if (
                not stat.S_ISREG(created.st_mode)
                or stat.S_IMODE(created.st_mode) != mode
                or created.st_nlink != 0
                or created.st_size != len(data)
                or (created.st_uid, created.st_gid, created.st_dev) != self.owner
            ):
                raise Refused("temporary artifact metadata")
            self.writable()
            current_parent, _leaf = self.parent(name)
            try:
                before, current = os.fstat(parent), os.fstat(current_parent)
                if (before.st_dev, before.st_ino) != (current.st_dev, current.st_ino):
                    raise Refused("publication parent changed")
            finally:
                os.close(current_parent)
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
            if linkat(temporary, b"", parent, leaf.encode("ascii"), 0x1000) != 0:
                error = ctypes.get_errno()
                raise OSError(error, os.strerror(error))
            os.fsync(parent)
        finally:
            if temporary is not None:
                os.close(temporary)
            os.close(parent)
        if self.read(name, len(data), mode) != data:
            raise Refused("published artifact integrity")
        return True

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


def project(tool, data):
    files = tool_files(tool)
    verify_source(tool["source"], data)
    if tool["archive"] == "binary":
        return {files[0]["path"]: data}
    selected = {file["member"] for file in files}
    regular, links, seen = {}, {}, set()
    expanded = 0
    with gzip.GzipFile(fileobj=io.BytesIO(data), mode="rb") as compressed:
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
                        regular[name] = archive.extractfile(member).read(
                            member.size + 1
                        )
                        if len(regular[name]) != member.size:
                            raise Refused("archive executable size")
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
    result = {}
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
        result[file["path"]] = regular[name]
    return result


def prepare_tool(location, tool, egress, inspect_only=False):
    files = tool_files(tool)
    proxy_for(egress, endpoint(tool["source"]["url"]))
    bundle = Bundle(location)
    changed = False
    try:
        name = "sources/" + tool["source"]["id"]
        data = bundle.read(name, tool["source"]["bytes"], 0o600)
        if data is None:
            if inspect_only:
                raise Refused("required retained source is missing")
            bundle.writable()
            for file in files:
                if bundle.read(file["path"], MAX_SOURCE, 0o700) is not None:
                    raise Refused("unattributed target without retained source")
            data = download(tool["source"], egress)
        projected = project(tool, data)
        if not inspect_only:
            changed = bundle.publish(name, data, 0o600) or changed
        manifest = []
        for path, content in sorted(projected.items()):
            if inspect_only:
                if bundle.read(path, len(content), 0o700) != content:
                    raise Refused("target executable postcondition")
            else:
                changed = bundle.publish(path, content, 0o700) or changed
            manifest.append(
                {"path": path, "sha256": sha256(content), "bytes": len(content)}
            )
        return {
            "changed": changed,
            "evidence": {
                "source": tool["source"]["id"],
                "sha256": tool["source"]["sha256"],
                "files": sha256(canonical(manifest)),
            },
        }
    finally:
        bundle.close()
