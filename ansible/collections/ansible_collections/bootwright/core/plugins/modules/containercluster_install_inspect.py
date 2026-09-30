#!/usr/bin/python
"""Observe what one cluster's installation recorded, and keep its kubeconfig whole."""

from __future__ import annotations

DOCUMENTATION = r"""
module: containercluster_install_inspect
short_description: Observe one container cluster's own installation record
version_added: "0.1.0"
description:
  - Reports this build's identity, a domain-separated SHA-256 of the
    administrator client certificate in the copy of the installer's
    kubeconfig this installation keeps beside it, and the address of the image
    its media block published.
  - The kept copy is the installer's file as last seen whole, within the
    64 KiB this module reads and naming the same identity, so it never grows
    past that bound however often C(agent wait-for install-complete) rewrites
    the installer's own file, and never holds a file that write truncated.
  - A kept copy that is missing, larger than that bound, not whole, in any
    other shape, or that disables verification or authenticates another way,
    names no identity.
  - Without O(keep) it performs no change and is safe to repeat.
options:
  request:
    description: The frozen installation request.
    type: dict
    required: true
  keep:
    description:
      - Replace the kept copy with the installer's file, in one rename, when
        that file is whole, within the bound and names an identity, and the
        kept copy is missing or names that same identity.
      - A kept copy that names no identity or another one is never replaced.
    type: bool
    default: false
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Observe the installation record
  bootwright.core.containercluster_install_inspect:
    request: '{{ bootwright_cluster_install_request }}'

- name: Keep the installer's kubeconfig while it is whole, then observe
  bootwright.core.containercluster_install_inspect:
    request: '{{ bootwright_cluster_install_request }}'
    keep: true
"""

RETURN = r"""
observation:
  description: This build's identity, whether the kept copy exists, and the published image address.
  returned: always
  type: dict
"""

import base64
import binascii
import hashlib
import os
import re
import stat
import tempfile

from ansible.module_utils.basic import AnsibleModule

# KUBECONFIG is the administrator kubeconfig `openshift-install agent create
# image` writes into the work area together with the image, and IMAGE the name
# the media block published the image under inside the unguessable directory
# that attempt minted. The agent installer writes no metadata.json: its image
# target persists only the image, this kubeconfig and the administrator
# password (cmd/openshift-install/agent.go, agentImageTarget, release-4.21).
KUBECONFIG = os.path.join("auth", "kubeconfig")
IMAGE = "agent.iso"
# KEPT is this installation's own copy of that kubeconfig, beside the mark the
# installation leaves in the same work area, and the file every read of the
# cluster goes through. The installer rewrites its own file in place
# (clientcmd.WriteToFile calls os.WriteFile, which truncates before it writes,
# in the client-go v0.34.1 the installer vendors at release-4.21), so a kill
# during that write can leave it cut short, and each install-complete run grows
# it (see TYPED). The kept copy is replaced only in one rename, and only by a
# whole file within MAX_KUBECONFIG that names the identity it already names.
KEPT = ".bootwright-kubeconfig"
# A larger kubeconfig names no identity and is never kept. Each install-complete
# run grows the installer's file by the base64 of the router CA bundle, so the
# kept copy follows only as many rewrites as fit within this bound and then
# stays at the last one that did.
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
KEY = "client-key-data"
# Every key the installer writes in every form; only preferences, apiVersion and
# kind may be absent. The emitter sorts keys and client-key-data sorts last
# under the last top-level key, so a file cut short at a line boundary lacks at
# least that line, and one cut inside a line lacks its final newline.
REQUIRED = KEYS - {"preferences"}
# One PEM block as Go's encoding/pem writes it: the type line, base64 lines, the
# matching end line, each ending in a newline. A value cut short ends inside a
# block and matches none.
BLOCK = re.compile(rb"-----BEGIN ([A-Z0-9 ]+)-----\n(?:[A-Za-z0-9+/=]+\n)*-----END \1-----\n")
CERTIFICATE = re.compile(rb"CERTIFICATE\Z")
PRIVATE_KEY = re.compile(rb"(?:[A-Z0-9]+ )?PRIVATE KEY\Z")
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


def blocks(values, kind):
    """The whole PEM blocks of one kind one base64 kubeconfig value carries, or nothing."""
    if len(values) != 1:
        return b""
    try:
        decoded = base64.b64decode(values[0], validate=True)
    except (binascii.Error, ValueError):
        return b""
    position = 0
    while position < len(decoded):
        block = BLOCK.match(decoded, position)
        if block is None or kind.match(block.group(1)) is None:
            return b""
        position = block.end()
    return decoded


def named(data):
    """This build's own identity in one whole kubeconfig, or nothing.

    The administrator client certificate is signed by a signer minted when the
    image was built (AgentAdminClient's AdminKubeConfigClientCertKey, from
    pkg/asset/kubeconfig/agent.go and pkg/asset/tls/adminkubeconfig.go,
    release-4.21), so only the cluster that image installs accepts it, and the
    install-complete rewrite leaves it byte for byte as it was. The certificate
    authority it verifies the serving certificate against is required, because
    a read that succeeds through the file must have verified that certificate,
    but it is not hashed: the rewrite prepends the router CA to it. Only the
    client certificate is hashed. The client key is required whole, because a
    file cut short inside it would otherwise name the identity of a file no
    read can authenticate with, and it never leaves this function.
    """
    try:
        text = data.decode("utf-8")
    except UnicodeDecodeError:
        return ""
    if not text.endswith("\n"):
        return ""
    values = scalars(text)
    if values is None or not REQUIRED.issubset(values):
        return ""
    client = blocks(values[CLIENT], CERTIFICATE)
    if not blocks(values[AUTHORITY], CERTIFICATE) or not client or not blocks(values[KEY], PRIVATE_KEY):
        return ""
    return hashlib.sha256(DOMAIN + client).hexdigest()


def bounded(path):
    """The bytes of one regular file within the read bound, or nothing."""
    try:
        descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    except OSError:
        return None
    with os.fdopen(descriptor, "rb") as handle:
        try:
            if not stat.S_ISREG(os.fstat(handle.fileno()).st_mode):
                return None
            data = handle.read(MAX_KUBECONFIG + 1)
        except OSError:
            return None
    return data if len(data) <= MAX_KUBECONFIG else None


def identity(path):
    """The identity the kubeconfig at path names, or nothing."""
    data = bounded(path)
    return "" if data is None else named(data)


def publish(directory, target, data):
    """Replace target with data in one rename, so it is never seen part written."""
    descriptor, temporary = tempfile.mkstemp(prefix=KEPT + ".", dir=directory)
    try:
        with os.fdopen(descriptor, "wb") as handle:
            handle.write(data)
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temporary, target)
        temporary = None
    finally:
        if temporary is not None:
            os.unlink(temporary)
    parent = os.open(directory, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(parent)
    finally:
        os.close(parent)


def keep(request, check_mode=False):
    """Whether the kept copy follows the installer's file now, which it does only while that file is whole.

    The installer's file is kept only when it is within the bound, whole and
    names an identity, and only over a kept copy that is missing or names that
    same identity, so the copy every read goes through never grows past the
    bound, never holds a truncated write and never changes identity. A kept
    copy that names none, or another one, stays as it is.
    """
    work = request["workRoot"]
    source = bounded(os.path.join(work, KUBECONFIG))
    anchor = "" if source is None else named(source)
    if not anchor:
        return False
    target = os.path.join(work, KEPT)
    if os.path.lexists(target):
        current = bounded(target)
        if current is None or current == source or named(current) != anchor:
            return False
    if not check_mode:
        publish(work, target, source)
    return True


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
    """What this installation's work area and publication hold now.

    The identity is the one the kept copy names, never the installer's own
    file, because the kept copy is the file every read of the cluster goes
    through.
    """
    image = request["image"]
    return {
        "identity": identity(os.path.join(request["workRoot"], KEPT)),
        "kubeconfig": os.path.isfile(os.path.join(request["workRoot"], KEPT)),
        "url": published(image["path"], image["url"]),
    }


def main():
    module = AnsibleModule(
        argument_spec={
            "request": {"type": "dict", "required": True},
            "keep": {"type": "bool", "default": False},
        },
        supports_check_mode=True,
    )
    request, changed = module.params["request"], False
    if module.params["keep"]:
        try:
            changed = keep(request, module.check_mode)
        except OSError as error:
            module.fail_json(msg="the installation's copy of its kubeconfig could not be kept: %s" % error.strerror)
    module.exit_json(changed=changed, observation=observe(request))


if __name__ == "__main__":
    main()
