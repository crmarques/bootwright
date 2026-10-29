"""Mint the one access a KubeVirt provider hosted by a cluster of the context is lent.

The host installation's administrator kubeconfig reaches only this procedure,
which asks the host for one TokenRequest token of the ServiceAccount the
host's kubevirt add-on instance owns in the provider's namespace, and returns a
kubeconfig naming the same server and authority and that token alone. The token
is never evidence, a log entry or an argument: it leaves only as the one output
file the runner reads back and lends to the consuming run.
"""

from __future__ import annotations

import base64
import json
import os

from ansible_collections.bootwright.core.plugins.module_utils.kubevirt_api import Refused, path

VERSION = "substrate-access-kubevirt-v1"
MIN_SECONDS = 600
MAX_SECONDS = 6 * 3600 + 600


def token_request(seconds):
    return {"apiVersion": "authentication.k8s.io/v1", "kind": "TokenRequest", "spec": {"expirationSeconds": seconds}}


def mint(client, request):
    """The lent kubeconfig and the evidence of one lend, or Refused naming why none is lent.

    The granted lifetime is the one the host reports in the answer's spec, so
    no clock comparison decides it; one shorter than requested refuses.
    """
    if request.get("version") != VERSION:
        raise Refused("the KubeVirt host access request is not one this build reads")
    seconds = request.get("expirationSeconds")
    if not isinstance(seconds, int) or isinstance(seconds, bool) or not MIN_SECONDS <= seconds <= MAX_SECONDS:
        raise Refused("the KubeVirt host access request names no admitted token lifetime")
    namespace, account = request["namespace"], request["serviceAccount"]
    status, body = client.request("POST", path("", "v1", "serviceaccounts", namespace, account, "token"), token_request(seconds))
    if status == 404:
        raise Refused("ServiceAccount %s/%s is absent on host cluster %s: its kubevirt add-on instance owns it" % (namespace, account, request["cluster"]))
    if status not in (200, 201) or not isinstance(body, dict):
        raise Refused("host cluster %s refused the token of ServiceAccount %s/%s (HTTP %d)" % (request["cluster"], namespace, account, status))
    token = (body.get("status") or {}).get("token")
    granted = (body.get("spec") or {}).get("expirationSeconds")
    if not isinstance(token, str) or not token or not token.isprintable() or not isinstance(granted, int) or isinstance(granted, bool):
        raise Refused("host cluster %s answered no token for ServiceAccount %s/%s" % (request["cluster"], namespace, account))
    authority = base64.b64encode(client.connection["ca"].encode("ascii")).decode("ascii")
    config = {
        "apiVersion": "v1", "kind": "Config", "current-context": "host",
        "clusters": [{"name": "host", "cluster": {"server": client.server, "certificate-authority-data": authority}}],
        "contexts": [{"name": "host", "context": {"cluster": "host", "user": account, "namespace": namespace}}],
        "users": [{"name": account, "user": {"token": token}}],
    }
    evidence = {"expirationSeconds": granted, "postcondition": granted >= seconds, "server": client.server}
    return json.dumps(config), evidence


def write(output, data):
    """Write the lent kubeconfig to the run's output file, root-only, never over another file."""
    descriptor = os.open(output, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, "w") as handle:
        handle.write(data)
