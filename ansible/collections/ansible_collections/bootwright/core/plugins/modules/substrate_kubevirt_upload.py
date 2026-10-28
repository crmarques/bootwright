#!/usr/bin/python
"""Upload one image into a prepared DataVolume with the controller's virtctl."""

from __future__ import annotations

DOCUMENTATION = r"""
module: substrate_kubevirt_upload
short_description: Upload one image through the CDI upload proxy
version_added: "0.1.0"
description:
  - Runs C(virtctl image-upload dv --no-create) with the image file into an
    upload DataVolume the arm created and proved ready, by an argument list
    and never a shell.
  - The upload proxy is verified against the ingress certificate authority the
    arm read through the verified API, which is the process's only trust
    anchor; C(--insecure) is never passed, and no proxy is inherited.
options:
  virtctl:
    description: The absolute path of the controller's virtctl.
    type: path
    required: true
  kubeconfig:
    description: The path of the lent kubeconfig file.
    type: path
    required: true
  trust:
    description: The path of the ingress certificate authority file.
    type: path
    required: true
  namespace:
    description: The provider's namespace.
    type: str
    required: true
  volume:
    description: The upload DataVolume.
    type: str
    required: true
  image:
    description: The path of the image file.
    type: path
    required: true
  proxy:
    description: The https URL of the upload proxy.
    type: str
    required: true
  seconds:
    description: The bound of the upload.
    type: int
    required: true
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
# Invoked by the substrate_kubevirt_api action only.
- name: Upload the image
  bootwright.core.substrate_kubevirt_upload:
    virtctl: '{{ virtctl }}'
    kubeconfig: '{{ kubeconfig_file }}'
    trust: '{{ ingress_authority_file }}'
    namespace: guests
    volume: node-0-0a1b2c3d-media
    image: '{{ image_file }}'
    proxy: https://cdi-uploadproxy.apps.example.com
    seconds: 1200
"""

RETURN = r"""
changed:
  description: Whether the image was uploaded.
  returned: always
  type: bool
"""

import os
import re
from urllib.parse import urlsplit

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils import bounded_process

NAME = re.compile(r"[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?\Z")


def argv(params):
    parts = urlsplit(params["proxy"])
    if parts.scheme != "https" or not parts.hostname or parts.username or parts.query or parts.fragment:
        raise ValueError("the upload proxy is not one https URL")
    for value in (params["namespace"], params["volume"]):
        if not NAME.match(value):
            raise ValueError("a namespace or DataVolume name is not a DNS label")
    for value in (params["virtctl"], params["kubeconfig"], params["trust"], params["image"]):
        if not os.path.isabs(value):
            raise ValueError("a path is not absolute")
    return [params["virtctl"], "image-upload", "dv", params["volume"], "--no-create",
            "--namespace", params["namespace"], "--kubeconfig", params["kubeconfig"],
            "--image-path", params["image"], "--uploadproxy-url", params["proxy"],
            "--wait-secs", str(max(60, int(params["seconds"]) // 4))]


def environment(params):
    # Go's TLS stack reads SSL_CERT_FILE as the whole system pool on Linux, so
    # the ingress authority is the one anchor of the upload proxy's
    # connection; no proxy variable is passed, so none is used.
    return {"HOME": "/nonexistent", "PATH": "/usr/bin:/bin", "LANG": "C.UTF-8",
            "SSL_CERT_FILE": params["trust"], "SSL_CERT_DIR": "/nonexistent"}


def main():
    module = AnsibleModule(argument_spec={
        "virtctl": {"type": "path", "required": True}, "kubeconfig": {"type": "path", "required": True, "no_log": True},
        "trust": {"type": "path", "required": True}, "namespace": {"type": "str", "required": True},
        "volume": {"type": "str", "required": True}, "image": {"type": "path", "required": True},
        "proxy": {"type": "str", "required": True}, "seconds": {"type": "int", "required": True},
    })
    try:
        command = argv(module.params)
    except ValueError as refused:
        module.fail_json(msg=str(refused))
    try:
        code, ignored_output, error = bounded_process.run(command, b"", module.params["seconds"], 1 << 20, environment=environment(module.params))
    except bounded_process.Failure as failure:
        module.fail_json(msg="virtctl image-upload did not finish: %s" % failure.kind)
    if code != 0:
        lines = error.decode("utf-8", "replace").strip().splitlines()
        line = "".join(c for c in (lines[-1] if lines else "") if c.isprintable())
        module.fail_json(msg=("virtctl image-upload failed: " + line)[:512])
    module.exit_json(changed=True)


if __name__ == "__main__":
    main()
