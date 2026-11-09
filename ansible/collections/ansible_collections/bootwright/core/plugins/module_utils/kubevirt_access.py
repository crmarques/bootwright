"""Mint the one access a KubeVirt provider hosted by a cluster of the context is lent, and revoke it.

The host installation's administrator kubeconfig reaches only this procedure.
A lend creates one per-lend Secret in the provider's namespace, carrying the
lend mark, asks the host for one TokenRequest token of the ServiceAccount the
host's kubevirt add-on instance owns there, bound to that Secret, and returns a
kubeconfig naming the same server and authority and that token alone. A
revoke deletes the Secret, so the API server stops accepting the token
within seconds, before it expires. The token is never evidence, a log entry
or an argument: it leaves only as the one output file the runner reads back
and lends to the consuming run.
"""

from __future__ import annotations

import base64
import datetime
import json
import os
import re
import time
from urllib.parse import quote

from ansible_collections.bootwright.core.plugins.module_utils.kubevirt_api import Answered, Refused, Unanswered, path

VERSION = "substrate-access-kubevirt-v1"
MIN_SECONDS = 600
# The label a per-lend Secret carries: its value is the lifetime, in seconds,
# of the token bound to it, so a later lend can tell an expired leftover from
# a Secret another run's live token is bound to.
MARK = "bootwright.io/lend"
NAME = re.compile(r"^[a-z0-9]([-a-z0-9]{0,251}[a-z0-9])?$")
SWEEP_LIMIT = 64
# A sweep stops issuing deletes once this many seconds have passed since it
# began, or at the first delete the host does not answer, so leftovers never
# spend the lend's own bound.
SWEEP_SECONDS = 30


def token_request(seconds, secret, secret_uid):
    return {"apiVersion": "authentication.k8s.io/v1", "kind": "TokenRequest", "spec": {
        "expirationSeconds": seconds,
        "boundObjectRef": {"apiVersion": "v1", "kind": "Secret", "name": secret, "uid": secret_uid}}}


def lend_secret(request, seconds):
    return {"apiVersion": "v1", "kind": "Secret", "type": "Opaque", "metadata": {
        "name": request["secret"], "namespace": request["namespace"], "labels": {MARK: str(seconds)}}}


def validated(request, lend=True):
    if request.get("version") != VERSION:
        raise Refused("the KubeVirt host access request is not one this build reads")
    seconds = request.get("expirationSeconds")
    if lend and (not isinstance(seconds, int) or isinstance(seconds, bool) or seconds < MIN_SECONDS):
        raise Refused("the KubeVirt host access request names no admitted token lifetime")
    secret = request.get("secret")
    if not isinstance(secret, str) or not NAME.match(secret):
        raise Refused("the KubeVirt host access request names no per-lend Secret")
    if not isinstance(request.get("sweep"), bool):
        raise Refused("the KubeVirt host access request does not say whether its lend sweeps")
    return seconds, secret


def mint(client, request):
    """The lent kubeconfig and the evidence of one lend, or Refused naming why none is lent.

    The per-lend Secret is created first, never adopted: a Secret of that
    name already present refuses. The granted lifetime is the one the host
    reports in the answer's spec, so no clock comparison decides it; one
    shorter than requested refuses. Only a request that says so sweeps
    leftovers: a lend of a read-only or unrecorded execution deletes nothing
    but its own Secret. A lend that fails after creating its Secret deletes it
    before it refuses.
    """
    seconds, secret = validated(request)
    namespace = request["namespace"]
    status, body = client.request("POST", path("", "v1", "secrets", namespace), lend_secret(request, seconds))
    if status == 409:
        raise Refused("Secret %s/%s already exists on host cluster %s, so this lend cannot bind its token to it" % (namespace, secret, request["cluster"]))
    if status == 404:
        raise Refused("namespace %s is absent on host cluster %s: its kubevirt add-on instance owns it" % (namespace, request["cluster"]))
    created = body.get("metadata") if status == 201 and isinstance(body, dict) else None
    if not isinstance(created, dict) or not isinstance(created.get("uid"), str) or not created["uid"]:
        raise Refused("host cluster %s refused the per-lend Secret %s/%s (HTTP %d)" % (request["cluster"], namespace, secret, status))
    try:
        if request["sweep"]:
            sweep(client, namespace, secret, created.get("creationTimestamp"))
        return bound(client, request, seconds, secret, created["uid"])
    except BaseException:
        try:
            remove(client, namespace, secret, created["uid"])
        except (Answered, Unanswered, Refused):
            pass
        raise


def bound(client, request, seconds, secret, secret_uid):
    namespace, account = request["namespace"], request["serviceAccount"]
    status, body = client.request("POST", path("", "v1", "serviceaccounts", namespace, account, "token"), token_request(seconds, secret, secret_uid))
    if status == 404:
        raise Refused("ServiceAccount %s/%s is absent on host cluster %s: its kubevirt add-on instance owns it" % (namespace, account, request["cluster"]))
    if status not in (200, 201) or not isinstance(body, dict):
        raise Refused("host cluster %s refused the token of ServiceAccount %s/%s (HTTP %d)" % (request["cluster"], namespace, account, status))
    token = (body.get("status") or {}).get("token")
    granted = (body.get("spec") or {}).get("expirationSeconds")
    reference = (body.get("spec") or {}).get("boundObjectRef") or {}
    if not isinstance(token, str) or not token or not token.isprintable() or not isinstance(granted, int) or isinstance(granted, bool):
        raise Refused("host cluster %s answered no token for ServiceAccount %s/%s" % (request["cluster"], namespace, account))
    if reference.get("kind") != "Secret" or reference.get("name") != secret or reference.get("uid") != secret_uid:
        raise Refused("host cluster %s answered a token of ServiceAccount %s/%s not bound to its per-lend Secret" % (request["cluster"], namespace, account))
    authority = base64.b64encode(client.connection["ca"].encode("ascii")).decode("ascii")
    config = {
        "apiVersion": "v1", "kind": "Config", "current-context": "host",
        "clusters": [{"name": "host", "cluster": {"server": client.server, "certificate-authority-data": authority}}],
        "contexts": [{"name": "host", "context": {"cluster": "host", "user": account, "namespace": namespace}}],
        "users": [{"name": account, "user": {"token": token}}],
    }
    evidence = {"expirationSeconds": granted, "postcondition": granted >= seconds, "secret": secret, "server": client.server}
    return json.dumps(config), evidence


def instant(value):
    try:
        return datetime.datetime.strptime(value, "%Y-%m-%dT%H:%M:%SZ")
    except (TypeError, ValueError):
        return None


def sweep(client, namespace, own, now, clock=time.monotonic):
    """Delete the marked Secrets in namespace whose bound token has expired; the count deleted.

    now is the creationTimestamp the host gave this lend's own Secret, so the
    host's clock alone decides expiry. A Secret whose mark names no lifetime,
    or one still inside it, is kept: another run's live token may be bound to
    it. Sweeping is best effort and bounded: it reads one page of SWEEP_LIMIT,
    stops at the first delete the host does not answer and issues none once
    SWEEP_SECONDS have passed, leaving the rest for a later lend.
    """
    started = clock()
    current = instant(now)
    if current is None:
        return 0
    try:
        listed = client.get(path("", "v1", "secrets", namespace), query="labelSelector=" + quote(MARK, safe="") + "&limit=%d" % SWEEP_LIMIT)
    except (Answered, Unanswered):
        return 0
    swept = 0
    for item in ((listed or {}).get("items") or [])[:SWEEP_LIMIT]:
        metadata = item.get("metadata") if isinstance(item, dict) else None
        if not isinstance(metadata, dict) or metadata.get("name") == own:
            continue
        lifetime = (metadata.get("labels") or {}).get(MARK)
        created = instant(metadata.get("creationTimestamp"))
        if not isinstance(lifetime, str) or not lifetime.isdigit() or created is None or not isinstance(metadata.get("uid"), str):
            continue
        if created + datetime.timedelta(seconds=int(lifetime)) >= current:
            continue
        if clock() - started >= SWEEP_SECONDS:
            break
        try:
            swept += 1 if remove(client, namespace, metadata["name"], metadata["uid"]) else 0
        except Answered:
            continue
        except Unanswered:
            break
    return swept


def remove(client, namespace, secret, secret_uid):
    """Delete exactly the Secret secret_uid names; False when it is already gone.

    The UID precondition makes the delete safe to send again: a retry after a
    lost answer finds the Secret gone (404) or replaced by another (409).
    """
    options = {"apiVersion": "v1", "kind": "DeleteOptions", "preconditions": {"uid": secret_uid}}
    status, body = client.request("DELETE", path("", "v1", "secrets", namespace, secret), options, idempotent=True)
    if status in (200, 202):
        return True
    if status in (404, 409):
        return False
    raise Answered(status, body)


def revoke(client, request):
    """The evidence of one revoke: the per-lend Secret is absent, so the token bound to it no longer authenticates.

    Only a Secret carrying the lend mark is deleted, and only the object read,
    by its UID; one already gone proves the revoke as well.
    """
    ignored_seconds, secret = validated(request, lend=False)
    namespace = request["namespace"]
    live = client.get(path("", "v1", "secrets", namespace, secret))
    if live is not None:
        metadata = live.get("metadata") or {}
        if MARK not in (metadata.get("labels") or {}) or not isinstance(metadata.get("uid"), str):
            raise Refused("Secret %s/%s on host cluster %s carries no lend mark, so this revoke leaves it" % (namespace, secret, request["cluster"]))
        remove(client, namespace, secret, metadata["uid"])
    return {"expirationSeconds": 0, "postcondition": True, "secret": secret, "server": client.server}


def write(output, data):
    """Write the lent kubeconfig to the run's output file, root-only, never over another file."""
    descriptor = os.open(output, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, "w") as handle:
        handle.write(data)
