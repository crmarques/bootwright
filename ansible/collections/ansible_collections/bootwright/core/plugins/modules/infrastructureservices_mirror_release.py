#!/usr/bin/python
"""Read which release an image mirror holds, and prepare the release's own oc to copy one into it."""

from __future__ import annotations

DOCUMENTATION = r"""
module: infrastructureservices_mirror_release
short_description: Read a release and its components in the image mirror, or prepare the release's oc and its lent files
version_added: "0.1.0"
description:
  - With C(mode=read), asks the mirror for the release image by its pinned digest or its version's tag, reads its
    image references from the release's own layers there, and asks the mirror for every component by digest. It
    reports the release's digest, each component and how many the mirror lacks, and copies nothing.
  - With C(mode=prepare), fills a private run directory the role created with the release's own C(oc), read from its
    vendor origin through the frozen egress route and checked against the SHA-256 the publisher lists, and a private
    home, so the role's copy reads no file of the host's own. The lent auth document and the mirror's trust anchor go
    to a private directory beneath C(/run), a tmpfs a reboot clears, which it returns. It first removes another run's
    copy scratch and lent directory older than any run lives, which a killed copy left.
  - With C(mode=clean), removes the lent directory and everything beneath the run directory, which the role then
    removes itself.
  - Every credential and anchor reaches only files of the lent directory.
options:
  mode:
    description: Whether to read what the mirror holds, prepare the copy or remove what it lent.
    type: str
    choices:
      - read
      - prepare
      - clean
    required: true
  release:
    description: The frozen release, with repository, components, tag, version and an optional digest.
    type: dict
    required: false
  client:
    description: The frozen client, with url, file and checksums.
    type: dict
    required: false
  mirror:
    description: The mirror registry's host and port.
    type: str
    required: false
  egress:
    description: The frozen route, with httpProxy, httpsProxy and noProxy.
    type: dict
    required: false
  username:
    description: The mirror's user name. Never returned or logged.
    type: str
    required: false
  password:
    description: The mirror's password. Never returned or logged.
    type: str
    required: false
  trust:
    description: The PEM trust anchor the mirror's certificate is verified against. Never returned or logged.
    type: str
    required: false
  auth:
    description: The lent auth document of the copy, prepare only. Never returned or logged.
    type: str
    required: false
  lent:
    description: The private directory on tmpfs prepare returned with the lent auth document and anchor, clean only.
    type: path
    required: false
    default: ""
  scratch:
    description: The private run directory the role created for the copy, prepare and clean only.
    type: path
    required: false
  timeout:
    description: The seconds one read may take.
    type: int
    default: 60
author:
  - Bootwright contributors (@crmarques)
"""
EXAMPLES = r"""
- name: Read which release the image mirror holds
  bootwright.core.infrastructureservices_mirror_release:
    mode: read
    release: '{{ request.release }}'
    client: '{{ request.client }}'
    mirror: '{{ request.mirror.host }}'
    egress: '{{ request.egress }}'
    username: "{{ lookup('ansible.builtin.file', material.imageUsername, rstrip=false) }}"
    password: "{{ lookup('ansible.builtin.file', material.imagePassword, rstrip=false) }}"
    trust: "{{ lookup('ansible.builtin.file', material.imageTrust, rstrip=false) }}"
  no_log: true
"""
RETURN = r"""
digest:
  description: The digest of the release the mirror holds, empty when it holds none.
  returned: mode read
  type: str
components:
  description: Each component the mirror holds, by name and digest.
  returned: mode read
  type: list
  elements: dict
missing:
  description: How many of the release's components the mirror lacks.
  returned: mode read
  type: int
lent:
  description: The private directory on tmpfs holding the lent auth document and the anchor.
  returned: mode prepare
  type: str
reason:
  description: The refusal a failure names.
  returned: failure
  type: str
"""

import gzip
import hashlib
import json
import os
import re
import shutil
import stat
import tarfile as tarlib
import tempfile
import time
import zlib

from urllib.error import HTTPError, URLError

from ansible.module_utils.basic import AnsibleModule
from ansible.module_utils.urls import open_url

DIGEST = re.compile(r"sha256:[0-9a-f]{64}\Z")
MIRROR = re.compile(r"([a-z0-9]([a-z0-9.-]*[a-z0-9])?|\[[0-9a-f:.]+\]):[0-9]{1,5}\Z")
NAME = re.compile(r"[a-z0-9]([a-z0-9._-]*[a-z0-9])?\Z")
CHECKSUM = re.compile(r"[0-9a-f]{64}\Z")
RELEASE = "quay.io/openshift-release-dev/ocp-release"
COMPONENTS = "quay.io/openshift-release-dev/ocp-v4.0-art-dev"
REFERENCES = "release-manifests/image-references"
INDEXES = ("application/vnd.oci.image.index.v1+json", "application/vnd.docker.distribution.manifest.list.v2+json")
MANIFESTS = ", ".join(INDEXES + ("application/vnd.oci.image.manifest.v1+json", "application/vnd.docker.distribution.manifest.v2+json"))
MAX_MANIFEST = 4 << 20
MAX_LAYER = 1 << 30
MAX_EXPANDED = 4 << 30
MAX_HEADER = 1 << 20
READ_BUDGET = 30 * 60
SECRETS_BASE = "/run"
SECRETS_PREFIX = "bootwright-release-"
SCRATCH_NAME = re.compile(r"bootwright-scratch-[0-9]+-[0-9]+\Z")
STALE_SECONDS = 7 * 3600
HEADERS = (tarlib.GNUTYPE_LONGNAME, tarlib.GNUTYPE_LONGLINK, tarlib.XHDTYPE, tarlib.XGLTYPE, tarlib.SOLARIS_XHDTYPE)
MAX_REFERENCES = 8 << 20
MAX_CHECKSUMS = 1 << 20
MAX_CLIENT = 1 << 30
MAX_COMPONENTS = 512
CHUNK = 1 << 20


class Refused(Exception):
    """A failure the runner reports under its own refusal."""

    def __init__(self, reason, message):
        super().__init__(message)
        self.reason = reason


class Bounded:
    """A stream that refuses to yield more than its bound."""

    def __init__(self, stream, bound):
        self.stream, self.left = stream, bound

    def read(self, size=-1):
        if size is None or size < 0:
            size = self.left + 1
        data = self.stream.read(min(size, self.left + 1))
        self.left -= len(data)
        if self.left < 0:
            raise Refused("release-incomplete", "a release layer in the image mirror is larger than its bound")
        return data


class Hashing:
    """A blob stream bounded in bytes and time that hashes what it yields."""

    def __init__(self, stream, bound, deadline):
        self.stream, self.left, self.deadline, self.digest = stream, bound, deadline, hashlib.sha256()

    def read(self, size=-1):
        if time.monotonic() > self.deadline:
            raise Refused("mirror-unreadable", "reading the release from the image mirror outlasted its bound")
        if size is None or size < 0:
            size = CHUNK
        data = self.stream.read(min(size, self.left + 1))
        self.left -= len(data)
        if self.left < 0:
            raise Refused("release-incomplete", "a release layer in the image mirror is larger than its bound")
        self.digest.update(data)
        return data

    def drained(self):
        """The digest of the whole blob, read to its end."""
        while self.read(CHUNK):
            pass
        return "sha256:" + self.digest.hexdigest()


class BoundedTarInfo(tarlib.TarInfo):
    """A member whose long-name or extended header is refused past its bound before it is read."""

    def _proc_member(self, tarfile):
        if self.type in HEADERS and self.size > MAX_HEADER:
            raise tarlib.HeaderError("an archive header is larger than its bound")
        return super()._proc_member(tarfile)


def checked(release, mirror):
    if MIRROR.match(mirror) is None:
        raise ValueError("the image mirror is not a host and port")
    version, digest = release.get("version"), release.get("digest") or ""
    if (release.get("repository") != RELEASE or release.get("components") != COMPONENTS or not isinstance(version, str)
            or release.get("tag") != version + "-x86_64"):
        raise ValueError("the request names another release than quay.io's by its version's tag")
    if digest and DIGEST.match(digest) is None:
        raise ValueError("the release digest is not a sha256 digest")
    return digest or release["tag"]


def references(document):
    """Each component the release names, by name and digest, all in the component repository."""
    try:
        tags = json.loads(document)["spec"]["tags"]
    except (ValueError, KeyError, TypeError):
        raise Refused("release-incomplete", "the release in the image mirror names no readable image references") from None
    if not isinstance(tags, list) or not tags or len(tags) > MAX_COMPONENTS:
        raise Refused("release-incomplete", "the release in the image mirror names no bounded list of components")
    components = []
    for tag in tags:
        tag = tag if isinstance(tag, dict) else {}
        source = tag.get("from") if isinstance(tag.get("from"), dict) else {}
        name, pulled = tag.get("name"), source.get("name")
        repository, _separator, digest = (pulled if isinstance(pulled, str) else "").partition("@")
        if not isinstance(name, str) or NAME.match(name) is None or len(name) > 128 or repository != COMPONENTS or DIGEST.match(digest) is None:
            raise Refused("release-incomplete", "the release in the image mirror names a component outside %s" % COMPONENTS)
        components.append({"name": name, "digest": digest})
    return components


def manifest(get, mirror, repository, reference):
    """The manifest the mirror serves for a reference with its digest, or None when it holds none."""
    status, _headers, body = get("https://%s/v2/%s/manifests/%s" % (mirror, repository, reference), MANIFESTS, MAX_MANIFEST)
    if status == 404:
        return None, ""
    if status != 200:
        raise Refused("mirror-unreadable", "the image mirror at %s answered %d for %s" % (mirror, status, repository))
    try:
        return json.loads(body), "sha256:" + hashlib.sha256(body).hexdigest()
    except ValueError:
        raise Refused("release-incomplete", "the image mirror serves an unreadable manifest for %s" % repository) from None


def expanded_stream(raw, media):
    """The layer's tar stream: gzip-compressed or plain, any other compression refused."""
    if media.endswith("gzip"):
        return gzip.GzipFile(fileobj=raw, mode="rb")
    if media.endswith("tar"):
        return raw
    raise Refused("release-incomplete", "a release layer in the image mirror has a compression this copy does not read")


def layer_references(get, mirror, layer, deadline):
    """The image references of one release layer, or None when it holds none; the layer must hash to its digest."""
    digest = layer.get("digest")
    if not isinstance(digest, str) or DIGEST.match(digest) is None:
        raise Refused("release-incomplete", "the release in the image mirror names a layer by no sha256 digest")
    status, _headers, stream = get("https://%s/v2/%s/blobs/%s" % (mirror, RELEASE, digest), "*/*", None)
    if status != 200:
        raise Refused("release-incomplete", "the image mirror holds no layer %s of the release" % digest)
    raw, document = Hashing(stream, MAX_LAYER, deadline), None
    try:
        expanded = Bounded(expanded_stream(raw, layer.get("mediaType") or ""), MAX_EXPANDED)
        with tarlib.open(fileobj=expanded, mode="r|", tarinfo=BoundedTarInfo) as archive:
            for member in archive:
                if member.name.lstrip("./") == REFERENCES and member.isfile() and member.size <= MAX_REFERENCES:
                    document = archive.extractfile(member).read(MAX_REFERENCES)
                    break
        if raw.drained() != digest:
            raise Refused("release-incomplete", "the image mirror's layer %s of the release does not hash to its digest" % digest)
    except (tarlib.TarError, OSError, EOFError, zlib.error):
        raise Refused("release-incomplete", "a layer of the release in the image mirror is no readable archive") from None
    return document


def held(get, head, mirror, release, budget=READ_BUDGET):
    """The release digest the mirror holds, each component it holds and how many it lacks."""
    reference, deadline = checked(release, mirror), time.monotonic() + budget
    body, digest = manifest(get, mirror, RELEASE, reference)
    if body is None:
        return "", [], 0
    if release.get("digest") and digest != release["digest"]:
        raise Refused("release-digest-mismatch", "the image mirror holds the release %s at another digest than the pinned one" % release["tag"])
    if body.get("mediaType") in INDEXES or "manifests" in body:
        platform = [entry for entry in body.get("manifests") or [] if (entry.get("platform") or {}).get("architecture") == "amd64"]
        if len(platform) != 1:
            raise Refused("release-incomplete", "the release index in the image mirror names no single amd64 release")
        body, platform_digest = manifest(get, mirror, RELEASE, platform[0].get("digest"))
        if body is None:
            return digest, [], 1
        if platform_digest != platform[0].get("digest"):
            raise Refused("release-incomplete", "the image mirror's amd64 release does not hash to the digest its index names")
    document = None
    for layer in reversed(body.get("layers") or []):
        document = layer_references(get, mirror, layer, deadline)
        if document is not None:
            break
    if document is None:
        raise Refused("release-incomplete", "the release in the image mirror carries no image references")
    present, missing = [], 0
    for component in references(document):
        if time.monotonic() > deadline:
            raise Refused("mirror-unreadable", "reading the release from the image mirror outlasted its bound")
        if head("https://%s/v2/%s/manifests/%s" % (mirror, COMPONENTS, component["digest"])) == component["digest"]:
            present.append(component)
        else:
            missing += 1
    return digest, present, missing


def publisher_checksum(data, filename):
    """The one SHA-256 the publisher's list names for the client archive."""
    result = ""
    lines = data.decode("utf-8", "replace").strip().split("\n")
    for line in lines:
        fields = line.split()
        if len(fields) != 2 or fields[1].lstrip("*") != filename:
            continue
        if CHECKSUM.match(fields[0]) is None or result:
            raise Refused("client-digest-mismatch", "the publisher's checksum list is malformed or ambiguous for %s" % filename)
        result = fields[0]
    if not result:
        raise Refused("client-digest-mismatch", "the publisher's checksum list does not name %s" % filename)
    return result


def private_directory(path):
    os.mkdir(path, 0o700)
    return path


def write_private(path, data, mode=0o600):
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, mode)
    with os.fdopen(descriptor, "wb") as stream:
        stream.write(data if isinstance(data, bytes) else data.encode("utf-8"))


def scratch_checked(scratch):
    """The role's run directory, which must be this user's private, empty directory."""
    status = os.lstat(scratch)
    if not stat.S_ISDIR(status.st_mode) or status.st_uid != os.geteuid() or status.st_mode & 0o077 or os.listdir(scratch):
        raise ValueError("the run directory is not this user's private, empty directory")
    return scratch


def download(fetch, client, scratch):
    """The client archive, read once, checked against the publisher's list, then its oc alone."""
    listed = fetch(client["checksums"], MAX_CHECKSUMS).read(MAX_CHECKSUMS + 1)
    if len(listed) > MAX_CHECKSUMS:
        raise Refused("client-digest-mismatch", "the publisher's checksum list is larger than its bound")
    expected = publisher_checksum(listed, client["file"])
    archive = os.path.join(scratch, "client.tar.gz")
    digest = hashlib.sha256()
    descriptor = os.open(archive, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    with os.fdopen(descriptor, "wb") as target:
        stream, size = fetch(client["url"], MAX_CLIENT), 0
        for chunk in iter(lambda: stream.read(CHUNK), b""):
            size += len(chunk)
            if size > MAX_CLIENT:
                raise Refused("client-digest-mismatch", "the client archive is larger than its bound")
            digest.update(chunk)
            target.write(chunk)
    try:
        if digest.hexdigest() != expected:
            raise Refused("client-digest-mismatch", "the client archive's SHA-256 is not the one its publisher lists")
        with tarlib.open(archive, mode="r:gz") as opened:
            member = next((entry for entry in opened.getmembers() if entry.name.lstrip("./") == "oc"), None)
            if member is None or not member.isfile() or member.size > MAX_CLIENT:
                raise Refused("client-digest-mismatch", "the client archive holds no oc")
            binary = private_directory(os.path.join(scratch, "bin"))
            write_private(os.path.join(binary, "oc"), opened.extractfile(member).read(), 0o700)
    finally:
        os.unlink(archive)


def sweep(scratch, secrets_base, now):
    """Remove what a copy killed before its cleanup left: another run's mirror-release scratch and lent
    secrets older than any run lives, since a run ends within its deadline."""
    parent, own = os.path.dirname(os.path.dirname(scratch)), os.path.basename(os.path.dirname(scratch))
    stale = []
    for name in os.listdir(parent):
        if name != own and SCRATCH_NAME.match(name):
            stale.append((os.path.join(parent, name, "mirror-release"), os.path.join(parent, name)))
    for name in os.listdir(secrets_base):
        if name.startswith(SECRETS_PREFIX):
            stale.append((os.path.join(secrets_base, name), None))
    for path, holder in stale:
        try:
            status = os.lstat(path)
        except FileNotFoundError:
            continue
        if stat.S_ISDIR(status.st_mode) and status.st_uid == os.geteuid() and status.st_mtime < now - STALE_SECONDS:
            shutil.rmtree(path)
            if holder is not None:
                try:
                    os.rmdir(holder)
                except OSError:
                    pass


def prepare(fetch, client, scratch, auth, trust, secrets_base=SECRETS_BASE, now=None):
    """Fill the run directory with the client and a private home, and a private directory on tmpfs
    with the lent auth document and the anchor; return that directory."""
    scratch = scratch_checked(scratch)
    sweep(scratch, secrets_base, time.time() if now is None else now)
    try:
        download(fetch, client, scratch)
    except tarlib.TarError:
        raise Refused("client-digest-mismatch", "the client archive is no readable archive") from None
    except (HTTPError, URLError, OSError):
        raise Refused("client-unreachable", "the release's oc could not be read from its origin") from None
    for name in ("home", "config", "cache", "data", "runtime"):
        private_directory(os.path.join(scratch, name))
    lent = tempfile.mkdtemp(prefix=SECRETS_PREFIX, dir=secrets_base)
    try:
        write_private(os.path.join(lent, "auth.json"), auth)
        write_private(os.path.join(private_directory(os.path.join(lent, "trust")), "mirror.pem"), trust)
    except OSError:
        shutil.rmtree(lent, ignore_errors=True)
        raise
    return lent


def clean(scratch, lent="", secrets_base=SECRETS_BASE):
    """Remove the lent secrets directory and everything beneath the run directory, never following a link out of either."""
    if lent:
        if os.path.dirname(lent) != secrets_base or not os.path.basename(lent).startswith(SECRETS_PREFIX):
            raise ValueError("the lent directory is not one this module creates")
        status = os.lstat(lent)
        if not stat.S_ISDIR(status.st_mode) or status.st_uid != os.geteuid():
            raise ValueError("the lent directory is not this user's directory")
        shutil.rmtree(lent)
    status = os.lstat(scratch)
    if not stat.S_ISDIR(status.st_mode) or status.st_uid != os.geteuid() or status.st_mode & 0o077:
        raise ValueError("the run directory is not this user's private directory")
    for name in os.listdir(scratch):
        path = os.path.join(scratch, name)
        if stat.S_ISDIR(os.lstat(path).st_mode):
            shutil.rmtree(path)
        else:
            os.unlink(path)


def route(egress, mirror):
    host = mirror.rsplit(":", 1)[0]
    bypass = ",".join(list(egress.get("noProxy") or []) + [host, host.strip("[]")])
    values = {"http_proxy": egress.get("httpProxy") or "", "https_proxy": egress.get("httpsProxy") or "", "no_proxy": bypass}
    return dict(values, **{key.upper(): value for key, value in values.items()})


def mirror_reads(username, password, trust, timeout, base):
    """A GET and a HEAD of the mirror with the lent credential, verified against the anchor alone."""
    scratch = tempfile.mkdtemp(prefix="bootwright-release-trust-", dir=base)
    anchor = os.path.join(scratch, "ca.crt")
    write_private(anchor, trust)

    def open_mirror(url, accept, method):
        try:
            return open_url(url, method=method, headers={"Accept": accept}, url_username=username, url_password=password, force_basic_auth=True,
                            validate_certs=True, ca_path=anchor, use_proxy=False, follow_redirects="none", timeout=timeout)
        except HTTPError as error:
            return error
        except (URLError, OSError):
            raise Refused("mirror-unreadable", "the image mirror could not be reached") from None

    def get(url, accept, bound):
        response = open_mirror(url, accept, "GET")
        if response.getcode() != 200 or bound is None:
            return response.getcode(), {}, response
        body = response.read(bound + 1)
        if len(body) > bound:
            raise Refused("release-incomplete", "a release manifest in the image mirror is larger than its bound")
        return 200, {}, body

    def head(url):
        response = open_mirror(url, MANIFESTS, "HEAD")
        if response.getcode() == 404:
            return None
        if response.getcode() != 200:
            raise Refused("mirror-unreadable", "the image mirror answered %d for a release component" % response.getcode())
        return response.headers.get("docker-content-digest") or ""

    return get, head, scratch


def publisher_fetch(egress, mirror, timeout):
    """A GET of the vendor origin through the frozen route, with the system trust."""
    os.environ.update(route(egress, mirror))

    def fetch(url, _bound):
        return open_url(url, method="GET", validate_certs=True, use_proxy=True, follow_redirects="none", timeout=timeout)

    return fetch


def main():
    secret = {"type": "str", "required": False, "no_log": True}
    module = AnsibleModule(argument_spec={
        "mode": {"type": "str", "required": True, "choices": ["read", "prepare", "clean"]},
        "release": {"type": "dict", "required": False}, "client": {"type": "dict", "required": False},
        "mirror": {"type": "str", "required": False}, "egress": {"type": "dict", "required": False},
        "username": secret, "password": secret, "trust": secret, "auth": secret, "scratch": {"type": "path", "required": False},
        "lent": {"type": "path", "required": False, "default": ""},
        "timeout": {"type": "int", "default": 60},
    }, required_if=[
        ("mode", "read", ("release", "client", "mirror", "egress", "username", "password", "trust")),
        ("mode", "prepare", ("release", "client", "mirror", "egress", "username", "password", "trust", "auth", "scratch")),
        ("mode", "clean", ("scratch",)),
    ], supports_check_mode=False)
    params = module.params
    if params["mode"] == "clean":
        try:
            clean(params["scratch"], params["lent"])
        except (ValueError, OSError):
            module.fail_json(msg="the release copy's run directory could not be safely emptied")
        module.exit_json(changed=True)
    get, head, scratch = mirror_reads(params["username"], params["password"], params["trust"], params["timeout"], module.tmpdir)
    try:
        if params["mode"] == "prepare":
            checked(params["release"], params["mirror"])
            fetch = publisher_fetch(params["egress"], params["mirror"], params["timeout"])
            lent = prepare(fetch, params["client"], params["scratch"], params["auth"], params["trust"])
            module.exit_json(changed=True, lent=lent)
        digest, components, missing = held(get, head, params["mirror"], params["release"])
    except Refused as refused:
        module.fail_json(msg=str(refused), reason=refused.reason)
    except ValueError as error:
        module.fail_json(msg=str(error))
    finally:
        for name in os.listdir(scratch):
            os.unlink(os.path.join(scratch, name))
        os.rmdir(scratch)
    module.exit_json(changed=False, digest=digest, components=components, missing=missing)


if __name__ == "__main__":
    main()
