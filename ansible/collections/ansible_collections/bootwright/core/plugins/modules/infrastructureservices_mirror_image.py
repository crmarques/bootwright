#!/usr/bin/python
"""Copy pinned images by digest into the managed image mirror and verify them there."""

from __future__ import annotations

DOCUMENTATION = r"""
module: infrastructureservices_mirror_image
short_description: Copy each pinned image into the image mirror by digest and read its digest back
version_added: "0.1.0"
description:
  - Asks the mirror for each image's manifest by its digest and copies nothing the mirror already holds.
  - Copies every other image from its public origin with C(skopeo copy --all --preserve-digests) to a tag
    derived from its digest, through the frozen egress route, then reads back the digest the mirror holds
    under that tag and fails unless it is the pinned one.
  - The mirror's credential and trust anchor reach skopeo only as files in a private directory this module
    creates for the run and removes after it, whatever its result, and are lent to the mirror's own host alone.
  - With C(observe), copies nothing and reports which images the mirror holds.
options:
  images:
    description: The frozen images, each with repository, digest and source.
    type: list
    elements: dict
    required: true
  mirror:
    description: The mirror registry's host and port.
    type: str
    required: true
  egress:
    description: The frozen route, with httpProxy, httpsProxy and noProxy.
    type: dict
    required: true
  username:
    description: The mirror's user name. Never returned or logged.
    type: str
    required: true
  password:
    description: The mirror's password. Never returned or logged.
    type: str
    required: true
  trust:
    description: The PEM trust anchor the mirror's certificate is verified against. Never returned or logged.
    type: str
    required: true
  observe:
    description: Whether to report what the mirror holds and copy nothing.
    type: bool
    default: false
  timeout:
    description: The seconds one mirror read may take.
    type: int
    default: 60
author:
  - Bootwright contributors (@crmarques)
"""
EXAMPLES = r"""
- name: Copy the pinned images into the image mirror
  bootwright.core.infrastructureservices_mirror_image:
    images: '{{ request.images }}'
    mirror: '{{ request.mirror.host }}'
    egress: '{{ request.egress }}'
    username: "{{ lookup('ansible.builtin.file', material.imageUsername, rstrip=false) }}"
    password: "{{ lookup('ansible.builtin.file', material.imagePassword, rstrip=false) }}"
    trust: "{{ lookup('ansible.builtin.file', material.imageTrust, rstrip=false) }}"
  no_log: true
"""
RETURN = r"""
images:
  description: Each image the mirror holds, with repository, digest and whether its digest was verified.
  returned: success
  type: list
  elements: dict
reason:
  description: The refusal a failure names, image-digest-mismatch, image-unreachable or mirror-unreadable.
  returned: failure
  type: str
"""

import base64
import json
import os
import re
import shutil
import tempfile

from urllib.error import HTTPError, URLError

from ansible.module_utils.basic import AnsibleModule
from ansible.module_utils.urls import open_url

SKOPEO = "/usr/bin/skopeo"
DIGEST = re.compile(r"sha256:[0-9a-f]{64}\Z")
REPOSITORY = re.compile(r"[a-z0-9]([a-z0-9.-]*[a-z0-9])?(:[0-9]{1,5})?(/[a-z0-9]+([._-]+[a-z0-9]+)*)+\Z")
MIRROR = re.compile(r"([a-z0-9]([a-z0-9.-]*[a-z0-9])?|\[[0-9a-f:.]+\]):[0-9]{1,5}\Z")
MANIFESTS = ", ".join((
    "application/vnd.oci.image.index.v1+json",
    "application/vnd.docker.distribution.manifest.list.v2+json",
    "application/vnd.oci.image.manifest.v1+json",
    "application/vnd.docker.distribution.manifest.v2+json",
))


class Refused(Exception):
    """A failure the runner reports under its own refusal."""

    def __init__(self, reason, message):
        super().__init__(message)
        self.reason = reason


def tag(digest):
    """The deterministic tag an image is pushed under: its digest, spelled as a tag."""
    return digest.replace(":", "-", 1)


def checked(images, mirror):
    if MIRROR.match(mirror) is None:
        raise ValueError("the image mirror is not a host and port")
    if not isinstance(images, list) or not images:
        raise ValueError("the request names no image")
    for image in images:
        repository, digest = image.get("repository"), image.get("digest")
        if not isinstance(repository, str) or REPOSITORY.match(repository) is None or not isinstance(digest, str) or DIGEST.match(digest) is None:
            raise ValueError("an image is not a repository pinned by its sha256 digest")
        if image.get("source") != repository + "@" + digest:
            raise ValueError("an image's source is not its repository at its digest")
    return images


def head(read, mirror, repository, reference):
    """The digest the mirror serves for the reference, or None when it holds none."""
    try:
        status, headers = read("https://%s/v2/%s/manifests/%s" % (mirror, repository, reference))
    except Refused:
        raise
    except Exception:
        raise Refused("mirror-unreadable", "the image mirror at %s could not be read" % mirror) from None
    if status == 404:
        return None
    if status != 200:
        raise Refused("mirror-unreadable", "the image mirror at %s answered %d for %s" % (mirror, status, repository))
    return headers.get("docker-content-digest") or ""


def environment(egress, mirror):
    route = {}
    for name, key in (("http_proxy", "httpProxy"), ("https_proxy", "httpsProxy")):
        route[name] = route[name.upper()] = egress.get(key) or ""
    host = mirror.rsplit(":", 1)[0]
    bypass = list(egress.get("noProxy") or []) + [host, host.strip("[]")]
    route["no_proxy"] = route["NO_PROXY"] = ",".join(bypass)
    return route


def write_private(path, text):
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
        stream.write(text)


def lend(scratch, mirror, username, password, trust):
    """Write the mirror's auth file and certificate directory beneath scratch."""
    token = base64.b64encode(("%s:%s" % (username, password)).encode("utf-8")).decode("ascii")
    write_private(os.path.join(scratch, "auth.json"), json.dumps({"auths": {mirror: {"auth": token}}}))
    certificates = os.path.join(scratch, "certificates")
    os.mkdir(certificates, 0o700)
    write_private(os.path.join(certificates, "ca.crt"), trust)
    return os.path.join(scratch, "auth.json"), certificates


def copy_arguments(image, mirror, authfile, certificates):
    """The copy's argv: the origin is read anonymously with the system trust, the
    mirror with the lent credential and anchor alone."""
    return [
        SKOPEO, "copy", "--all", "--preserve-digests", "--retry-times", "2",
        "--src-no-creds", "--src-tls-verify=true",
        "--dest-tls-verify=true", "--dest-authfile", authfile, "--dest-cert-dir", certificates,
        "docker://" + image["source"],
        "docker://%s/%s:%s" % (mirror, image["repository"], tag(image["digest"])),
    ]


def fill(run, read, images, mirror, egress, username, password, trust, observe=False, base=None):
    """Copy every image the mirror lacks, verify each, and report what it holds."""
    images = checked(images, mirror)
    if not username or not password or not trust:
        raise ValueError("the image mirror's credential or trust anchor is missing")
    held, changed = [], False
    scratch = None
    try:
        for image in images:
            present = head(read, mirror, image["repository"], image["digest"])
            if present is None and observe:
                continue
            if present is None:
                if scratch is None:
                    scratch = tempfile.mkdtemp(prefix="bootwright-mirror-", dir=base)
                    authfile, certificates = lend(scratch, mirror, username, password, trust)
                code, _out, err = run(copy_arguments(image, mirror, authfile, certificates), environment(egress, mirror))
                if code != 0:
                    detail = (err or "").strip().splitlines()
                    raise Refused("image-unreachable", "%s could not be copied into the image mirror: %s" % (
                        image["source"], detail[-1] if detail else "skopeo reported no reason"))
                changed = True
                present = head(read, mirror, image["repository"], tag(image["digest"]))
                if present is None:
                    raise Refused("mirror-unreadable", "the image mirror holds no %s under its tag after the copy" % image["repository"])
            if present != image["digest"]:
                raise Refused("image-digest-mismatch", "the image mirror holds %s at another digest than the pinned one" % image["repository"])
            held.append({"repository": image["repository"], "digest": image["digest"], "verified": True})
    finally:
        if scratch is not None:
            shutil.rmtree(scratch, ignore_errors=True)
    return changed, held


def reader(username, password, trust, timeout, base):
    """A HEAD of one mirror URL with the lent credential, verified against the anchor alone."""
    scratch = tempfile.mkdtemp(prefix="bootwright-mirror-trust-", dir=base)
    anchor = os.path.join(scratch, "ca.crt")
    write_private(anchor, trust)

    def read(url):
        try:
            response = open_url(url, method="HEAD", headers={"Accept": MANIFESTS}, url_username=username, url_password=password,
                                force_basic_auth=True, validate_certs=True, ca_path=anchor, use_proxy=False, follow_redirects="none",
                                timeout=timeout)
        except HTTPError as error:
            return error.code, {}
        except URLError:
            raise Refused("mirror-unreadable", "the image mirror could not be reached") from None
        return response.getcode(), {key.lower(): value for key, value in response.headers.items()}

    return read, scratch


def main():
    secret = {"type": "str", "required": True, "no_log": True}
    module = AnsibleModule(argument_spec={
        "images": {"type": "list", "elements": "dict", "required": True},
        "mirror": {"type": "str", "required": True},
        "egress": {"type": "dict", "required": True},
        "username": secret, "password": secret,
        "trust": {"type": "str", "required": True, "no_log": True},
        "observe": {"type": "bool", "default": False},
        "timeout": {"type": "int", "default": 60},
    }, supports_check_mode=False)
    params = module.params

    def run(argv, route):
        return module.run_command(argv, environ_update=route)

    read, scratch = reader(params["username"], params["password"], params["trust"], params["timeout"], module.tmpdir)
    try:
        changed, held = fill(run, read, params["images"], params["mirror"], params["egress"], params["username"],
                             params["password"], params["trust"], params["observe"], module.tmpdir)
    except Refused as refused:
        module.fail_json(msg=str(refused), reason=refused.reason)
    except ValueError as error:
        module.fail_json(msg=str(error))
    finally:
        shutil.rmtree(scratch, ignore_errors=True)
    module.exit_json(changed=changed, images=held)


if __name__ == "__main__":
    main()
