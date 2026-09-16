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

from ansible_collections.bootwright.core.plugins.module_utils import redfish_discovery

MAX_BODY = 1 << 20
REQUEST_TIMEOUT = 30
# Inserting media makes the controller fetch the whole image, which is a
# transfer rather than a property read, so it is bounded far more generously.
MEDIA_TIMEOUT = 300
INSERT_ATTEMPTS = 3
INSERT_RETRY_DELAY = 10
TASK_POLLS = 60
TASK_POLL_DELAY = 2
MEDIA_PROBES = 24
MEDIA_PROBE_DELAY = 5
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


class Client:
    """One controller, driven by what it advertises rather than by its vendor.

    Every mutating call confirms its outcome by reading the resource back: a
    status code says a request was accepted, and only the resource says what is
    true. The discovery this uses lives in `redfish_discovery`, which names no
    vendor and decides from the controller's own metadata.
    """

    def __init__(self, endpoint, user, password, verify=True):
        self.endpoint = endpoint.rstrip("/")
        self.user, self.password, self.verify = user, password, verify
        self.root = redfish_discovery.service_root(self.endpoint)
        self._media = None

    def fetch(self, url="", method="GET", payload=None, headers=None, timeout=REQUEST_TIMEOUT):
        """One bounded call, returning status, body and headers rather than raising.

        A caller decides what a status means, because the same code is fatal in
        one place and expected in another: a controller that answers 404 for an
        ejected device has told us it is ejected.
        """
        target = url or self.endpoint
        if not target.startswith("http"):
            target = redfish_discovery.resolve(self.root, target)
        body = None if payload is None else json.dumps(payload).encode("utf-8")
        call = urllib.request.Request(target, data=body, method=method)
        token = base64.b64encode(("%s:%s" % (self.user, self.password)).encode("utf-8")).decode("ascii")
        call.add_header("Authorization", "Basic " + token)
        call.add_header("Accept", "application/json")
        if body is not None:
            call.add_header("Content-Type", "application/json")
        for name, value in (headers or {}).items():
            call.add_header(name, value)
        try:
            with _opener(self.verify).open(call, timeout=timeout) as answer:
                return answer.status, _decode(answer.read(MAX_BODY)), dict(answer.headers)
        except urllib.error.HTTPError as refused:
            return refused.code, _decode(refused.read(MAX_BODY)), dict(refused.headers or {})
        except (urllib.error.URLError, OSError, ValueError):
            return 0, {}, {}

    def system(self):
        status, resource, _headers = self.fetch()
        return resource if status == 200 else {}

    def identity(self):
        """What the controller says this machine is, for a target proof."""
        system = self.system()
        return {field: str(system.get(field) or "") for field in
                ("UUID", "SerialNumber", "Manufacturer", "Model")}

    def power_state(self):
        state = self.system().get("PowerState")
        return state if state in POWER_STATES else ""

    def hardware_addresses(self):
        """Every address this system reports, with any member that proved none.

        The collection must be readable in full: a partial inventory cannot show
        that this is the machine the declaration names, so an unreadable member
        is a failure rather than an absent address.
        """
        status, collection, _headers = self.fetch(self._interfaces_path())
        if status != 200 or not isinstance(collection.get("Members"), list):
            return [], ["the interface collection could not be read"]
        members = []
        for reference in collection["Members"]:
            url = reference.get("@odata.id") if isinstance(reference, dict) else ""
            member_status, member, _headers = self.fetch(url)
            members.append(member if member_status == 200 else None)
        observed, failures = redfish_discovery.reported_macs(members)
        if not observed and not failures:
            failures.append("the interface collection is empty")
        return observed, failures

    def _interfaces_path(self):
        declared = self.system().get("EthernetInterfaces")
        if isinstance(declared, dict) and isinstance(declared.get("@odata.id"), str):
            return declared["@odata.id"]
        return self.endpoint + "/EthernetInterfaces"

    def media_member(self):
        """The optical virtual-media member, wherever this controller keeps it."""
        if self._media is not None:
            return self._media
        system = self.system()
        candidates = redfish_discovery.media_candidates(
            self._collection(system.get("VirtualMedia"), self.endpoint + "/VirtualMedia"),
            self._manager_media(system))
        for url in candidates:
            status, member, _headers = self.fetch(url)
            if status == 200 and redfish_discovery.optical(member):
                self._media = url
                return url
        self._media = candidates[0] if candidates else ""
        return self._media

    def _manager_media(self, system):
        found = []
        links = system.get("Links") if isinstance(system.get("Links"), dict) else {}
        for manager in links.get("ManagedBy") or []:
            reference = manager.get("@odata.id") if isinstance(manager, dict) else ""
            if not reference:
                continue
            status, resource, _headers = self.fetch(reference)
            if status != 200:
                continue
            found.extend(self._collection(resource.get("VirtualMedia"), reference.rstrip("/") + "/VirtualMedia"))
        return found

    def _collection(self, declared, fallback):
        reference = declared.get("@odata.id") if isinstance(declared, dict) else fallback
        status, collection, _headers = self.fetch(reference)
        if status != 200 or not isinstance(collection.get("Members"), list):
            return []
        return [redfish_discovery.resolve(self.root, m.get("@odata.id"))
                for m in collection["Members"] if isinstance(m, dict)]

    def inserted(self):
        member = self.media_member()
        if not member:
            return ""
        status, resource, _headers = self.fetch(member)
        return redfish_discovery.inserted_image(resource) if status == 200 else ""

    def insert(self, image, attempts=INSERT_ATTEMPTS, sleep=time.sleep):
        """Attach an image and confirm the controller presents it.

        The response is never the evidence. An accepted request may still be
        reported as a failure by an asynchronous task, and a completed task may
        still leave nothing attached, so the member itself is read back.
        """
        member = self.media_member()
        if not member:
            return False, "the controller exposes no virtual-media device"
        if redfish_discovery.image_matches(self.inserted(), image):
            return True, ""
        reason = ""
        for attempt in range(attempts):
            if attempt:
                self.eject()
                sleep(INSERT_RETRY_DELAY)
            reason = self._insert_once(member, image, sleep)
            if not reason:
                return True, ""
        return False, reason

    def _insert_once(self, member, image, sleep):
        status, resource, _headers = self.fetch(member)
        target, style = self._attach_action(resource if status == 200 else {}, member)
        payload = {"Image": image, "Inserted": True, "WriteProtected": True}
        if style == "vmm-control":
            payload = {"Image": image, "VmmControlType": "Connect"}
        protocol = redfish_discovery.transfer_protocol(image)
        if protocol and style != "vmm-control":
            payload["TransferProtocolType"] = protocol
        status, body, headers = self.fetch(target, method="POST", payload=payload, timeout=MEDIA_TIMEOUT)
        if status not in (200, 202, 204):
            return "the controller refused the attach with HTTP %d" % status
        if status == 202 and not self._await_task(redfish_discovery.task_reference(body, headers), sleep):
            return "the controller reported the attach as failed"
        for _probe in range(MEDIA_PROBES):
            if redfish_discovery.image_matches(self.inserted(), image):
                return ""
            sleep(MEDIA_PROBE_DELAY)
        return "the controller accepted the attach but presents no image"

    def _attach_action(self, member, url):
        """The attach this controller advertises, preferring the standard one.

        A vendor extension is used only where no standard action exists and the
        extension's own metadata says it takes what this client sends.
        """
        advertised = redfish_discovery.actions(member, "#VirtualMedia.InsertMedia")
        standard = [entry for entry in advertised if entry["source"] == "standard"]
        if standard:
            return standard[0]["target"], "standard"
        for entry in redfish_discovery.actions(member, "#VirtualMedia.VmmControl"):
            if not entry["actionInfo"]:
                continue
            status, info, _headers = self.fetch(entry["actionInfo"])
            if status == 200 and redfish_discovery.accepts_connect_disconnect(info):
                return entry["target"], "vmm-control"
        if advertised:
            return advertised[0]["target"], "standard"
        return url.rstrip("/") + "/Actions/VirtualMedia.InsertMedia", "standard"

    def _await_task(self, reference, sleep):
        if not reference:
            return True
        for _poll in range(TASK_POLLS):
            status, task, _headers = self.fetch(reference)
            if status not in (200, 202):
                return False
            settled, succeeded = redfish_discovery.task_settled(task)
            if settled:
                return succeeded
            sleep(TASK_POLL_DELAY)
        return False

    def eject(self, sleep=time.sleep):
        """Detach whatever is attached and confirm the device reports nothing.

        A controller that removes the device entirely has also ejected it, so a
        member that stops answering is success rather than a failure.
        """
        member = self.media_member()
        if not member:
            return True
        status, resource, _headers = self.fetch(member)
        if status == 200 and not redfish_discovery.inserted_image(resource):
            return True
        for entry in redfish_discovery.actions(resource if status == 200 else {}, "#VirtualMedia.EjectMedia"):
            self.fetch(entry["target"], method="POST", payload={})
            break
        else:
            self.fetch(member.rstrip("/") + "/Actions/VirtualMedia.EjectMedia", method="POST", payload={})
        for _probe in range(MEDIA_PROBES):
            status, resource, _headers = self.fetch(member)
            if status == 404 or (status == 200 and not redfish_discovery.inserted_image(resource)):
                return True
            sleep(MEDIA_PROBE_DELAY)
        return False

    def boot_once(self, target="Cd"):
        """Select the next boot device, carrying the precondition some require."""
        payload = {"Boot": {"BootSourceOverrideEnabled": "Once", "BootSourceOverrideTarget": target}}
        status, resource, _headers = self.fetch()
        headers = {}
        if isinstance(resource.get("@odata.etag"), str) and resource["@odata.etag"]:
            headers["If-Match"] = resource["@odata.etag"]
        status, _body, _headers = self.fetch(method="PATCH", payload=payload, headers=headers)
        if status == 412:
            status, _body, _headers = self.fetch(method="PATCH", payload=payload, headers={"If-Match": "*"})
        return status in (200, 202, 204)

    def power(self, kind, expected, attempts=POLL_ATTEMPTS, sleep=time.sleep):
        if self.power_state() == expected:
            return False, True
        if kind == "On":
            kind = redfish_discovery.power_on_reset_type(self.system())
        self.fetch(self.endpoint + "/Actions/ComputerSystem.Reset", method="POST", payload={"ResetType": kind})
        for remaining in range(attempts):
            if self.power_state() == expected:
                return True, True
            if remaining + 1 < attempts:
                sleep(POLL_DELAY)
        return True, False


def _decode(raw):
    if not raw:
        return {}
    try:
        return json.loads(raw)
    except ValueError:
        return {}
