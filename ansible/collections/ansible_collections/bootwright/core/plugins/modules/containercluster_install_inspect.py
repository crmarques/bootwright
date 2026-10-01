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
  - The installer's own waits load its file, not the kept copy, so O(restore)
    puts the kept copy back in its place, in one rename, when that file is
    within the bound and names no identity, as a write the installer was
    killed in leaves it, and the kept copy names this installation's identity.
  - Without O(keep) or O(restore) it performs no change and is safe to repeat.
  - O(registered) reads which declared nodes the assisted service on the
    rendezvous host has registered, in one request over plain HTTP bounded in
    time and size, with the watcher token the installer keeps in its asset
    state. The token never leaves the module.
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
  restore:
    description:
      - Replace the installer's own kubeconfig with the kept copy, in one
        rename, when that file is a regular file within the bound that names
        no identity, and the kept copy names O(identity).
      - A kept copy that names no identity or another one is never written
        there; the result then reports the restore refused.
      - Exclusive with O(keep).
    type: bool
    default: false
  identity:
    description:
      - The identity the apply took from the kept copy before its first
        effect, which the kept copy must still name for O(restore).
    type: str
    default: ""
  registered:
    description:
      - Read the hosts registered with the assisted service on the rendezvous
        host and report each declared node no registered host carries the
        name of.
      - The address is the rendezvous address the installer's asset state
        names, read only when it is one of the addresses the request declares
        a node at.
      - Exclusive with O(keep) and O(restore).
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

- name: Restore the installer's kubeconfig from the kept copy when it is cut short
  bootwright.core.containercluster_install_inspect:
    request: '{{ bootwright_cluster_install_request }}'
    restore: true
    identity: '{{ containercluster_install_agent_before.observation.identity }}'

- name: Read which declared nodes never registered after a stall
  bootwright.core.containercluster_install_inspect:
    request: '{{ bootwright_cluster_install_request }}'
    registered: true
"""

RETURN = r"""
observation:
  description: This build's identity, whether the kept copy exists, and the published image address.
  returned: always
  type: dict
restore:
  description:
    - With O(restore), C(restored) when the kept copy replaced the installer's
      file, C(refused) when that file needed it and the kept copy does not name
      O(identity), and C(untouched) when the file was left as it is.
  returned: when O(restore) is true
  type: str
registration:
  description:
    - With O(registered), C(read) is true when the assisted service answered
      with this cluster's hosts and named each of them, and C(unregistered)
      lists the declared nodes, in request order, that no registered host
      carries the name of. A read that failed lists none.
  returned: when O(registered) is true
  type: dict
"""

import base64
import binascii
import contextlib
import hashlib
import http.client
import ipaddress
import json
import os
import re
import signal
import stat
import tempfile
import time

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
# What a restore did to the installer's own kubeconfig before a wait.
RESTORED = "restored"
REFUSED = "refused"
UNTOUCHED = "untouched"
# The installer's asset state in its asset directory: one JSON object keyed by
# asset type. It holds the watcher token as *gencrypto.AuthConfig's
# WatcherAuthToken and the rendezvous address as *agentconfig.AgentConfig's
# Config.rendezvousIP, as openshift-install 4.21.10 writes them, and the
# installer's own pkg/agent finds both there (FindAuthTokenFromAssetStore and
# FindRendezvouIPAndSSHKeyFromAssetStore). A larger file is never read.
STATE = ".openshift_install_state.json"
AUTH_CONFIG = "*gencrypto.AuthConfig"
AGENT_CONFIG = "*agentconfig.AgentConfig"
MAX_STATE = 16 * 1024 * 1024
# The assisted service on the rendezvous host answers plain HTTP on this port,
# under this base path: SERVICE_BASE_URL in the rendezvous-host.env the
# installer writes into the image, which the image's own common.sh extends
# with api/assisted-install/v2. The watcher token is the credential the
# installer's own client of that service sends (NewNodeZeroRestClient with
# gencrypto's WatcherAuthHeaderWriter), in the header the service reads it
# from. One request lists the cluster with its hosts; it ends within
# READ_SECONDS of its start, connecting included, and is dropped once its
# body passes MAX_CLUSTERS.
SERVICE_PORT = 8090
CLUSTERS = "/api/assisted-install/v2/clusters?with_hosts=true"
WATCHER = "Watcher-Authorization"
MAX_CLUSTERS = 16 * 1024 * 1024
READ_SECONDS = 10


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


def bounded(path, limit=MAX_KUBECONFIG):
    """The bytes of one regular file within the read bound, or nothing."""
    try:
        descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    except OSError:
        return None
    with os.fdopen(descriptor, "rb") as handle:
        try:
            if not stat.S_ISREG(os.fstat(handle.fileno()).st_mode):
                return None
            data = handle.read(limit + 1)
        except OSError:
            return None
    return data if len(data) <= limit else None


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


def restore(request, anchor, check_mode=False):
    """What the installer's own kubeconfig needed before a wait: RESTORED, REFUSED or UNTOUCHED.

    `agent wait-for` loads auth/kubeconfig from its asset directory itself and
    exits when that fails (cmd/openshift-install/agent/waitfor.go builds the
    path with filepath.Join(assetDir, "auth", "kubeconfig"), and
    pkg/agent/cluster.go NewCluster ends in logrus.Fatal when
    NewClusterKubeAPIClient cannot load it, release-4.21), so a file a kill cut
    short stops every later wait. Only a regular file within the bound that
    names no identity is replaced, and only by a kept copy that names the
    identity the apply took from it before its first effect. A file naming an
    identity, past the bound, missing or not a regular file is left as it is.
    """
    work = request["workRoot"]
    target = os.path.join(work, KUBECONFIG)
    current = bounded(target)
    if current is None or named(current):
        return UNTOUCHED
    kept = bounded(os.path.join(work, KEPT))
    if not anchor or kept is None or named(kept) != anchor:
        return REFUSED
    if not check_mode:
        publish(os.path.dirname(target), target, kept)
    return RESTORED


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


def installer_state(work):
    """The rendezvous address and watcher token the installer's asset state holds, or nothing."""
    data = bounded(os.path.join(work, STATE), MAX_STATE)
    try:
        state = None if data is None else json.loads(data)
    except (ValueError, RecursionError):
        return None
    if not isinstance(state, dict):
        return None
    auth, agent = state.get(AUTH_CONFIG), state.get(AGENT_CONFIG)
    config = agent.get("Config") if isinstance(agent, dict) else None
    token = auth.get("WatcherAuthToken") if isinstance(auth, dict) else None
    address = config.get("rendezvousIP") if isinstance(config, dict) else None
    if not isinstance(token, str) or not token or not isinstance(address, str):
        return None
    return address, token


def admitted(address, request):
    """The rendezvous address when the request declares a node at it, or nothing.

    The address comes from a file, so it is reached only when it is one of the
    addresses the frozen request names: the token is never sent anywhere else.
    """
    try:
        rendezvous = ipaddress.ip_address(address)
    except ValueError:
        return ""
    for node in request.get("nodes") or []:
        try:
            declared = ipaddress.ip_address(str(node.get("address", ""))) if isinstance(node, dict) else None
        except ValueError:
            continue
        if declared == rendezvous:
            return str(rendezvous)
    return ""


class Expired(Exception):
    """The read of the registered hosts ran past its deadline."""


@contextlib.contextmanager
def deadline(seconds):
    """Bound connecting, the headers and the body together, however slowly they arrive.

    A socket timeout bounds each wait alone, so a body arriving a byte at a
    time would outlast it. This is the alarm controller_files.alarm sets: an
    earlier one is kept, the sooner of the two fires, and the earlier one is
    re-armed with what remains of it.
    """
    started = time.monotonic()
    prior_handler = signal.getsignal(signal.SIGALRM)
    prior_timer = signal.getitimer(signal.ITIMER_REAL)

    def expired(_number, _frame):
        raise Expired()

    try:
        signal.signal(signal.SIGALRM, expired)
        signal.setitimer(signal.ITIMER_REAL, min(seconds, prior_timer[0]) if prior_timer[0] else seconds)
        yield
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)
        signal.signal(signal.SIGALRM, prior_handler)
        if prior_timer[0]:
            signal.setitimer(signal.ITIMER_REAL, max(0.001, prior_timer[0] - (time.monotonic() - started)),
                             prior_timer[1])


def answered(connection, token, limit):
    """The body the service answered the one request with, or nothing unless it is a 200 within the limit."""
    connection.request("GET", CLUSTERS, headers={WATCHER: token, "Accept": "application/json"})
    answer = connection.getresponse()
    if answer.status != 200:
        return None
    body = answer.read(limit + 1)
    return body if len(body) <= limit else None


def fetch(address, token, port, seconds=READ_SECONDS, limit=MAX_CLUSTERS):
    """The body of one request for the service's clusters with their hosts, or nothing.

    http.client follows no redirect and reads no proxy setting, so the request
    reaches the admitted address alone. Any status but 200, a body past the
    limit, or a read still unfinished at the deadline is nothing.
    """
    connection = http.client.HTTPConnection(address, port, timeout=seconds)
    try:
        with deadline(seconds):
            return answered(connection, token, limit)
    except (Expired, OSError, http.client.HTTPException, ValueError):
        return None
    finally:
        connection.close()


def host_names(host):
    """The names a registered host carries: the one requested for it and the one its inventory reports."""
    if not isinstance(host, dict):
        return set()
    names = set()
    requested = host.get("requested_hostname")
    if isinstance(requested, str) and requested:
        names.add(requested)
    try:
        inventory = json.loads(host.get("inventory") or "null")
    except (TypeError, ValueError, RecursionError):
        inventory = None
    reported = inventory.get("hostname") if isinstance(inventory, dict) else None
    if isinstance(reported, str) and reported:
        names.add(reported)
    return names


def registered_names(body):
    """Every name the one cluster's registered hosts carry, or nothing when the answer cannot prove it.

    The rendezvous host registers itself, so an answer with no host, with any
    cluster but exactly one, or with a host that carries no name proves
    nothing about which declared node is missing.
    """
    try:
        clusters = json.loads(body)
    except (ValueError, RecursionError):
        return None
    if not isinstance(clusters, list) or len(clusters) != 1 or not isinstance(clusters[0], dict):
        return None
    hosts = clusters[0].get("hosts")
    if not isinstance(hosts, list) or not hosts:
        return None
    found = set()
    for host in hosts:
        names = host_names(host)
        if not names:
            return None
        found |= names
    return found


def registration(request):
    """Which declared nodes no host registered with the assisted service carries the name of.

    The installer's two give-ups that mark a stall name no host, so this is
    read once after one. A read that fails reports read false and no node.
    """
    held = installer_state(request["workRoot"])
    address = "" if held is None else admitted(held[0], request)
    body = fetch(address, held[1], SERVICE_PORT) if address else None
    found = None if body is None else registered_names(body)
    if found is None:
        return {"read": False, "unregistered": []}
    declared = [node.get("name") for node in request.get("nodes") or [] if isinstance(node, dict)]
    return {"read": True, "unregistered": [name for name in declared if isinstance(name, str) and name not in found]}


def main():
    module = AnsibleModule(
        argument_spec={
            "request": {"type": "dict", "required": True},
            "keep": {"type": "bool", "default": False},
            "restore": {"type": "bool", "default": False},
            "identity": {"type": "str", "default": ""},
            "registered": {"type": "bool", "default": False},
        },
        supports_check_mode=True,
    )
    request, changed, result = module.params["request"], False, {}
    if module.params["keep"] and module.params["restore"]:
        module.fail_json(msg="keep and restore are exclusive")
    if module.params["registered"] and (module.params["keep"] or module.params["restore"]):
        module.fail_json(msg="registered only reads, so it is exclusive with keep and restore")
    if module.params["registered"]:
        result["registration"] = registration(request)
    if module.params["keep"]:
        try:
            changed = keep(request, module.check_mode)
        except OSError as error:
            module.fail_json(msg="the installation's copy of its kubeconfig could not be kept: %s" % error.strerror)
    if module.params["restore"]:
        try:
            result["restore"] = restore(request, module.params["identity"], module.check_mode)
        except OSError as error:
            module.fail_json(msg="the installer's kubeconfig could not be restored: %s" % error.strerror)
        changed = result["restore"] == RESTORED
    module.exit_json(changed=changed, observation=observe(request), **result)


if __name__ == "__main__":
    main()
