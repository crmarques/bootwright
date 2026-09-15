"""Bounded Redfish power, boot and virtual-media operations.

A power request is not evidence: every operation polls the resource to the
state it asked for within a bounded window and reports unknown when it does not
arrive. The client speaks to exactly the endpoint the frozen request names and
follows no redirect.
"""

from __future__ import annotations

import base64
import json
import time
import urllib.error
import urllib.request

MAX_BODY = 1 << 20
REQUEST_TIMEOUT = 30
POLL_DELAY = 2
POLL_ATTEMPTS = 60
POWER_STATES = ("On", "Off")


class NoRedirect(urllib.request.HTTPRedirectHandler):
    """A management controller answers directly or not at all."""

    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise urllib.error.HTTPError(req.full_url, code, "redirect refused", headers, fp)


def _opener():
    return urllib.request.build_opener(NoRedirect())


def request(endpoint, user, password, method="GET", path="", payload=None):
    """One bounded call against exactly this controller's system resource."""
    url = endpoint + path
    body = None if payload is None else json.dumps(payload).encode("utf-8")
    call = urllib.request.Request(url, data=body, method=method)
    token = base64.b64encode(("%s:%s" % (user, password)).encode("utf-8")).decode("ascii")
    call.add_header("Authorization", "Basic " + token)
    call.add_header("Accept", "application/json")
    if body is not None:
        call.add_header("Content-Type", "application/json")
    with _opener().open(call, timeout=REQUEST_TIMEOUT) as answer:
        raw = answer.read(MAX_BODY)
        if not raw:
            return {}
        try:
            return json.loads(raw)
        except ValueError:
            return {}


def power_state(endpoint, user, password):
    """Read the reported power state, or the empty string when it is not one."""
    state = request(endpoint, user, password).get("PowerState")
    return state if state in POWER_STATES else ""


def media_inserted(endpoint, user, password):
    """Report the image this controller currently presents, if any."""
    for device in ("Cd", "Cd1", "1"):
        try:
            media = request(endpoint, user, password, path="/VirtualMedia/" + device)
        except (urllib.error.URLError, OSError, ValueError):
            continue
        if media.get("Inserted"):
            return str(media.get("Image") or "")
    return ""


def insert_media(endpoint, user, password, image, device="Cd"):
    request(
        endpoint, user, password, method="POST",
        path="/VirtualMedia/%s/Actions/VirtualMedia.InsertMedia" % device,
        payload={"Image": image, "Inserted": True, "WriteProtected": True},
    )


def eject_media(endpoint, user, password, device="Cd"):
    request(
        endpoint, user, password, method="POST",
        path="/VirtualMedia/%s/Actions/VirtualMedia.EjectMedia" % device,
        payload={},
    )


def boot_once(endpoint, user, password, target="Cd"):
    request(
        endpoint, user, password, method="PATCH",
        payload={"Boot": {"BootSourceOverrideEnabled": "Once", "BootSourceOverrideTarget": target}},
    )


def reset(endpoint, user, password, kind):
    request(
        endpoint, user, password, method="POST",
        path="/Actions/ComputerSystem.Reset", payload={"ResetType": kind},
    )


def await_power(endpoint, user, password, expected, attempts=POLL_ATTEMPTS, sleep=time.sleep):
    """Poll the resource to the state that was asked for, within a bound."""
    for remaining in range(attempts):
        try:
            if power_state(endpoint, user, password) == expected:
                return True
        except (urllib.error.URLError, OSError, ValueError):
            pass
        if remaining + 1 < attempts:
            sleep(POLL_DELAY)
    return False
