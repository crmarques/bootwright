#!/usr/bin/python
"""Observe what one cluster's installation recorded, without changing any of it."""

from __future__ import annotations

DOCUMENTATION = r"""
module: containercluster_install_inspect
short_description: Observe one container cluster's own installation record
version_added: "0.1.0"
description:
  - Reports this build's identity, a domain-separated SHA-256 of the
    administrator client certificate in the kubeconfig the installer wrote
    beside the image, and the address of the image its media block published.
  - Each time C(agent wait-for install-complete) rewrites that kubeconfig,
    prepending the router CA bundle to its certificate authority and adding
    C(apiVersion) and C(kind), the identity stays the same, while the file
    stays within the 64 KiB this module reads.
  - A kubeconfig larger than that, in any other shape, or one that disables
    verification or authenticates another way, names no identity.
  - Performs no change and is safe to repeat.
options:
  request:
    description: The frozen installation request.
    type: dict
    required: true
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Observe the installation record
  bootwright.core.containercluster_install_inspect:
    request: '{{ bootwright_cluster_install_request }}'
"""

RETURN = r"""
observation:
  description: This build's identity and the published image address.
  returned: always
  type: dict
"""

import base64
import binascii
import hashlib
import os
import re

from ansible.module_utils.basic import AnsibleModule

# KUBECONFIG is the administrator kubeconfig `openshift-install agent create
# image` writes into the work area together with the image, and IMAGE the name
# the media block published the image under inside the unguessable directory
# that attempt minted. The agent installer writes no metadata.json: its image
# target persists only the image, this kubeconfig and the administrator
# password (cmd/openshift-install/agent.go, agentImageTarget, release-4.21).
KUBECONFIG = os.path.join("auth", "kubeconfig")
IMAGE = "agent.iso"
# A larger kubeconfig names no identity. Each install-complete run grows the
# file by the base64 of the router CA bundle (see TYPED), so the identity
# outlives only as many rewrites as fit within this bound.
MAX_KUBECONFIG = 65536

# The installer marshals one clientcmd v1 Config through sigs.k8s.io/yaml:
# block style, sorted keys, one cluster, one user and one context, each value a
# plain scalar unless it would read as another type, as a cluster named "on"
# would, when it is double-quoted (pkg/asset/kubeconfig/kubeconfig.go; the
# emitter's stringv in go.yaml.in/yaml/v2 encode.go). A key outside this set,
# such as insecure-skip-tls-verify, a certificate-authority path, a token or
# tokenFile, a username and password, an auth provider or an exec plugin, would
# let a read through the file succeed without the certificates it names, so a
# kubeconfig carrying one, or any shape the installer does not write, names no
# identity.
KEYS = frozenset((
    "certificate-authority-data", "client-certificate-data", "client-key-data", "cluster",
    "clusters", "context", "contexts", "current-context", "name", "preferences", "server",
    "user", "users",
))
# `agent wait-for install-complete` rewrites the file once the cluster
# initializes, and again on every later run: addRouterCAToClusterCA prepends
# the default-ingress-cert router CA bundle to each cluster's
# certificate-authority-data and writes the file back through
# clientcmd.WriteToFile (cmd/openshift-install/command/waitfor.go,
# WaitForInstallComplete and addRouterCAToClusterCA, release-4.21). That
# encodes through clientcmdlatest.Codec, which sets `apiVersion: v1` and
# `kind: Config` and otherwise marshals through the same sigs.k8s.io/yaml
# (client-go tools/clientcmd/loader.go Write, api/latest/latest.go and
# api/v1/register.go SetGroupVersionKind, v0.34.1). Those two lines are
# admitted together or not at all, each once, at the top level, with exactly
# the values the codec writes.
TYPED = {"apiVersion": "v1", "kind": "Config"}
ENTRY = re.compile(r'^( *)(- )?([a-z][A-Za-z-]*):(?: ([A-Za-z0-9+/=:._-]+|"[A-Za-z0-9._-]+"|\{\}))?$')
SINGLE = ("clusters", "contexts", "users")
AUTHORITY = "certificate-authority-data"
CLIENT = "client-certificate-data"
PEM = b"-----BEGIN CERTIFICATE-----"
# The identity's domain: a versioned prefix, so it never equals the digest of
# the authority and client certificate the inspection named before S26, nor a
# bare SHA-256 of the certificate.
DOMAIN = b"bootwright/containercluster/identity/v2\x00"


def placed(indent, item, key, value):
    """Whether one line is where and what the installer writes."""
    if key in TYPED:
        return not indent and not item and value == TYPED[key]
    return key in KEYS


def scalars(text):
    """Every value by key, or nothing when the file is not the installer's shape."""
    values, items, section = {}, {}, None
    for line in text.splitlines():
        entry = ENTRY.match(line)
        if entry is None or not placed(*entry.groups()):
            return None
        indent, item, key, value = entry.groups()
        if not indent and item:
            if section not in SINGLE:
                return None
            items[section] = items.get(section, 0) + 1
        elif not indent:
            section = key
        values.setdefault(key, []).append(value or "")
    if any(items.get(name) != 1 for name in SINGLE):
        return None
    if [len(values.get(key, [])) for key in TYPED] not in ([0, 0], [1, 1]):
        return None
    return values


def certificate(values):
    """The PEM one base64 kubeconfig value carries, or nothing."""
    if len(values) != 1:
        return b""
    try:
        decoded = base64.b64decode(values[0], validate=True)
    except (binascii.Error, ValueError):
        return b""
    return decoded if decoded.startswith(PEM) else b""


def identity(path):
    """This build's own identity, or nothing.

    The administrator client certificate is signed by a signer minted when the
    image was built (AgentAdminClient's AdminKubeConfigClientCertKey, from
    pkg/asset/kubeconfig/agent.go and pkg/asset/tls/adminkubeconfig.go,
    release-4.21), so only the cluster that image installs accepts it, and the
    install-complete rewrite leaves it byte for byte as it was. The certificate
    authority it verifies the serving certificate against is required, because
    a read that succeeds through the file must have verified that certificate,
    but it is not hashed: the rewrite prepends the router CA to it. Only the
    client certificate is hashed; the client key is read with the file and
    never leaves this function.
    """
    try:
        if os.path.getsize(path) > MAX_KUBECONFIG:
            return ""
        with open(path, "rb") as handle:
            text = handle.read().decode("utf-8")
    except (OSError, UnicodeDecodeError):
        return ""
    values = scalars(text)
    if values is None:
        return ""
    client = certificate(values.get(CLIENT, []))
    if not certificate(values.get(AUTHORITY, [])) or not client:
        return ""
    return hashlib.sha256(DOMAIN + client).hexdigest()


def published(root, base):
    """The address the image is published at, or nothing.

    The final segment is the unguessable one the publishing attempt minted, so
    this is the only place it is read, and every task that carries the result
    keeps it out of its own output.
    """
    try:
        entries = sorted(os.listdir(root))
    except OSError:
        return ""
    for entry in entries:
        if os.path.isfile(os.path.join(root, entry, IMAGE)):
            return base + "/" + entry + "/" + IMAGE
    return ""


def observe(request):
    """What this installation's work area and publication hold now."""
    image = request["image"]
    return {
        "identity": identity(os.path.join(request["workRoot"], KUBECONFIG)),
        "kubeconfig": os.path.isfile(os.path.join(request["workRoot"], KUBECONFIG)),
        "url": published(image["path"], image["url"]),
    }


def main():
    module = AnsibleModule(
        argument_spec={"request": {"type": "dict", "required": True}},
        supports_check_mode=True,
    )
    module.exit_json(changed=False, observation=observe(module.params["request"]))


if __name__ == "__main__":
    main()
