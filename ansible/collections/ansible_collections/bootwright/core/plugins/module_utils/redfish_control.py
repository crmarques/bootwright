"""Bounded Redfish power, boot and virtual-media operations.

A power request is not evidence: every operation polls the resource to the
state it asked for within a bounded window and reports unknown when it does not
arrive. The client speaks to exactly the endpoint the frozen request names,
follows no redirect, and reaches it directly: a management controller is
addressed by the frozen request, so an ambient proxy variable must never
redirect the call and must never turn its own refusal into what looks like the
controller's.
"""

from __future__ import annotations

import base64
import json
import ssl
import time
import urllib.error
import urllib.request

MAX_BODY = 1 << 20
REQUEST_TIMEOUT = 30
# Inserting media makes the controller fetch the whole image, which is a
# transfer rather than a property read, so it is bounded far more generously.
MEDIA_TIMEOUT = 300
POLL_DELAY = 2
POLL_ATTEMPTS = 60
POWER_STATES = ("On", "Off")


class NoRedirect(urllib.request.HTTPRedirectHandler):
    """A management controller answers directly or not at all."""

    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise urllib.error.HTTPError(req.full_url, code, "redirect refused", headers, fp)


def _opener(verify=True):
    """An opener that reaches exactly one endpoint with exactly one trust.

    The empty proxy handler is load-bearing rather than tidiness: urllib reads
    the ambient proxy environment by default and its bypass list does not treat
    a CIDR entry as covering a concrete controller address, so a proxy's own
    refusal arrives looking like the controller's. Passing it here removes
    urllib's own environment-reading handler from the chain.
    """
    handlers = [NoRedirect(), urllib.request.ProxyHandler({})]
    if not verify:
        context = ssl.create_default_context()
        context.check_hostname = False
        context.verify_mode = ssl.CERT_NONE
        handlers.append(urllib.request.HTTPSHandler(context=context))
    return urllib.request.build_opener(*handlers)


def request(endpoint, user, password, method="GET", path="", payload=None, verify=True, timeout=REQUEST_TIMEOUT):
    """One bounded call against exactly this controller's system resource."""
    url = endpoint + path
    body = None if payload is None else json.dumps(payload).encode("utf-8")
    call = urllib.request.Request(url, data=body, method=method)
    token = base64.b64encode(("%s:%s" % (user, password)).encode("utf-8")).decode("ascii")
    call.add_header("Authorization", "Basic " + token)
    call.add_header("Accept", "application/json")
    if body is not None:
        call.add_header("Content-Type", "application/json")
    with _opener(verify).open(call, timeout=timeout) as answer:
        raw = answer.read(MAX_BODY)
        if not raw:
            return {}
        try:
            return json.loads(raw)
        except ValueError:
            return {}


def power_state(endpoint, user, password, verify=True):
    """Read the reported power state, or the empty string when it is not one."""
    state = request(endpoint, user, password, verify=verify).get("PowerState")
    return state if state in POWER_STATES else ""


def media_inserted(endpoint, user, password, verify=True):
    """Report the image this controller currently presents, if any."""
    for device in ("Cd", "Cd1", "1"):
        try:
            media = request(endpoint, user, password, path="/VirtualMedia/" + device, verify=verify)
        except (urllib.error.URLError, OSError, ValueError):
            continue
        if media.get("Inserted"):
            return str(media.get("Image") or "")
    return ""


def insert_media(endpoint, user, password, image, device="Cd", verify=True):
    request(
        endpoint, user, password, method="POST",
        path="/VirtualMedia/%s/Actions/VirtualMedia.InsertMedia" % device,
        payload={"Image": image, "Inserted": True, "WriteProtected": True},
        verify=verify, timeout=MEDIA_TIMEOUT,
    )


def eject_media(endpoint, user, password, device="Cd", verify=True):
    request(
        endpoint, user, password, method="POST",
        path="/VirtualMedia/%s/Actions/VirtualMedia.EjectMedia" % device,
        payload={}, verify=verify,
    )


def boot_once(endpoint, user, password, target="Cd", verify=True):
    request(
        endpoint, user, password, method="PATCH",
        payload={"Boot": {"BootSourceOverrideEnabled": "Once", "BootSourceOverrideTarget": target}},
        verify=verify,
    )


def reset(endpoint, user, password, kind, verify=True):
    request(
        endpoint, user, password, method="POST",
        path="/Actions/ComputerSystem.Reset", payload={"ResetType": kind}, verify=verify,
    )


def await_power(endpoint, user, password, expected, attempts=POLL_ATTEMPTS, sleep=time.sleep, verify=True):
    """Poll the resource to the state that was asked for, within a bound."""
    for remaining in range(attempts):
        try:
            if power_state(endpoint, user, password, verify=verify) == expected:
                return True
        except (urllib.error.URLError, OSError, ValueError):
            pass
        if remaining + 1 < attempts:
            sleep(POLL_DELAY)
    return False
