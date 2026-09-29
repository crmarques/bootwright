"""Bounded Redfish power, boot and virtual-media operations.

A power request is not evidence: every operation polls the resource to the
state it asked for within a bounded window and reports unknown when it does not
arrive. The client speaks to exactly the endpoint the frozen request names,
follows no redirect, and reaches it directly: a management controller is
addressed by the frozen request, so an ambient proxy variable must never
redirect the call and must never turn its own refusal into what looks like the
controller's.

Every reference the controller returns is held to that one endpoint before a
request or its credential is built. A resource that cannot be read is a
failure rather than an empty answer, and the one failure, ControllerError, is a
line naming what was being done, where and the status, never a body or the
credential.
"""

from __future__ import annotations

import base64
import http.client
import json
import ssl
import time
import urllib.error
import urllib.request
from urllib.parse import urlsplit

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
# A boot selection is read back like any other effect. The bound is borrowed
# from the reference's power-state poll; no firmware has been observed taking
# this long to report a selection.
BOOT_POLLS = 12
BOOT_POLL_DELAY = 5
POWER_STATES = ("On", "Off")
LINE_LIMIT = 200
PATH_LIMIT = 128
HOST_LIMIT = 64
# 401 and 403 mean the account lacks the privilege, which no retry and no
# other request changes, so they end an operation at once.
PRIVILEGE = (401, 403)


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


class ControllerError(Exception):
    """The one way this client fails: a line an operator can act on.

    It names what was being done, the resource path, the HTTP status (0 when
    nothing answered) and the controller's own message identifier. It is built
    only from those and cut to one printable line, so it never carries user
    information, the credential or a response body.
    """

    def __init__(self, line):
        super().__init__(_printable(line, LINE_LIMIT, " "))


class Client:
    """One controller, driven by what it advertises rather than by its vendor.

    Every mutating call confirms its outcome by reading the resource back: a
    status code says a request was accepted, and only the resource says what is
    true. The discovery this uses lives in `redfish_discovery`, which names no
    vendor and decides from the controller's own metadata.

    The system is read once and kept until the client writes, and the media
    device is discovered once, so an operation makes one system read before its
    first write. `last_power` and `last_image` are what the controller last
    reported, which is what an operation reports back.
    """

    def __init__(self, endpoint, user, password, verify=True):
        self.endpoint = endpoint.rstrip("/")
        self.user, self.password, self.verify = user, password, verify
        self.home = _authority(self.endpoint)
        self.root = redfish_discovery.service_root(self.endpoint) if self.home else ""
        self.sleep = time.sleep
        self.last_power, self.last_image = "", ""
        self._system = self._media = self._member = None

    def fetch(self, reference="", method="GET", payload=None, headers=None, timeout=REQUEST_TIMEOUT, doing=""):
        """One bounded call, returning status, body and headers.

        This is the only place a request is built, and a reference is requested
        only when it names this controller's endpoint, so the credential never
        leaves it. The status is 0 when nothing answered, the body is one JSON
        object or None, and header names are lower-cased. A caller decides what
        a status means, because the same code is fatal in one place and
        expected in another: a controller that answers 404 for an ejected
        device has told us it is ejected. A write drops what was kept, because
        the controller has changed.
        """
        url = self._own(reference or self.endpoint, doing or ("reading" if method == "GET" else "writing"))
        if method != "GET":
            self._system = self._member = None
        try:
            with _opener(self.verify).open(self._request(url, method, payload, headers), timeout=timeout) as answer:
                return answer.status, _body(answer.read(MAX_BODY + 1), method), _lowered(answer.headers)
        except urllib.error.HTTPError as refused:
            return refused.code, _body(_drain(refused), method), _lowered(refused.headers)
        except (urllib.error.URLError, OSError, ValueError, http.client.HTTPException):
            return 0, None, {}

    def _request(self, url, method, payload, headers):
        data = None if payload is None else json.dumps(payload).encode("utf-8")
        call = urllib.request.Request(url, data=data, method=method)
        token = base64.b64encode(("%s:%s" % (self.user, self.password)).encode("utf-8")).decode("ascii")
        call.add_header("Authorization", "Basic " + token)
        call.add_header("Accept", "application/json")
        if data is not None:
            call.add_header("Content-Type", "application/json")
        for name, value in (headers or {}).items():
            call.add_header(name, value)
        return call

    def _own(self, reference, doing):
        """The URL to request for a reference, which must name this controller.

        A path reference resolves against the service root. One that names an
        authority, absolute or network-path, is requested only when its scheme,
        host and port are the endpoint's and it carries no user information.
        Anything else fails here, before a request or the credential exists, and
        nothing is rebased onto the endpoint.
        """
        reference = reference.strip() if isinstance(reference, str) else ""
        if not reference:
            raise ControllerError("%s: the controller named no resource" % doing)
        if not self.home:
            raise _refused(doing, self.endpoint, "")
        if not _names_authority(reference):
            return redfish_discovery.resolve(self.root, reference)
        if _authority(reference, self.home[0]) != self.home:
            raise _refused(doing, reference, self.home[0])
        return self.home[0] + ":" + reference if reference.startswith("//") else reference

    def _read(self, reference="", doing="reading"):
        """A resource that must be readable: 200 with one object, or a failure."""
        status, body, headers = self.fetch(reference, doing=doing)
        if status != 200 or not isinstance(body, dict):
            raise _failure(doing, reference or self.endpoint, status, body)
        if not reference:
            self._keep(body, headers)
        return body, headers

    def _keep(self, system, headers):
        self._system = (system, headers)
        self.last_power = _power(system)

    def system(self):
        """The system resource, read once and kept until this client writes."""
        if self._system is None:
            try:
                self._read()
            except ControllerError as failure:
                self._system = failure
        if isinstance(self._system, ControllerError):
            raise ControllerError(str(self._system))
        return self._system[0]

    def identity(self):
        """What the controller says this machine is, for a target proof."""
        system = self.system()
        return {field: str(system.get(field) or "") for field in
                ("UUID", "SerialNumber", "Manufacturer", "Model")}

    def power_state(self):
        """The power state of the kept system read, or the empty string."""
        return _power(self.system())

    def hardware_addresses(self):
        """Every address this system reports, with any member that proved none.

        The collection must be readable in full: a partial inventory cannot show
        that this is the machine the declaration names, so an unreadable or
        refused member is a failure rather than an absent address.
        """
        try:
            status, collection, _headers = self.fetch(self._interfaces_path())
        except ControllerError:
            status, collection = 0, None
        listed = collection.get("Members") if isinstance(collection, dict) else None
        if status != 200 or not isinstance(listed, list):
            return [], ["the interface collection could not be read"]
        observed, failures = redfish_discovery.reported_macs([self._interface(link) for link in listed])
        if not observed and not failures:
            failures.append("the interface collection is empty")
        return observed, failures

    def _interfaces_path(self):
        try:
            declared = self.system().get("EthernetInterfaces")
        except ControllerError:
            declared = None
        if isinstance(declared, dict) and isinstance(declared.get("@odata.id"), str) and declared["@odata.id"]:
            return declared["@odata.id"]
        return self.endpoint + "/EthernetInterfaces"

    def _interface(self, link):
        """One interface member, or None when it cannot be read or is refused."""
        reference = link.get("@odata.id") if isinstance(link, dict) else None
        if not isinstance(reference, str) or not reference:
            return None
        try:
            status, member, _headers = self.fetch(reference)
        except ControllerError:
            return None
        return member if status == 200 and isinstance(member, dict) else None

    def media_member(self):
        """The optical virtual-media member, or "" when the controller offers none.

        It is discovered once per client, from the system and every manager the
        system names, and fails closed: a view that cannot be read raises
        rather than reading as no media, because an ejection proved against a
        device that was never found proves nothing.
        """
        if self._media is None:
            try:
                self._media = self._discover()
            except ControllerError as failure:
                self._media = failure
        if isinstance(self._media, ControllerError):
            raise ControllerError(str(self._media))
        return self._media

    def _discover(self):
        system = self.system()
        fetched = set()
        listed = self._view(system, self.endpoint, fetched)
        managed = []
        links = system.get("Links")
        for manager in self._references(links.get("ManagedBy") if isinstance(links, dict) else None, self.endpoint):
            managed.extend(self._view(self._read(manager)[0], manager, fetched))
        for url in redfish_discovery.media_candidates(listed, managed):
            member = self._read(url)[0]
            if redfish_discovery.optical(member):
                self._member = member
                return url
        return ""

    def _view(self, resource, owner, fetched):
        """The members one view lists: the system's or a manager's VirtualMedia.

        A declared collection must be readable. An undeclared one is probed at
        its conventional path, and only an answer saying it does not exist
        there, 404, 501 or another 4xx but 401 and 403, reads as none offered.
        """
        declared = resource.get("VirtualMedia")
        if declared is None:
            reference = owner.rstrip("/") + "/VirtualMedia"
        else:
            reference = declared.get("@odata.id") if isinstance(declared, dict) else None
        url = self._own(reference, "reading")
        if url in fetched:
            return []
        fetched.add(url)
        status, collection, _headers = self.fetch(url)
        if declared is None and (status == 501 or (400 <= status < 500 and status not in PRIVILEGE)):
            return []
        if status != 200 or not isinstance(collection, dict):
            raise _failure("reading", url, status, collection)
        if not isinstance(collection.get("Members"), list):
            raise _failure("reading", url, status, collection, "the collection lists no members")
        return self._references(collection["Members"], url)

    def _references(self, links, where):
        """The distinct URLs a list of links names; a malformed link is unreadable."""
        if links is None:
            return []
        found = []
        for link in links if isinstance(links, list) else [None]:
            reference = link.get("@odata.id") if isinstance(link, dict) else None
            if not isinstance(reference, str) or not reference:
                raise _failure("reading", where, 200, {}, "a link names no resource")
            url = self._own(reference, "reading")
            if url not in found:
                found.append(url)
        return found

    def _probe(self, member):
        """One fresh read of the device: its status and body, kept when readable."""
        status, body, _headers = self.fetch(member)
        if status == 200 and isinstance(body, dict):
            self._member = body
            self.last_image = redfish_discovery.inserted_image(body)
        elif status == 404:
            self.last_image = ""
        return status, body

    def _device(self, member, doing):
        """The device as last read, reading it again after a write."""
        if self._member is None:
            status, body = self._probe(member)
            if self._member is None:
                raise _failure(doing, member, status, body)
        return self._member

    def inserted(self):
        """The image the device reports, or "" when it presents none or none is offered.

        The body kept from discovery answers until the client writes; after
        that the device is read again, and a device that then answers 404 was
        removed with its media, which reads as nothing presented.
        """
        member = self.media_member()
        self.last_image = ""
        if member and self._member is None:
            status, body = self._probe(member)
            if status != 404 and self._member is None:
                raise _failure("reading", member, status, body)
        if member and self._member is not None:
            self.last_image = redfish_discovery.inserted_image(self._member)
        return self.last_image

    def insert(self, image):
        """Attach an image and confirm the device presents it; True when it changed.

        The response is never the evidence. An accepted request may still be
        reported as a failure by an asynchronous task, and a completed task may
        still leave nothing attached, so the device itself is read back. A
        failed attempt is retried only from a device proved empty.
        """
        member = self.media_member()
        if not member:
            raise ControllerError("attaching: the controller exposes no virtual-media device")
        if redfish_discovery.image_matches(self.inserted(), image):
            return False
        reason = ""
        for attempt in range(INSERT_ATTEMPTS):
            if attempt:
                self.sleep(INSERT_RETRY_DELAY)
            reason = self._attach(member, image)
            if not reason:
                return True
            if attempt + 1 < INSERT_ATTEMPTS and self._release(member)[1]:
                raise ControllerError("the device was not released after " + reason)
        raise ControllerError(reason)

    def _attach(self, member, image):
        """One attach: the reason it failed, or "" once the device presents the image.

        A refused privilege ends the insert at once. An attach nothing answered
        may still have happened, so it is read back before anything else.
        """
        target, payload = self._attach_request(self._device(member, "attaching"), member, image)
        status, body, headers = self.fetch(target, "POST", payload, timeout=MEDIA_TIMEOUT, doing="attaching")
        if status in PRIVILEGE:
            raise _failure("attaching", target, status, body)
        if status == 202:
            reason = self._await_task(body, headers)
            if reason:
                return reason
        elif status not in (0, 200, 204):
            return str(_failure("attaching", target, status, body))
        return self._presented(member, image)

    def _attach_request(self, resource, member, image):
        """The attach this device advertises and what it is sent, standard first.

        A vendor extension is used only where no standard action exists and the
        extension's own metadata proves it takes what this client sends.
        """
        payload = {"Image": image, "Inserted": True, "WriteProtected": True}
        if redfish_discovery.transfer_protocol(image):
            payload["TransferProtocolType"] = redfish_discovery.transfer_protocol(image)
        advertised = redfish_discovery.actions(resource, "#VirtualMedia.InsertMedia")
        standard = [entry for entry in advertised if entry["source"] == "standard"]
        if standard:
            return standard[0]["target"], payload
        control = self._vmm_control(resource, "attaching")
        if control:
            return control[0], {"Image": image, "VmmControlType": "Connect"}
        if advertised:
            return advertised[0]["target"], payload
        return member.rstrip("/") + "/Actions/VirtualMedia.InsertMedia", payload

    def _vmm_control(self, resource, doing):
        """The vendor media control whose own metadata proves it fits, or None."""
        for entry in redfish_discovery.actions(resource, "#VirtualMedia.VmmControl"):
            if entry["actionInfo"]:
                info = self._read(entry["actionInfo"], doing)[0]
                if redfish_discovery.accepts_connect_disconnect(info):
                    return entry["target"], info
        return None

    def _await_task(self, body, headers):
        """Follow an accepted attach's task: the reason it failed, or "" to read back.

        A reference that is absent or names another authority is never
        requested, and the device read back decides. 202, a 5xx, no answer or a
        running state mean the task is not finished. A terminal state decides,
        Completed whatever its TaskStatus says; any other answer leaves the
        outcome to the device.
        """
        try:
            url = self._own(redfish_discovery.task_reference(body, headers), "attaching")
        except ControllerError:
            return ""
        status, task = 0, None
        for poll in range(TASK_POLLS):
            status, task, _headers = self.fetch(url, doing="attaching")
            settled, succeeded = redfish_discovery.task_settled(task)
            if settled and status in (200, 202):
                return "" if succeeded else str(_failure("attaching", url, status, task, "task " + task["TaskState"]))
            if not _task_running(status, task):
                return ""
            if poll + 1 < TASK_POLLS:
                self.sleep(TASK_POLL_DELAY)
        state = task.get("TaskState") if isinstance(task, dict) and isinstance(task.get("TaskState"), str) else ""
        return str(_failure("attaching", url, status, task, "the task is still %s after %d polls" % (
            state[:32] or "unsettled", TASK_POLLS)))

    def _presented(self, member, image):
        """Read the device back until it presents the image: "" once it does."""
        status, body = 0, None
        for probe in range(MEDIA_PROBES):
            status, body = self._probe(member)
            if status == 200 and isinstance(body, dict) and redfish_discovery.image_matches(self.last_image, image):
                return ""
            if probe + 1 < MEDIA_PROBES:
                self.sleep(MEDIA_PROBE_DELAY)
        return str(_failure("attaching", member, status, body, "the device does not present the image"))

    def eject(self):
        """Detach whatever the device presents and confirm it reports nothing.

        True when something was detached. A controller that removes the device
        entirely has ejected it too, so a device answering 404 is empty.
        """
        member = self.media_member()
        if not member:
            self.last_image = ""
            return False
        changed, reason = self._release(member)
        if reason:
            raise ControllerError(reason)
        return changed

    def _release(self, member):
        """Detach what the device presents: whether a detach was sent, and why the
        device is not proved empty, which is "" once it is."""
        if self._member is None:
            status, body = self._probe(member)
            if status == 404:
                return False, ""
            if self._member is None:
                return False, str(_failure("ejecting", member, status, body))
        if not redfish_discovery.media_present(self._member):
            self.last_image = ""
            return False, ""
        target, payload = self._eject_request(self._member, member)
        status, body, _headers = self.fetch(target, "POST", payload, doing="ejecting")
        if status in PRIVILEGE:
            raise _failure("ejecting", target, status, body)
        return True, self._emptied(member)

    def _eject_request(self, resource, member):
        """The detach this device advertises and what it is sent, standard first."""
        advertised = redfish_discovery.actions(resource, "#VirtualMedia.EjectMedia")
        standard = [entry for entry in advertised if entry["source"] == "standard"]
        if standard:
            return standard[0]["target"], {}
        control = self._vmm_control(resource, "ejecting")
        if control:
            payload = {"VmmControlType": "Disconnect"}
            if redfish_discovery.parameter_required(control[1], "Image"):
                payload["Image"] = redfish_discovery.inserted_image(resource)
            return control[0], payload
        if advertised:
            return advertised[0]["target"], {}
        return member.rstrip("/") + "/Actions/VirtualMedia.EjectMedia", {}

    def _emptied(self, member):
        """Read the device back until it reports nothing: "" once it does."""
        status, body = 0, None
        for probe in range(MEDIA_PROBES):
            status, body = self._probe(member)
            if status == 404 or (status == 200 and isinstance(body, dict) and not redfish_discovery.media_present(body)):
                self.last_image = ""
                return ""
            if probe + 1 < MEDIA_PROBES:
                self.sleep(MEDIA_PROBE_DELAY)
        return str(_failure("ejecting", member, status, body, "the device still presents media"))

    def boot_once(self, target="Cd"):
        """Select the next boot device and read the selection back.

        The precondition some firmware requires is the system's entity tag,
        from its ETag header or its body, and `*` is the one retry after a 412.
        """
        doing = "selecting the boot device"
        tag = self._tag()
        payload = {"Boot": {"BootSourceOverrideEnabled": "Once", "BootSourceOverrideTarget": target}}
        status, body, _headers = self.fetch(
            method="PATCH", payload=payload, headers={"If-Match": tag} if tag else {}, doing=doing)
        if status == 412:
            status, body, _headers = self.fetch(method="PATCH", payload=payload, headers={"If-Match": "*"}, doing=doing)
        if status != 0 and not 200 <= status < 300:
            raise _failure(doing, self.endpoint, status, body)
        status = self._await(BOOT_POLLS, BOOT_POLL_DELAY, lambda seen: redfish_discovery.boot_selected(seen, target))
        if status is not None:
            raise _failure(doing, self.endpoint, status, None, "the system does not report %s for the next boot" % target)

    def _tag(self):
        system = self.system()
        tag = self._system[1].get("etag") or system.get("@odata.etag")
        return tag if isinstance(tag, str) else ""

    def power(self, kind, expected, attempts=POLL_ATTEMPTS):
        """Ask for one reset and poll to the state it reaches; True when one was sent."""
        if self.power_state() == expected:
            return False
        target, chosen = self._reset_request(kind)
        status, body, _headers = self.fetch(target, "POST", {"ResetType": chosen}, doing="resetting")
        if status != 0 and not 200 <= status < 300:
            raise _failure("resetting", target, status, body)
        status = self._await(attempts, POLL_DELAY, lambda seen: _power(seen) == expected)
        if status is not None:
            raise _failure("resetting", self.endpoint, status, None, "the system does not report %s" % expected)
        return True

    def _reset_request(self, kind):
        """Where the reset goes and which advertised type it asks for.

        A type is chosen from what the controller advertises and never
        substituted: power-on takes the first of ForceOn, On and PushPowerButton,
        and a stop requires exactly the type it names. A controller that
        advertises no list is sent the operation's own type.
        """
        system = self.system()
        action = redfish_discovery.reset_action(system)
        info = None
        if action and action["actionInfo"] and not isinstance(
                action["action"].get("ResetType@Redfish.AllowableValues"), list):
            info = self._read(action["actionInfo"], "resetting")[0]
        allowed = redfish_discovery.allowed_reset_types(system, info)
        target = action["target"] if action else self.endpoint + "/Actions/ComputerSystem.Reset"
        if allowed is None:
            return target, kind
        chosen = redfish_discovery.power_on_reset_type(allowed) if kind == "On" else (kind if kind in allowed else "")
        if not chosen:
            wanted = " or ".join(redfish_discovery.POWER_ON_ORDER) if kind == "On" else kind
            raise ControllerError("resetting %s: the controller advertises no %s, only %s" % (
                _shown(target), wanted, ", ".join(allowed) or "nothing"))
        return target, chosen

    def _await(self, polls, delay, reached):
        """Read the system fresh until `reached` holds: None once it does, else
        the last status. An answer that is not a readable resource is not yet
        the state, so only exhaustion fails."""
        status = 0
        for poll in range(polls):
            status, body, headers = self.fetch()
            if status == 200 and isinstance(body, dict):
                self._keep(body, headers)
                if reached(body):
                    return None
            if poll + 1 < polls:
                self.sleep(delay)
        return status


def _power(system):
    state = system.get("PowerState")
    return state if state in POWER_STATES else ""


def _task_running(status, task):
    """Whether a task answer says the task has not finished yet."""
    if status == 0 or status >= 500 or status == 202:
        return True
    return status == 200 and (not isinstance(task, dict) or "TaskState" in task)


def _body(raw, method):
    """One JSON object, or None: never a list, a scalar or a cut-off body.

    A write answered with nothing has told us nothing, which is an empty
    object; a read answered with nothing has not answered.
    """
    if raw is None or len(raw) > MAX_BODY:
        return None
    if not raw:
        return {} if method in ("POST", "PATCH") else None
    try:
        value = json.loads(raw)
    except (ValueError, RecursionError):
        return None
    return value if isinstance(value, dict) else None


def _drain(refused):
    """An error answer's body, or None when even that cannot be read."""
    try:
        return refused.read(MAX_BODY + 1)
    except (OSError, ValueError, http.client.HTTPException):
        return None
    finally:
        refused.close()


def _lowered(message):
    """Response headers under lower-cased names, which every reader uses."""
    items = message.items() if message is not None and hasattr(message, "items") else []
    return {str(name).lower(): str(value) for name, value in items}


def _names_authority(reference):
    try:
        return bool(urlsplit(reference).scheme) or reference.startswith("//")
    except ValueError:
        return True


def _authority(reference, scheme=""):
    """The scheme, host and port an http or https reference names, or None.

    None is anything this client never speaks to: another scheme, user
    information, no host, or an authority urlsplit cannot read. The host is
    compared without case, and an absent port is the scheme's default.
    """
    try:
        parts = urlsplit(reference)
        port = parts.port
    except ValueError:
        return None
    scheme = parts.scheme or scheme
    if scheme not in redfish_discovery.DEFAULT_PORTS or "@" in parts.netloc or not parts.hostname:
        return None
    return scheme, parts.hostname.lower(), redfish_discovery.DEFAULT_PORTS[scheme] if port is None else port


def _refused(doing, reference, scheme):
    """The line for a reference this client will not follow.

    It names the scheme, the host and the port only, so user information in
    the reference never reaches it.
    """
    try:
        parts = urlsplit(reference)
        port = parts.port
    except ValueError:
        return ControllerError("%s: refused a reference whose authority cannot be read" % doing)
    scheme = parts.scheme or scheme
    if "@" in parts.netloc:
        why = "it carries user information"
    elif scheme not in redfish_discovery.DEFAULT_PORTS:
        why = "only http and https are spoken"
    else:
        why = "it is not this controller's endpoint"
    if port is None:
        port = redfish_discovery.DEFAULT_PORTS.get(scheme, 0)
    return ControllerError("%s: refused %s://%s:%d because %s" % (
        doing, _printable(scheme, 16), _printable(parts.hostname or "", HOST_LIMIT), port, why))


def _failure(doing, reference, status, body=None, detail=""):
    """The line for an answer an operation cannot use."""
    if not detail and status == 200 and not isinstance(body, dict):
        detail = "the body is not a Redfish resource"
    line = "%s %s: HTTP %d" % (doing, _shown(reference), status)
    if detail:
        line += ", " + detail
    found = redfish_discovery.message_id(body)
    return ControllerError(line + (" (%s)" % found if found else ""))


def _shown(reference):
    """The path of a reference, bounded and printable, for a failure line."""
    try:
        path = urlsplit(reference).path if isinstance(reference, str) else ""
    except ValueError:
        path = ""
    return _printable(path or "/", PATH_LIMIT)


def _printable(text, limit, keep=""):
    """Text cut to a bound, with anything but printable ASCII replaced."""
    return "".join(char if "!" <= char <= "~" or char in keep else "?" for char in str(text)[:limit])
