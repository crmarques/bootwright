#!/usr/bin/python
"""Acquire one pinned container image, from its declared mirror when it has one."""

from __future__ import annotations

DOCUMENTATION = r"""
module: image_acquire
short_description: Pull one image pinned by digest, binding a mirror's credential and trust anchor
version_added: "0.1.0"
description:
  - Does nothing when the host already holds the reference, which carries its digest.
  - Otherwise pulls it over HTTPS with TLS verification on, through the frozen egress route
    alone, never through an insecure-registry setting or the host's registry configuration.
  - A credential and a trust anchor reach the pull only as files in a private directory this
    module creates for the one pull and removes after it, whatever its result.
  - A credential is lent only to the registry host the reference names.
options:
  image:
    description: The reference, pinned by its sha256 digest.
    type: str
    required: true
  egress:
    description: The frozen route, with httpProxy, httpsProxy and noProxy.
    type: dict
    required: true
  host:
    description: The mirror registry host the credential and trust anchor belong to.
    type: str
    default: ''
  username:
    description: The mirror's user name. Never returned or logged.
    type: str
    default: ''
  password:
    description: The mirror's password. Never returned or logged.
    type: str
    default: ''
  auth:
    description: A container auth document for the mirror. Never returned or logged.
    type: str
    default: ''
  trust:
    description: The PEM trust anchor the mirror's certificate is verified against.
    type: str
    default: ''
author:
  - Bootwright contributors (@crmarques)
"""
EXAMPLES = r"""
- name: Acquire the pinned service image
  bootwright.core.image_acquire:
    image: '{{ request.image }}'
    egress: '{{ request.egress }}'
    host: '{{ request.imageSource.host | default("") }}'
    trust: "{{ lookup('ansible.builtin.file', material.imageTrust, rstrip=false) if material.imageTrust is defined else '' }}"
  no_log: true
"""
RETURN = r"""
image:
  description: The reference acquired.
  returned: always
  type: str
"""

import base64
import json
import os
import re
import shutil
import tempfile

from ansible.module_utils.basic import AnsibleModule

PODMAN = "/usr/bin/podman"
PINNED = re.compile(r"[a-z0-9][^@\s]*@sha256:[0-9a-f]{64}\Z")


def registry_host(image):
    """The registry host a reference names; a short name is Docker Hub's."""
    first = image.split("/", 1)[0]
    if "/" in image and ("." in first or ":" in first or first == "localhost"):
        return first
    return "docker.io"


def entry_host(key):
    return key.split("://", 1)[-1].split("/", 1)[0]


def auth_document(host, username, password, auth):
    """The auth file's text for this pull: the mirror's own entry alone, an
    empty document for a mirror that binds no credential, or None without one."""
    if auth:
        try:
            entries = json.loads(auth).get("auths") or {}
        except (ValueError, AttributeError):
            raise ValueError("the mirror's auth document is not a JSON object") from None
        kept = {key: value for key, value in entries.items() if entry_host(key) == host}
        if not kept:
            raise ValueError("the mirror's auth document holds no entry for %s" % host)
        return json.dumps({"auths": kept})
    if not username and not password:
        return json.dumps({"auths": {}}) if host else None
    if not username or not password:
        raise ValueError("the mirror credential is missing its user name or its password")
    token = base64.b64encode(("%s:%s" % (username, password)).encode("utf-8")).decode("ascii")
    return json.dumps({"auths": {host: {"auth": token}}})


def environment(egress):
    route = {}
    for name, key in (("http_proxy", "httpProxy"), ("https_proxy", "httpsProxy")):
        route[name] = route[name.upper()] = egress.get(key) or ""
    route["no_proxy"] = route["NO_PROXY"] = ",".join(egress.get("noProxy") or [])
    return route


def write_private(path, text):
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
        stream.write(text)


def pull_arguments(image, scratch, host, document, trust):
    """The pull's argv, writing its credential and anchor beneath scratch. A
    mirror pull reads neither the host's own login nor its certs.d."""
    argv = [PODMAN, "pull", "--quiet", "--tls-verify=true"]
    if document is not None:
        write_private(os.path.join(scratch, "auth.json"), document)
        argv.append("--authfile=" + os.path.join(scratch, "auth.json"))
    if host:
        certificates = os.path.join(scratch, "certificates")
        os.mkdir(certificates, 0o700)
        if trust:
            write_private(os.path.join(certificates, "ca.crt"), trust)
        argv.append("--cert-dir=" + certificates)
    return argv + [image]


def acquire(run, image, egress, host="", username="", password="", auth="", trust="", base=None):
    """Pull image unless the host holds it, and report whether it pulled."""
    if PINNED.match(image) is None:
        raise ValueError("the image is not a reference pinned by its sha256 digest")
    if (host or username or password or auth or trust) and not (host and image.startswith(host + "/")):
        raise ValueError("the mirror's credential and trust anchor belong to %s, not to the image's registry" % (host or "no host"))
    document = auth_document(host, username, password, auth)
    held, _out, _err = run([PODMAN, "image", "exists", image], None)
    if held == 0:
        return False
    scratch = tempfile.mkdtemp(prefix="bootwright-image-", dir=base)
    try:
        code, _out, err = run(pull_arguments(image, scratch, host, document, trust), environment(egress))
    finally:
        shutil.rmtree(scratch, ignore_errors=True)
    if code != 0:
        detail = (err or "").strip().splitlines()
        raise RuntimeError("%s could not be pulled from %s: %s" % (image, registry_host(image), detail[-1] if detail else "podman reported no reason"))
    return True


def main():
    secret = {"type": "str", "default": "", "no_log": True}
    module = AnsibleModule(argument_spec={
        "image": {"type": "str", "required": True},
        "egress": {"type": "dict", "required": True},
        "host": {"type": "str", "default": ""},
        "username": secret, "password": secret, "auth": secret,
        "trust": {"type": "str", "default": ""},
    }, supports_check_mode=False)
    params = module.params

    def run(argv, route):
        return module.run_command(argv, environ_update=route or {})

    try:
        changed = acquire(run, params["image"], params["egress"], params["host"], params["username"],
                          params["password"], params["auth"], params["trust"], module.tmpdir)
    except (ValueError, RuntimeError) as error:
        module.fail_json(msg=str(error))
    module.exit_json(changed=changed, image=params["image"])


if __name__ == "__main__":
    main()
