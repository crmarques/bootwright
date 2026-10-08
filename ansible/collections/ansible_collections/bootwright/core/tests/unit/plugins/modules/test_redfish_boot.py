"""Every Redfish effect, and every read a consumer decides from, goes through one client.

Four firmware shapes answer through urllib itself, so every test runs through
the client's single request path: the pinned emulator, which keeps virtual
media under the system and attaches synchronously; a manager-scoped controller
that attaches through a vendor extension and reports the outcome in a task; one
that exposes the same device under both views and demands a precondition on a
write; and the owner's xFusion iBMC as captured, redacted, which attaches
through a task and echoes an image without its non-default port. No test names
a vendor in the client.
"""

from __future__ import annotations

import ast
import base64
import http.client
import io
import json
import pathlib
import re
import ssl
import urllib.error
from urllib.parse import urlsplit

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import redfish_control
from ansible_collections.bootwright.core.plugins.modules import redfish_boot
from ansible_collections.bootwright.core.plugins.modules import redfish_system_inspect
from ansible_collections.bootwright.core.plugins.modules import redfish_system_read

ControllerError = redfish_control.ControllerError
IMAGE = "https://server.test:8443/os/m/install.iso"
PASSWORD = "p4ssw0rd-never-shown"
UUID = "5c8f2a3e-1d2b-4c5d-9e6f-7a8b9c0d1e2f"
TASK = "/redfish/v1/TaskService/Tasks/5"
MONITOR = "/redfish/v1/TaskService/TaskMonitors/5"
BUNDLE = "controller-bundle-marker"


def certificate(content):
    """A PEM block whose DER is the given bytes: the client compares DER and
    never parses the certificate, so no key is needed to stand for one."""
    return "-----BEGIN CERTIFICATE-----\n%s\n-----END CERTIFICATE-----\n" % base64.b64encode(content).decode()


SERVER = certificate(b"artifact server leaf")
ISSUER = certificate(b"artifact server issuer")
STALE = certificate(b"a certificate left by another server")


class Answer:
    """A 2xx answer the way urllib hands one back."""

    def __init__(self, status, raw, headers):
        self.status, self.headers, self.stream = status, headers, io.BytesIO(raw)

    def read(self, amount=-1):
        return self.stream.read(amount)

    def __enter__(self):
        return self

    def __exit__(self, *exc):
        return False


def respond(url, status, body=None, headers=None):
    """Answer as urllib does: a 2xx as a response, anything else raised as HTTPError."""
    raw = body if isinstance(body, bytes) else b"" if body is None else json.dumps(body).encode("utf-8")
    message = http.client.HTTPMessage()
    for name, value in (headers or {}).items():
        message[name] = value
    if 200 <= status < 300:
        return Answer(status, raw, message)
    raise urllib.error.HTTPError(url, status, "HTTP %d" % status, message, io.BytesIO(raw))


class Firmware:
    """A controller behind urllib: routed resources, scripted answers, recorded calls.

    A resource is a dict, a callable of the firmware returning an answer, a
    (status, body, headers) answer or an exception; a missing path answers 404.
    A script queues answers for one method and path ahead of the routing, and a
    scripted callable may change the firmware before it answers.
    """

    def __init__(self, endpoint, resources, write):
        parts = urlsplit(endpoint)
        self.endpoint, self.origin, self.system = endpoint, "%s://%s" % (parts.scheme, parts.netloc), parts.path
        self.resources, self.write = resources, write
        self.scripts, self.calls, self.trust, self.bundles, self.pauses = {}, [], [], [], []
        self.power, self.image, self.pending = "Off", "", ""
        self.certificates, self.verify_certificate, self.certificate_root = {}, False, ""
        self.boot = {"BootSourceOverrideEnabled": "Continuous", "BootSourceOverrideTarget": "Hdd"}

    def opener(self, verify=True, ca_data=""):
        self.trust.append(verify)
        self.bundles.append(ca_data)
        return self

    def script(self, method, path, *answers):
        self.scripts.setdefault((method, path), []).extend(answers)

    def open(self, request, timeout=None):
        method, url = request.get_method(), request.full_url
        headers = {name.lower(): value for name, value in request.header_items()}
        payload = json.loads(request.data) if request.data else None
        self.calls.append((method, url, headers, payload))
        path = urlsplit(url).path
        queued = self.scripts.get((method, path))
        answer = queued.pop(0) if queued else None
        if answer is None:
            answer = self.route(path) if method == "GET" else self.write(self, method, path, payload, headers)
        if callable(answer):
            answer = answer(self)
        if isinstance(answer, BaseException):
            raise answer
        return respond(url, *answer)

    def route(self, path):
        resource = self.resources.get(path)
        if callable(resource):
            resource = resource(self)
        root = self.certificate_root + "/"
        if resource is None and self.certificate_root and path.startswith(root) and path[len(root):] in self.certificates:
            resource = {"@odata.id": path, "CertificateString": self.certificates[path[len(root):]],
                        "CertificateType": "PEM"}
        if resource is None:
            return 404, {"error": {"@Message.ExtendedInfo": [{"MessageId": "Base.1.8.ResourceMissingAtURI"}]}}, {}
        if isinstance(resource, (tuple, BaseException)):
            return resource
        return 200, resource, {}


def gets(firmware, since=0):
    return [urlsplit(call[1]).path for call in firmware.calls[since:] if call[0] == "GET"]


def writes(firmware, since=0):
    return [(call[0], urlsplit(call[1]).path, call[3]) for call in firmware.calls[since:] if call[0] != "GET"]


def connect(monkeypatch, firmware, verify=True, ca_data=""):
    """A client on the firmware. A bundle here is a marker the fake opener
    records rather than a loadable CA, so building its context is skipped."""
    monkeypatch.setattr(redfish_control, "_opener", firmware.opener)
    if ca_data:
        monkeypatch.setattr(redfish_control, "_trust_context", lambda verify, ca_data: None)
    client = redfish_control.Client(firmware.endpoint, "operator", PASSWORD, verify=verify, ca_data=ca_data)
    client.sleep = firmware.pauses.append
    return client


def run(monkeypatch, firmware, operation, attempts=3, image="", target="Cd", **trust):
    """One module invocation: a fresh client, as main() builds one."""
    return redfish_boot.drive(connect(monkeypatch, firmware), operation, attempts, image, target, **trust)


def read(monkeypatch, firmware, media=True):
    """One read-module invocation: a fresh client, as its main() builds one."""
    return redfish_system_read.read(connect(monkeypatch, firmware), media)


def emulator():
    """The pinned sushy-tools image, as its own source serves one libvirt domain.

    Image quay.io/metal3-io/sushy-tools, digest sha256:f760343718e1..., carrying
    sushy-tools 2.2.1.dev14, whose dist-info pbr.json records git 3b57e7a. Its
    system declares EthernetInterfaces and VirtualMedia and links its manager
    (templates/system.json:99-106, :117-125) and advertises
    #ComputerSystem.Reset with ForceOn among its types (:127-139). It always
    reports BootSourceOverrideEnabled Continuous with no advertised values
    (:19, :42), because the PATCH ignores that field (main.py:593-597) and
    answers 204 (:620); the reported target follows the PATCH
    (resources/systems/libvirtdriver.py:416); nothing carries an etag or a
    SerialNumber. The manager links the system's own collection
    (main.py:366-368), which lists Cd by the system UUID
    (templates/virtual_media_collection.json:9), and Cd advertises standard
    insert and eject targets beside an empty Actions.Oem
    (templates/virtual_media.json:15-23). The insert reads only Image,
    Inserted, WriteProtected, UserName and Password and answers 204 once it has
    downloaded the image (controllers/virtual_media.py:172-204), the eject
    answers 204 (:207-225), and the reset answers 204 (main.py:713), treating
    ForceOn as On (libvirtdriver.py:299-301). Bootwright configures Cd alone,
    with MediaTypes CD and DVD.

    The device links its Certificates collection and reports VerifyCertificate
    (templates/virtual_media.json:27-30). A PATCH sets VerifyCertificate and
    answers 204 without reading If-Match (controllers/virtual_media.py:59-75);
    the collection's POST takes CertificateString and a PEM CertificateType,
    refuses any other type, and answers 204 with a Location, a member reads
    back its CertificateString, and its DELETE answers 204 (:78-145). The
    device holds one certificate, "Default", and a second POST answers 409
    (resources/vmedia.py:49, :128-165). A test may hold more members than that,
    as another controller's collection can.
    """
    system, manager = "/redfish/v1/Systems/" + UUID, "/redfish/v1/Managers/" + UUID
    media, nic = system + "/VirtualMedia", system + "/EthernetInterfaces/52:54:00:aa:bb:01"
    certificates = media + "/Cd/Certificates"

    def write(firmware, method, path, payload, headers):
        if method == "PATCH" and path == media + "/Cd":
            firmware.verify_certificate = payload["VerifyCertificate"]
        elif method == "POST" and path == certificates:
            if firmware.certificates:
                return 409, {"error": {"@Message.ExtendedInfo": [{"MessageId": "Base.1.8.ResourceAlreadyExists"}]}}, {}
            if payload.get("CertificateType") != "PEM":
                return 400, None, {}
            firmware.certificates["Default"] = payload["CertificateString"]
            return 204, None, {"Location": certificates + "/Default"}
        elif method == "DELETE" and path.startswith(certificates + "/"):
            if firmware.certificates.pop(path.rsplit("/", 1)[1], None) is None:
                return 404, None, {}
        elif path == media + "/Cd/Actions/VirtualMedia.InsertMedia":
            firmware.image = payload["Image"]
        elif path == media + "/Cd/Actions/VirtualMedia.EjectMedia":
            firmware.image = ""
        elif path == system + "/Actions/ComputerSystem.Reset":
            firmware.power = "On" if payload["ResetType"] in ("On", "ForceOn") else "Off"
        elif method == "PATCH" and path == system:
            firmware.boot["BootSourceOverrideTarget"] = payload["Boot"]["BootSourceOverrideTarget"]
        else:
            return 404, None, {}
        return 204, None, {}

    firmware = Firmware("http://bmc.test:8000" + system, {
        system: lambda f: {
            "@odata.id": system, "Id": UUID, "UUID": UUID, "Manufacturer": "Sushy Emulator",
            "PowerState": f.power, "Boot": dict(f.boot),
            "EthernetInterfaces": {"@odata.id": system + "/EthernetInterfaces"},
            "VirtualMedia": {"@odata.id": media},
            "Links": {"ManagedBy": [{"@odata.id": manager}]},
            "Actions": {"#ComputerSystem.Reset": {
                "target": system + "/Actions/ComputerSystem.Reset",
                "ResetType@Redfish.AllowableValues": [
                    "On", "ForceOff", "GracefulShutdown", "GracefulRestart", "ForceRestart", "Nmi", "ForceOn"]}}},
        system + "/EthernetInterfaces": {"Members": [{"@odata.id": nic}]},
        nic: {"MACAddress": "52:54:00:aa:bb:01", "PermanentMACAddress": "52:54:00:aa:bb:01"},
        manager: {"@odata.id": manager, "VirtualMedia": {"@odata.id": media}},
        media: {"@odata.id": media, "Members": [{"@odata.id": media + "/Cd"}]},
        media + "/Cd": lambda f: {
            "@odata.id": media + "/Cd", "Id": "Cd", "MediaTypes": ["CD", "DVD"],
            "Image": f.image, "Inserted": bool(f.image), "WriteProtected": True,
            "Certificates": {"@odata.id": certificates}, "VerifyCertificate": f.verify_certificate,
            "Actions": {
                "#VirtualMedia.EjectMedia": {"target": media + "/Cd/Actions/VirtualMedia.EjectMedia"},
                "#VirtualMedia.InsertMedia": {"target": media + "/Cd/Actions/VirtualMedia.InsertMedia"},
                "Oem": {}}},
        certificates: lambda f: {"Members": [{"@odata.id": certificates + "/" + name} for name in f.certificates]},
    }, write)
    firmware.certificate_root = certificates
    return firmware


def manager_scoped():
    """Virtual media only under the manager, attached by a vendor extension.

    No captured firmware has this shape: the captured iBMC, xfusion() below,
    lists its device under both views and advertises the standard insert. It
    is the shape .agents/knowledge/redfish-physical-bmc.md records: the system's
    own VirtualMedia path answers 404, the device hangs off the manager, and
    its only attach is an OEM VmmControl whose ActionInfo proves it takes an
    image and Connect/Disconnect, reporting the outcome in a task behind a
    monitor. The system advertises no reset action. The interface collection,
    which the page does not record, sits at the conventional path.
    """
    system, manager = "/redfish/v1/Systems/1", "/redfish/v1/Managers/1"
    device = manager + "/VirtualMedia/CD"
    control = device + "/Actions/Oem/VmmControl"

    def write(firmware, method, path, payload, headers):
        if path == control and payload["VmmControlType"] == "Connect":
            firmware.image = payload["Image"]
            return 202, {"TaskMonitor": "/redfish/v1/TaskService/TaskMonitors/9/Monitor"}, {}
        if path == control:
            firmware.image = ""
        elif path == system + "/Actions/ComputerSystem.Reset":
            firmware.power = "On" if payload["ResetType"] == "On" else "Off"
        elif method == "PATCH" and path == system:
            firmware.boot = dict(payload["Boot"])
        else:
            return 404, None, {}
        return 204, None, {}

    return Firmware("https://bmc.test" + system, {
        system: lambda f: {
            "@odata.id": system, "UUID": "4c4c4544-0042-4c10-8057-b2c04f4a3432", "SerialNumber": "SN-0042",
            "Manufacturer": "Acme", "Model": "R1", "PowerState": f.power, "Boot": dict(f.boot),
            "Links": {"ManagedBy": [{"@odata.id": manager}]}},
        system + "/EthernetInterfaces": {"Members": [{"@odata.id": system + "/EthernetInterfaces/1"}]},
        system + "/EthernetInterfaces/1": {"MACAddress": "AA-BB-CC-DD-EE-42"},
        manager: {"@odata.id": manager, "VirtualMedia": {"@odata.id": manager + "/VirtualMedia"}},
        manager + "/VirtualMedia": {"Members": [{"@odata.id": device}]},
        device: lambda f: {
            "@odata.id": device, "MediaTypes": ["DVD"], "Inserted": bool(f.image), "Image": f.image,
            "Oem": {"Acme": {"Actions": {"#VirtualMedia.VmmControl": {
                "target": control, "@Redfish.ActionInfo": device + "/VmmControlInfo"}}}}},
        device + "/VmmControlInfo": {"Parameters": [
            {"Name": "Image"}, {"Name": "VmmControlType", "AllowableValues": ["Connect", "Disconnect"]}]},
        "/redfish/v1/TaskService/Tasks/9": {"TaskState": "Completed", "TaskStatus": "OK"},
    }, write)


def dual_view():
    """The same device under both views, and a write that needs a precondition.

    The shapes .agents/knowledge/redfish-physical-bmc.md records: the system's
    undeclared VirtualMedia path and the manager both list one device, as
    iDRAC-style firmware does, which advertises only the standard insert; a
    PATCH without If-Match answers 412, as some controllers do; the reset
    advertises no GracefulShutdown. Two things the page does not record are
    added here: the entity tag arrives as an ETag response header rather than
    the body's @odata.etag, and the system declares an interface collection.
    """
    system, manager = "/redfish/v1/Systems/1", "/redfish/v1/Managers/bmc"
    device = manager + "/VirtualMedia/Cd"

    def write(firmware, method, path, payload, headers):
        if method == "PATCH" and path == system and not headers.get("if-match"):
            return 412, {"error": {"@Message.ExtendedInfo": [{"MessageId": "Base.1.8.PreconditionRequired"}]}}, {}
        if method == "PATCH" and path == system:
            firmware.boot.update(payload["Boot"])
        elif path == device + "/Actions/VirtualMedia.InsertMedia":
            firmware.image = payload["Image"]
        elif path == device + "/Actions/VirtualMedia.EjectMedia":
            firmware.image = ""
        elif path == system + "/Actions/ComputerSystem.Reset":
            firmware.power = "On" if payload["ResetType"] in ("On", "ForceOn") else "Off"
        else:
            return 404, None, {}
        return 204, None, {}

    firmware = Firmware("https://bmc.test" + system, {
        system: lambda f: (200, {
            "@odata.id": system, "UUID": "4c4c4544-0051-3510-8030-b4c04f4d3232", "PowerState": f.power,
            "Boot": dict(f.boot, **{"BootSourceOverrideEnabled@Redfish.AllowableValues": ["Once", "Continuous", "Disabled"]}),
            "EthernetInterfaces": {"@odata.id": system + "/EthernetInterfaces"},
            "Links": {"ManagedBy": [{"@odata.id": manager}]},
            "Actions": {"#ComputerSystem.Reset": {
                "target": system + "/Actions/ComputerSystem.Reset",
                "ResetType@Redfish.AllowableValues": ["On", "ForceOn", "ForceOff"]}}}, {"ETag": 'W/"tag"'}),
        system + "/EthernetInterfaces": {"Members": [{"@odata.id": system + "/EthernetInterfaces/NIC.1"}]},
        system + "/EthernetInterfaces/NIC.1": {"PermanentMACAddress": "aabbccddee51"},
        system + "/VirtualMedia": {"Members": [{"@odata.id": device}]},
        manager: {"@odata.id": manager, "VirtualMedia": {"@odata.id": manager + "/VirtualMedia"}},
        manager + "/VirtualMedia": {"Members": [{"@odata.id": device}]},
        device: lambda f: {
            "@odata.id": device, "MediaTypes": ["CD"], "Inserted": bool(f.image), "Image": f.image,
            "Actions": {"#VirtualMedia.InsertMedia": {"target": device + "/Actions/VirtualMedia.InsertMedia"}}},
    }, write)
    firmware.power = "On"
    return firmware


XFUSION_PROTOCOLS = ("Nfs", "Cifs", "https", "NFS", "CIFS", "HTTPS")
SECURITY_SERVICE = "/redfish/v1/Managers/1/SecurityService"


def xfusion():
    """The owner's xFusion iBMC as captured on 2026-05-22, redacted.

    A 2288H V7 whose manager reports FirmwareVersion 3.08.05.85. Captured, with
    every name, address and identity replaced: the system reports Manufacturer
    XFUSION and Model 2288H V7, an ETag response header, a Boot with
    BootSourceOverrideMode UEFI and target allowable values but none for
    BootSourceOverrideEnabled, EthernetInterfaces and VirtualMedia links, one
    manager in Links.ManagedBy, and a standard reset with an ActionInfo and
    inline types without ForceOn. The system and the manager each list CD,
    USBStick and iBMAUSBStick under their own path, and both CD paths show one
    device: MediaTypes CD, ConnectedVia URI or NotConnected, VerifyCertificate
    false, no Certificates link, an Oem VmmControl with an ActionInfo beside
    standard InsertMedia and EjectMedia that carry theirs. InsertMedia answers
    202 with a Running task and a Location ending /Monitor; over a connected
    device the task ends Exception, Warning, with Messages one
    iBMC.1.0.ConnectionOccupied object, and a failed fetch ends it the same way
    with iBMC.1.0.ConnectionFailed. A TransferProtocolType outside the
    ActionInfo's allowable values answers 400
    Base.1.0.ActionParameterValueFormatError. The device echoes an image
    without its non-default port. EjectMedia answers 202 with a Running task. A
    PATCH of the device answers 501 iBMC.1.0.PropertyModificationNotSupported.
    A boot PATCH answers 200 with the system, and a reset 200 with a
    Base.1.0.Success extended message. The manager's top-level SecurityService
    is null and its link sits under Oem.xFusion; the service reports
    HttpsTransferCertVerification false.

    Invented, never captured: the UUID, serial and interface address; a
    connect task that completes, Completed/OK with no message; an eject task
    that completes, and a device empty as soon as the eject is accepted (the
    captured device still presented its image right after the 202); and the
    VerifyCertificate and HttpsTransferCertVerification a test sets true.
    """
    system, manager = "/redfish/v1/Systems/1", "/redfish/v1/Managers/1"
    views = (system + "/VirtualMedia", manager + "/VirtualMedia")
    devices = tuple(view + "/CD" for view in views)
    nic = system + "/EthernetInterfaces/1"

    def system_body(f):
        return {
            "@odata.id": system, "Id": "1", "UUID": "8d3a6f20-4b1c-4e7d-9a2b-3c4d5e6f7a81",
            "SerialNumber": "SN-XF-0001", "Manufacturer": "XFUSION", "Model": "2288H V7", "PowerState": f.power,
            "Boot": dict(f.boot, **{"BootSourceOverrideMode": "UEFI", "BootSourceOverrideTarget@Redfish.AllowableValues": [
                "None", "Pxe", "Floppy", "Cd", "Hdd", "BiosSetup"]}),
            "EthernetInterfaces": {"@odata.id": system + "/EthernetInterfaces"},
            "VirtualMedia": {"@odata.id": views[0]},
            "Links": {"ManagedBy": [{"@odata.id": manager}]},
            "Actions": {"#ComputerSystem.Reset": {
                "target": system + "/Actions/ComputerSystem.Reset",
                "@Redfish.ActionInfo": system + "/ResetActionInfo",
                "ResetType@Redfish.AllowableValues": [
                    "On", "ForceOff", "GracefulShutdown", "ForceRestart", "Nmi", "ForcePowerCycle", "PowerCycle"]}}}

    def device_body(path):
        def serve(f):
            return 200, {
                "@odata.id": path, "Id": "CD", "MediaTypes": ["CD"], "Image": f.image or None,
                "Inserted": bool(f.image), "ConnectedVia": "URI" if f.image else "NotConnected",
                "TransferProtocolType": "HTTPS" if f.image else None, "VerifyCertificate": f.verify_certificate,
                "Oem": {"xFusion": {"Actions": {"#VirtualMedia.VmmControl": {
                    "target": path + "/Oem/xFusion/Actions/VirtualMedia.VmmControl",
                    "@Redfish.ActionInfo": path + "/VmmControlActionInfo"}}}},
                "Actions": {
                    "#VirtualMedia.InsertMedia": {"target": path + "/Actions/VirtualMedia.InsertMedia",
                                                  "@Redfish.ActionInfo": path + "/InsertMediaActionInfo"},
                    "#VirtualMedia.EjectMedia": {"target": path + "/Actions/VirtualMedia.EjectMedia",
                                                 "@Redfish.ActionInfo": path + "/EjectMediaActionInfo"}}}, {"ETag": 'W/"d1"'}
        return serve

    def accepted(firmware, terminal):
        firmware.tasks += 1
        task = "/redfish/v1/TaskService/Tasks/%d" % firmware.tasks
        body = {"@odata.id": task, "Id": str(firmware.tasks), "TaskState": "Running", "Messages": [],
                "TaskMonitor": "/redfish/v1/TaskService/TaskMonitors/%d" % firmware.tasks}
        reads = [{}]
        firmware.resources[task] = lambda f: dict(body, **(reads.pop(0) if reads else terminal))
        return 202, dict(body), {"Location": task + "/Monitor"}

    def failed(message):
        return {"TaskState": "Exception", "TaskStatus": "Warning", "Messages": {
            "MessageId": message, "Message": "free text the client never shows"}}

    def write(firmware, method, path, payload, headers):
        if method == "POST" and path in [device + "/Actions/VirtualMedia.InsertMedia" for device in devices]:
            if payload.get("TransferProtocolType") not in XFUSION_PROTOCOLS:
                return 400, {"error": {"@Message.ExtendedInfo": [
                    {"MessageId": "Base.1.0.ActionParameterValueFormatError"}]}}, {}
            if firmware.image:
                return accepted(firmware, failed("iBMC.1.0.ConnectionOccupied"))
            if firmware.fetch_failure:
                return accepted(firmware, failed(firmware.fetch_failure))
            parts = urlsplit(payload["Image"])
            firmware.image = parts._replace(netloc=parts.hostname).geturl()
            return accepted(firmware, {"TaskState": "Completed", "TaskStatus": "OK", "Messages": []})
        if method == "POST" and path in [device + "/Actions/VirtualMedia.EjectMedia" for device in devices]:
            firmware.image = ""
            return accepted(firmware, {"TaskState": "Completed", "TaskStatus": "OK", "Messages": []})
        if method == "PATCH" and path in devices:
            return 501, {"error": {"@Message.ExtendedInfo": [
                {"MessageId": "iBMC.1.0.PropertyModificationNotSupported"}]}}, {}
        if method == "PATCH" and path == system:
            firmware.boot.update(payload["Boot"])
            return 200, system_body(firmware), {"ETag": 'W/"x1"'}
        if method == "POST" and path == system + "/Actions/ComputerSystem.Reset":
            firmware.power = "On" if payload["ResetType"] == "On" else "Off"
            return 200, {"error": {"@Message.ExtendedInfo": [{"MessageId": "Base.1.0.Success"}]}}, {}
        return 404, None, {}

    resources = {
        system: lambda f: (200, system_body(f), {"ETag": 'W/"x1"'}),
        system + "/EthernetInterfaces": {"Members": [{"@odata.id": nic}]},
        nic: {"@odata.id": nic, "MACAddress": "aa:bb:cc:dd:ee:71"},
        manager: {"@odata.id": manager, "FirmwareVersion": "3.08.05.85", "SecurityService": None,
                  "VirtualMedia": {"@odata.id": views[1]},
                  "Oem": {"xFusion": {"SecurityService": {"@odata.id": SECURITY_SERVICE}}}},
        SECURITY_SERVICE: lambda f: {"@odata.id": SECURITY_SERVICE, "Id": "SecurityService",
                                     "HttpsTransferCertVerification": f.transfer_verification},
    }
    for view, device in zip(views, devices):
        resources[view] = {"@odata.id": view, "Members": [
            {"@odata.id": view + "/" + name} for name in ("CD", "USBStick", "iBMAUSBStick")]}
        resources[device] = device_body(device)
    firmware = Firmware("https://bmc.test" + system, resources, write)
    firmware.boot = {"BootSourceOverrideEnabled": "Disabled", "BootSourceOverrideTarget": "None"}
    firmware.transfer_verification, firmware.fetch_failure, firmware.tasks = False, "", 0
    return firmware


SHAPES = {"emulator": emulator, "manager-scoped": manager_scoped, "dual-view": dual_view, "xfusion": xfusion}
CONNECT = {"Image": IMAGE, "Inserted": True, "TransferProtocolType": "HTTPS"}
EXPECTED = {
    "emulator": {
        "attach": ("/VirtualMedia/Cd/Actions/VirtualMedia.InsertMedia", CONNECT),
        "detach": ("/VirtualMedia/Cd/Actions/VirtualMedia.EjectMedia", {}),
        "precondition": [None], "enabled": "Continuous",
        "resets": {"power-off": "ForceOff", "shutdown": "GracefulShutdown", "power-on": "ForceOn"},
    },
    "manager-scoped": {
        "attach": ("/Managers/1/VirtualMedia/CD/Actions/Oem/VmmControl", {"Image": IMAGE, "VmmControlType": "Connect"}),
        "detach": ("/Managers/1/VirtualMedia/CD/Actions/Oem/VmmControl", {"VmmControlType": "Disconnect"}),
        "precondition": [None], "enabled": "Once",
        "resets": {"power-off": "ForceOff", "shutdown": "GracefulShutdown", "power-on": "On"},
    },
    "dual-view": {
        "attach": ("/Managers/bmc/VirtualMedia/Cd/Actions/VirtualMedia.InsertMedia", CONNECT),
        "detach": ("/Managers/bmc/VirtualMedia/Cd/Actions/VirtualMedia.EjectMedia", {}),
        "precondition": ['W/"tag"'], "enabled": "Once",
        "resets": {"power-off": "ForceOff", "shutdown": None, "power-on": "ForceOn"},
    },
    "xfusion": {
        "attach": ("/Managers/1/VirtualMedia/CD/Actions/VirtualMedia.InsertMedia", CONNECT),
        "detach": ("/Managers/1/VirtualMedia/CD/Actions/VirtualMedia.EjectMedia", {}),
        "precondition": ['W/"x1"'], "enabled": "Once",
        "resets": {"power-off": "ForceOff", "shutdown": "GracefulShutdown", "power-on": "On"},
        "echo": "https://server.test/os/m/install.iso",
    },
}


# Each operation a consumer runs, on each shape, is one client call whose
# writes are exactly the ones the controller advertises, and whose report is
# what the controller said last.
@pytest.mark.parametrize("shape", sorted(SHAPES))
def test_every_operation_goes_through_the_client(monkeypatch, shape):
    firmware, expected = SHAPES[shape](), EXPECTED[shape]
    power, echo = firmware.power, EXPECTED[shape].get("echo", IMAGE)

    assert read(monkeypatch, firmware) == (power, "")
    assert not writes(firmware) and gets(firmware).count(firmware.system) == 1

    mark = len(firmware.calls)
    assert run(monkeypatch, firmware, "insert", image=IMAGE) == (True, power, echo)
    [(method, path, payload)] = writes(firmware, mark)
    assert (method, path.endswith(expected["attach"][0]), payload) == ("POST", True, expected["attach"][1])
    if shape == "manager-scoped":
        assert "/redfish/v1/TaskService/Tasks/9" in gets(firmware, mark)
    mark = len(firmware.calls)
    assert run(monkeypatch, firmware, "insert", image=IMAGE) == (False, power, echo)
    assert read(monkeypatch, firmware) == (power, echo)
    assert not writes(firmware, mark)

    for target in ("Cd", "Hdd"):
        mark = len(firmware.calls)
        assert run(monkeypatch, firmware, "boot", target=target) == (True, power, "")
        patches = [call[2].get("if-match") for call in firmware.calls[mark:] if call[0] == "PATCH"]
        assert patches == expected["precondition"]
        assert firmware.boot["BootSourceOverrideTarget"] == target
        assert firmware.boot["BootSourceOverrideEnabled"] == expected["enabled"]

    mark = len(firmware.calls)
    assert run(monkeypatch, firmware, "eject") == (True, power, "")
    [(method, path, payload)] = writes(firmware, mark)
    assert (method, path.endswith(expected["detach"][0]), payload) == ("POST", True, expected["detach"][1])
    mark = len(firmware.calls)
    assert run(monkeypatch, firmware, "eject") == (False, power, "")
    assert not writes(firmware, mark)

    for operation, state in (("power-off", "Off"), ("shutdown", "Off"), ("power-on", "On")):
        firmware.power = "Off" if operation == "power-on" else "On"
        mark = len(firmware.calls)
        if expected["resets"][operation] is None:
            with pytest.raises(ControllerError, match="GracefulShutdown"):
                run(monkeypatch, firmware, operation)
            assert not writes(firmware, mark)
            continue
        assert run(monkeypatch, firmware, operation) == (True, state, "")
        [(method, path, payload)] = writes(firmware, mark)
        assert path == firmware.system + "/Actions/ComputerSystem.Reset"
        assert payload == {"ResetType": expected["resets"][operation]}

    assert all(call[1].startswith(firmware.origin + "/") for call in firmware.calls)
    assert all(call[2]["authorization"].startswith("Basic ") for call in firmware.calls)


# A boot selection and a power operation never look for media, so a media view
# that is broken cannot fail them and they report no image.
def test_power_and_boot_never_discover_media(monkeypatch):
    firmware = emulator()
    firmware.resources[firmware.system + "/VirtualMedia"] = (500, None, {})
    firmware.resources["/redfish/v1/Managers/" + UUID] = (500, None, {})
    for operation in ("boot", "power-on", "power-off", "shutdown"):
        firmware.power = "Off" if operation == "power-on" else "On"
        changed, power, media = run(monkeypatch, firmware, operation)
        assert changed and power and media == ""
    assert not [path for path in gets(firmware) if "/VirtualMedia" in path or "/Managers/" in path]


def test_no_operation_builds_a_controller_path_itself():
    for helper in ("request", "power_state", "media_inserted", "insert_media", "eject_media",
                   "boot_once", "reset", "await_power"):
        assert not hasattr(redfish_control, helper), helper
    for module in (redfish_boot, redfish_system_read):
        tree = ast.parse(pathlib.Path(module.__file__).read_text(encoding="utf-8"))
        literals = [node.value for node in ast.walk(tree)
                    if isinstance(node, ast.Constant) and isinstance(node.value, str)]
        assert not [text for text in literals if "/VirtualMedia" in text or "/Actions/" in text], module.__name__


# A power read is what a poll repeats while it waits, so it reads the system
# once and nothing else: a media view that cannot be read, and a manager that
# does not answer, cannot fail it or cost it a request.
@pytest.mark.parametrize("broken", [False, True], ids=["answering", "media views 500"])
@pytest.mark.parametrize("shape", sorted(SHAPES))
def test_a_power_read_makes_one_request_and_never_looks_for_media(monkeypatch, shape, broken):
    firmware = SHAPES[shape]()
    if broken:
        for path in [firmware.system + "/VirtualMedia"] + list(firmware.resources):
            if "/VirtualMedia" in path or "/Managers/" in path:
                firmware.resources[path] = (500, None, {})
    assert read(monkeypatch, firmware, media=False) == (firmware.power, "")
    assert [call[:2] for call in firmware.calls] == [("GET", firmware.origin + firmware.system)]


# A media read discovers the device before it reports what it presents, and
# changes nothing; on the pinned emulator that is four reads.
@pytest.mark.parametrize("shape", sorted(SHAPES))
def test_a_media_read_reports_what_the_device_presents(monkeypatch, shape):
    firmware = SHAPES[shape]()
    assert read(monkeypatch, firmware) == (firmware.power, "")
    firmware.image = IMAGE
    mark = len(firmware.calls)
    assert read(monkeypatch, firmware) == (firmware.power, IMAGE)
    assert not writes(firmware) and gets(firmware, mark).count(firmware.system) == 1
    if shape == "emulator":
        assert gets(firmware, mark) == [firmware.system, firmware.system + "/VirtualMedia",
                                        "/redfish/v1/Managers/" + UUID, firmware.system + "/VirtualMedia/Cd"]


class Built(Exception):
    """What a module's main() asked AnsibleModule for, raised before anything runs."""


def built(**arguments):
    raise Built(arguments)


class Module:
    """The part of AnsibleModule a read module's main() uses, recording how it ended."""

    def __init__(self, params):
        self.params, self.arguments, self.ended = params, None, None

    def build(self, **arguments):
        self.arguments = arguments
        return self

    def exit_json(self, **result):
        self.ended = ("exit", result)

    def fail_json(self, **result):
        self.ended = ("fail", result)


# The boot module only drives. A read offered there would be one a consumer
# could reach through a module that is allowed to change the machine.
def test_the_boot_module_offers_no_read(monkeypatch):
    monkeypatch.setattr(redfish_boot, "AnsibleModule", built)
    with pytest.raises(Built) as spec:
        redfish_boot.main()
    assert "read" not in spec.value.args[0]["argument_spec"]["operation"]["choices"]
    documented = re.search(r"operation:.*?choices: \[([^\]]*)\]", redfish_boot.DOCUMENTATION, re.S).group(1)
    assert "read" not in [choice.strip() for choice in documented.split(",")]
    firmware = emulator()
    with pytest.raises(ControllerError, match="read is not an operation this module drives"):
        run(monkeypatch, firmware, "read")
    assert not firmware.calls


# The read module changes nothing and says so, reports what the controller
# said, and fails closed: a controller that does not answer is never an empty
# answer.
def test_the_read_module_reports_what_it_read_and_fails_closed(monkeypatch):
    monkeypatch.setattr(redfish_system_read, "AnsibleModule", built)
    with pytest.raises(Built) as spec:
        redfish_system_read.main()
    assert spec.value.args[0]["supports_check_mode"] is True
    assert spec.value.args[0]["argument_spec"]["password"]["no_log"] is True
    assert spec.value.args[0]["argument_spec"]["media"] == {"type": "bool", "default": False}

    for answer, ended in (({}, "exit"), ((500, None, {}), "fail")):
        firmware = emulator()
        firmware.image = IMAGE
        if answer:
            firmware.resources[firmware.system] = answer
        module = Module({"endpoint": firmware.endpoint, "user": "operator", "password": PASSWORD,
                         "verify": True, "ca_data": "", "media": True})
        monkeypatch.setattr(redfish_system_read, "AnsibleModule", module.build)
        monkeypatch.setattr(redfish_control, "_opener", firmware.opener)
        redfish_system_read.main()
        assert module.ended[0] == ended
        if ended == "exit":
            assert module.ended[1] == {"changed": False, "power": "Off", "media": IMAGE}
        else:
            assert module.ended[1]["msg"].startswith("the management controller could not be read: ")
            assert firmware.system in module.ended[1]["msg"] and PASSWORD not in module.ended[1]["msg"]
        assert not writes(firmware)


UNREADABLE = {
    "system 500": ("emulator", lambda f: f.system, (500, None, {}), 500),
    "declared collection 500": ("emulator", lambda f: f.system + "/VirtualMedia", (500, None, {}), 500),
    "declared collection 404": ("emulator", lambda f: f.system + "/VirtualMedia", None, 404),
    "manager 500": ("emulator", lambda f: "/redfish/v1/Managers/" + UUID, (500, None, {}), 500),
    "manager 403": ("emulator", lambda f: "/redfish/v1/Managers/" + UUID, (403, None, {}), 403),
    "undeclared fallback 500": ("manager-scoped", lambda f: f.system + "/VirtualMedia", (500, None, {}), 500),
    "undeclared fallback 401": ("manager-scoped", lambda f: f.system + "/VirtualMedia", (401, None, {}), 401),
    "undeclared fallback 403": ("manager-scoped", lambda f: f.system + "/VirtualMedia", (403, None, {}), 403),
    "member 500": ("emulator", lambda f: f.system + "/VirtualMedia/Cd", (500, None, {}), 500),
    "transport": ("emulator", lambda f: f.system + "/VirtualMedia", urllib.error.URLError("refused"), 0),
}


# A view that cannot be read is not a controller without media: reading it as
# one would let an ejection be proved against a device that was never found.
@pytest.mark.parametrize("case", sorted(UNREADABLE))
def test_an_unreadable_controller_is_a_failure_not_an_empty_answer(monkeypatch, case):
    shape, where, answer, status = UNREADABLE[case]
    firmware = SHAPES[shape]()
    path = where(firmware)
    firmware.resources[path] = answer
    with pytest.raises(ControllerError) as failure:
        read(monkeypatch, firmware)
    line = str(failure.value)
    assert path in line and "HTTP %d" % status in line and "\n" not in line


# Only a view the controller never declared, answering that it does not exist
# there, reads as no media offered; inserting into such a controller refuses.
@pytest.mark.parametrize("status", [404, 400, 405, 501])
def test_a_controller_that_says_it_has_no_media_offers_none(monkeypatch, status):
    firmware = manager_scoped()
    manager = "/redfish/v1/Managers/1"
    firmware.resources[manager] = {"@odata.id": manager}
    firmware.resources[firmware.system + "/VirtualMedia"] = (status, None, {})
    firmware.resources[manager + "/VirtualMedia"] = (status, None, {})
    assert read(monkeypatch, firmware) == ("Off", "")
    with pytest.raises(ControllerError, match="the controller exposes no virtual-media device"):
        run(monkeypatch, firmware, "insert", image=IMAGE)
    assert not writes(firmware)


# A device that answers but is not optical is not where installer media goes:
# nothing is read from it, attached to it or proved against it.
def test_a_device_that_is_not_optical_is_never_used(monkeypatch):
    firmware = emulator()
    media = firmware.system + "/VirtualMedia"
    stick = media + "/USB1"
    firmware.resources[media] = {"@odata.id": media, "Members": [{"@odata.id": stick}]}
    firmware.resources[stick] = {
        "@odata.id": stick, "Id": "USB1", "MediaTypes": ["USBStick"], "Inserted": True,
        "Image": "https://server.test:8443/os/stick.img",
        "Actions": {"#VirtualMedia.InsertMedia": {"target": stick + "/Actions/VirtualMedia.InsertMedia"},
                    "#VirtualMedia.EjectMedia": {"target": stick + "/Actions/VirtualMedia.EjectMedia"}}}
    assert read(monkeypatch, firmware) == ("Off", "")
    assert stick in gets(firmware)
    with pytest.raises(ControllerError, match="the controller exposes no virtual-media device"):
        run(monkeypatch, firmware, "insert", image=IMAGE)
    assert run(monkeypatch, firmware, "eject") == (False, "Off", "")
    assert not writes(firmware)


# The oversized body is a whole object followed by padding, so what the bounded
# read returns is valid JSON and only the bound refuses it.
@pytest.mark.parametrize("body", [
    b"[]",
    b'{"PowerState": "On", "UUID": "ab',
    b'{"PowerState": "On"}' + b" " * redfish_control.MAX_BODY,
    b"",
], ids=["list", "truncated", "oversized", "empty"])
def test_a_body_that_is_not_a_resource_is_unreadable(monkeypatch, body):
    firmware = emulator()
    firmware.resources[firmware.system] = (200, body, {})
    with pytest.raises(ControllerError) as failure:
        read(monkeypatch, firmware)
    line = str(failure.value)
    assert firmware.system in line and "HTTP 200" in line and "\n" not in line


def completing(task):
    """A task answer that is the moment the controller attaches the image."""
    def answer(firmware):
        firmware.image = firmware.pending
        return 200, task, {}
    return answer


RUNNING = (200, {"TaskState": "Running"}, {})
COMPLETED = {"TaskState": "Completed", "TaskStatus": "OK"}
FOLLOWED = (202, {"@odata.id": TASK}, {})
# Each case: the attach's answer, scripted task answers, the task's answer from
# then on, whether the device presents the image as soon as the attach is
# accepted, how many task polls reach the controller, and the refusal if any.
ASYNCHRONOUS = {
    "monitor 202 then 204": ((202, None, {"Location": MONITOR}), [(202, None, {})], (204, None, {}), True, 2, ""),
    "running then completed": (FOLLOWED, [RUNNING], completing(COMPLETED), False, 2, ""),
    "interrupted then completed": (FOLLOWED, [(200, {"TaskState": "Interrupted"}, {}), RUNNING],
                                   completing(COMPLETED), False, 3, ""),
    "5xx and no answer keep polling": (FOLLOWED, [(503, None, {}), TimeoutError("slow")], completing(COMPLETED),
                                       False, 3, ""),
    "exception": (FOLLOWED, [], (200, {"TaskState": "Exception", "TaskStatus": "Critical",
                                       "Messages": [{"MessageId": "ConnectionFailed"}]}, {}),
                  False, 3, "task Exception (ConnectionFailed)"),
    "exception with one message object": (FOLLOWED, [], (200, {"TaskState": "Exception", "TaskStatus": "Warning",
                                                               "Messages": {"MessageId": "iBMC.1.0.ConnectionFailed"}}, {}),
                                          False, 3, "task Exception (iBMC.1.0.ConnectionFailed)"),
    "completed with a warning": (FOLLOWED, [], completing({"TaskState": "Completed", "TaskStatus": "Warning"}),
                                 False, 1, ""),
    "monitor 404": ((202, None, {"Location": MONITOR}), [], (404, None, {}), True, 1, ""),
    "location elsewhere": ((202, None, {"Location": "https://elsewhere.test" + TASK}), [], RUNNING, True, 0, ""),
    "still running": (FOLLOWED, [], RUNNING, False, 12, "still Running after 4 polls"),
}


# An accepted attach is followed to its task's end and then read back on the
# device, which alone decides; a task reference elsewhere is never requested.
@pytest.mark.parametrize("case", sorted(ASYNCHRONOUS))
def test_an_asynchronous_attach_settles_on_the_device(monkeypatch, case):
    accepted, scripted, then, at_once, polls, refusal = ASYNCHRONOUS[case]
    firmware = emulator()
    standard = firmware.write

    def write(firmware, method, path, payload, headers):
        if not path.endswith("VirtualMedia.InsertMedia"):
            return standard(firmware, method, path, payload, headers)
        firmware.pending = payload["Image"]
        if at_once:
            firmware.image = firmware.pending
        return accepted

    firmware.write = write
    firmware.script("GET", TASK, *scripted)
    firmware.resources[TASK] = then
    monkeypatch.setattr(redfish_control, "TASK_POLLS", 4)
    if refusal:
        with pytest.raises(ControllerError, match=re.escape(refusal)):
            run(monkeypatch, firmware, "insert", image=IMAGE)
    else:
        assert run(monkeypatch, firmware, "insert", image=IMAGE) == (True, "Off", IMAGE)
        assert len(writes(firmware)) == 1, "the first attach settles; nothing is retried"
    assert gets(firmware).count(TASK) == polls
    assert all(call[1].startswith(firmware.origin + "/") for call in firmware.calls)


INSERT = "/VirtualMedia/Cd/Actions/VirtualMedia.InsertMedia"
EJECT = "/VirtualMedia/Cd/Actions/VirtualMedia.EjectMedia"


# An attach nothing answered may still have happened, so the device is read
# before anything is detached or sent again.
def test_an_attach_that_timed_out_is_read_back_before_it_is_retried(monkeypatch):
    firmware = emulator()

    def timed_out(firmware):
        firmware.image = IMAGE
        return TimeoutError("the attach outlived its bound")

    firmware.script("POST", firmware.system + INSERT, timed_out)
    assert run(monkeypatch, firmware, "insert", image=IMAGE) == (True, "Off", IMAGE)
    assert [path for method, path, payload in writes(firmware)] == [firmware.system + INSERT]


OTHER = "https://server.test:8443/os/old.iso"


# A second attach is sent only to a device proved empty; one that could not be
# released ends the insert, naming both what failed and that it stayed attached.
def test_a_retry_starts_from_an_empty_device(monkeypatch):
    firmware = emulator()

    def left_other_media(firmware):
        firmware.image = OTHER
        return 500, None, {}

    firmware.script("POST", firmware.system + INSERT, left_other_media)
    firmware.script("POST", firmware.system + EJECT, (204, None, {}))
    with pytest.raises(ControllerError) as failure:
        run(monkeypatch, firmware, "insert", image=IMAGE)
    assert "HTTP 500" in str(failure.value) and "not released" in str(failure.value)
    assert [path for method, path, payload in writes(firmware)] == [firmware.system + INSERT, firmware.system + EJECT]


# A device presenting other media is released, and read back empty, before the
# first attach, so that attach is not refused as occupied and spends no retry.
@pytest.mark.parametrize("shape", ["emulator", "xfusion"])
def test_a_device_presenting_other_media_is_released_before_the_first_attach(monkeypatch, shape):
    firmware, expected = SHAPES[shape](), EXPECTED[shape]
    firmware.image = OTHER
    assert run(monkeypatch, firmware, "insert", image=IMAGE)[0] is True
    assert firmware.image == expected.get("echo", IMAGE)
    posts = [(index, urlsplit(call[1]).path) for index, call in enumerate(firmware.calls) if call[0] != "GET"]
    assert len(posts) == 2
    (eject_at, detach), (attach_at, attach) = posts
    assert detach.endswith(expected["detach"][0]) and attach.endswith(expected["attach"][0])
    device = detach[:-len("/Actions/VirtualMedia.EjectMedia")]
    read_back = [call for call in firmware.calls[eject_at + 1:attach_at] if urlsplit(call[1]).path == device]
    assert read_back and all(call[0] == "GET" for call in read_back)
    assert redfish_control.INSERT_RETRY_DELAY not in firmware.pauses


# A device whose other media cannot be released is never attached to: the
# insert ends naming the release that was not proved.
def test_a_device_that_cannot_be_released_is_never_attached(monkeypatch):
    firmware = emulator()
    firmware.image = OTHER
    firmware.script("POST", firmware.system + EJECT, (204, None, {}))
    with pytest.raises(ControllerError) as failure:
        run(monkeypatch, firmware, "insert", image=IMAGE)
    assert "was not released" in str(failure.value) and "still presents media" in str(failure.value)
    assert [path for _method, path, _payload in writes(firmware)] == [firmware.system + EJECT]


# A fetch the controller reports failed, or after which the device never
# presents the image, names the causes on the controller's side of the leg; a
# request the controller refused does not.
def test_a_failed_fetch_names_the_controller_side_causes(monkeypatch):
    failing = xfusion()
    failing.fetch_failure = "iBMC.1.0.ConnectionFailed"
    with pytest.raises(ControllerError) as failure:
        run(monkeypatch, failing, "insert", image=IMAGE)
    assert "(iBMC.1.0.ConnectionFailed)" in str(failure.value)
    assert str(failure.value).endswith("; " + redfish_control.FETCH_CAUSES)

    module = Module({"endpoint": failing.endpoint, "user": "operator", "password": PASSWORD, "verify": True,
                     "ca_data": "", "operation": "insert", "image": IMAGE, "target": "Cd", "attempts": 3,
                     "trust": "established", "certificate": "", "restore_verification": False,
                     "remove_certificate": False, "private_delivery": False})
    monkeypatch.setattr(redfish_boot, "AnsibleModule", module.build)
    monkeypatch.setattr(redfish_control, "_opener", failing.opener)
    monkeypatch.setattr(redfish_control.time, "sleep", lambda seconds: None)
    redfish_boot.main()
    assert module.ended[0] == "fail" and module.ended[1]["msg"].endswith(redfish_control.FETCH_CAUSES)
    assert len(module.ended[1]["msg"]) <= 512

    silent = emulator()
    for _attempt in range(redfish_control.INSERT_ATTEMPTS):
        silent.script("POST", silent.system + INSERT, FOLLOWED)
    silent.resources[TASK] = (200, COMPLETED, {})
    with pytest.raises(ControllerError) as failure:
        run(monkeypatch, silent, "insert", image=IMAGE)
    assert "the device does not present the image" in str(failure.value)
    assert str(failure.value).endswith("; " + redfish_control.FETCH_CAUSES)

    refusing = xfusion()
    with pytest.raises(ControllerError) as failure:
        run(monkeypatch, refusing, "insert", image="http://server.test:8080/os/m/install.iso")
    assert "HTTP 400" in str(failure.value) and "Base.1.0.ActionParameterValueFormatError" in str(failure.value)
    assert redfish_control.FETCH_CAUSES not in str(failure.value)


def test_the_fetch_causes_fit_the_module_failure():
    assert len(redfish_control.FETCH_CAUSES) <= 250
    assert all(" " <= char <= "~" for char in redfish_control.FETCH_CAUSES + redfish_control.NO_VERIFIED_FETCH)
    worst = "the management controller did not complete insert: " + str(
        ControllerError("x" * 400, redfish_control.FETCH_CAUSES))
    assert len(worst) <= 512


PRIVATE = {"private_delivery": True}


def verifying(firmware, device=True, manager=True):
    firmware.verify_certificate, firmware.transfer_verification = device, manager
    return firmware


# Private material under established trust is attached only once reads alone
# prove the controller verifies the server: the device's VerifyCertificate and
# the security service's HttpsTransferCertVerification, before any write.
def test_private_delivery_under_established_proves_the_controller_verifies_before_any_write(monkeypatch):
    firmware = verifying(xfusion())
    assert run(monkeypatch, firmware, "insert", image=IMAGE, **PRIVATE)[0] is True
    paths = [(call[0], urlsplit(call[1]).path) for call in firmware.calls]
    attach = ("POST", "/redfish/v1" + EXPECTED["xfusion"]["attach"][0])
    assert paths.index(("GET", SECURITY_SERVICE)) < paths.index(attach)
    assert [path for _method, path, _payload in writes(firmware)] == [attach[1]]


@pytest.mark.parametrize("other", ["", OTHER], ids=["empty", "holding other media"])
def test_private_delivery_refuses_a_device_that_does_not_verify(monkeypatch, other):
    firmware = xfusion()
    firmware.image = other
    with pytest.raises(ControllerError) as failure:
        run(monkeypatch, firmware, "insert", image=IMAGE, **PRIVATE)
    assert "VerifyCertificate reads false" in str(failure.value)
    assert str(failure.value).endswith("; " + redfish_control.NO_VERIFIED_FETCH)
    assert writes(firmware) == [] and firmware.image == other


def test_private_delivery_refuses_a_manager_that_does_not_verify(monkeypatch):
    firmware = verifying(xfusion(), manager=False)
    with pytest.raises(ControllerError) as failure:
        run(monkeypatch, firmware, "insert", image=IMAGE, **PRIVATE)
    assert SECURITY_SERVICE + ": HttpsTransferCertVerification reads false" in str(failure.value)
    assert str(failure.value).endswith("; " + redfish_control.NO_VERIFIED_FETCH)
    assert writes(firmware) == []


# A controller whose manager links no security service is decided by the
# device alone, and an absent VerifyCertificate reads as off.
def test_private_delivery_without_a_security_service_is_decided_by_the_device(monkeypatch):
    firmware = verifying(emulator())
    assert run(monkeypatch, firmware, "insert", image=IMAGE, **PRIVATE) == (True, "Off", IMAGE)
    refusing = emulator()
    with pytest.raises(ControllerError, match="VerifyCertificate reads false"):
        run(monkeypatch, refusing, "insert", image=IMAGE, **PRIVATE)
    absent = emulator()
    serve = absent.resources[absent.system + DEVICE]
    absent.resources[absent.system + DEVICE] = lambda f: {
        key: value for key, value in serve(f).items() if key != "VerifyCertificate"}
    with pytest.raises(ControllerError, match="VerifyCertificate reads nothing"):
        run(monkeypatch, absent, "insert", image=IMAGE, **PRIVATE)
    for each in (firmware, refusing, absent):
        assert not [path for path in gets(each) if "SecurityService" in path]
    assert writes(refusing) == [] and writes(absent) == []


def test_an_unreadable_security_service_refuses_private_delivery(monkeypatch):
    firmware = verifying(xfusion())
    firmware.resources[SECURITY_SERVICE] = (500, None, {})
    with pytest.raises(ControllerError) as failure:
        run(monkeypatch, firmware, "insert", image=IMAGE, **PRIVATE)
    assert SECURITY_SERVICE + ": HTTP 500" in str(failure.value)
    assert writes(firmware) == []


def test_established_without_private_delivery_reads_no_security_service(monkeypatch):
    firmware = xfusion()
    assert run(monkeypatch, firmware, "insert", image=IMAGE)[0] is True
    assert SECURITY_SERVICE not in gets(firmware)


def test_private_delivery_refuses_disable_verification_before_any_request(monkeypatch):
    firmware = verifying(xfusion())
    with pytest.raises(ControllerError, match="an insert carrying private material refuses disable-verification"):
        run(monkeypatch, firmware, "insert", image=IMAGE, trust=redfish_control.TRUST_DISABLED, **PRIVATE)
    assert firmware.calls == []


# The module Ansible runs carries private_delivery to the client: a play that
# asks for it on the captured iBMC is refused before any write.
def test_the_module_carries_private_delivery_to_the_client(monkeypatch):
    firmware = xfusion()
    module = Module({"endpoint": firmware.endpoint, "user": "operator", "password": PASSWORD, "verify": True,
                     "ca_data": "", "operation": "insert", "image": IMAGE, "target": "Cd", "attempts": 3,
                     "trust": "established", "certificate": "", "restore_verification": False,
                     "remove_certificate": False, "private_delivery": True})
    monkeypatch.setattr(redfish_boot, "AnsibleModule", module.build)
    monkeypatch.setattr(redfish_control, "_opener", firmware.opener)
    monkeypatch.setattr(redfish_control.time, "sleep", lambda seconds: None)
    redfish_boot.main()
    assert module.ended[0] == "fail"
    assert "VerifyCertificate reads false" in module.ended[1]["msg"]
    assert module.ended[1]["msg"].endswith(redfish_control.NO_VERIFIED_FETCH)
    assert writes(firmware) == []


# Importing the certificate turns verification on and reads it back, so a
# private insert under import-certificate reads nothing more and is not
# refused by a device that verifies nothing before the import.
def test_private_delivery_under_import_certificate_adds_no_read(monkeypatch):
    firmware = emulator()
    assert firmware.verify_certificate is False
    assert run(monkeypatch, firmware, "insert", image=IMAGE, **dict(IMPORT, **PRIVATE)) == (True, "Off", IMAGE)
    assert trust_writes(firmware) == [
        ("POST", CERTIFICATES, {"CertificateString": SERVER, "CertificateType": "PEM"}),
        ("PATCH", DEVICE, {"VerifyCertificate": True})]
    assert not [path for path in gets(firmware) if "SecurityService" in path]


# A device already presenting the image is left alone: an idempotent apply
# with private delivery reads no verification and writes nothing.
def test_private_delivery_over_the_presented_image_reads_no_verification(monkeypatch):
    firmware = xfusion()
    firmware.image = EXPECTED["xfusion"]["echo"]
    assert run(monkeypatch, firmware, "insert", image=IMAGE, **PRIVATE) == (
        False, firmware.power, EXPECTED["xfusion"]["echo"])
    assert writes(firmware) == []
    assert SECURITY_SERVICE not in gets(firmware)


# A security service that does not report HttpsTransferCertVerification adds
# no condition: it is read, and the device decides.
def test_a_security_service_without_the_setting_adds_no_condition(monkeypatch):
    firmware = verifying(xfusion())
    firmware.resources[SECURITY_SERVICE] = {"@odata.id": SECURITY_SERVICE, "Id": "SecurityService"}
    assert run(monkeypatch, firmware, "insert", image=IMAGE, **PRIVATE)[0] is True
    assert SECURITY_SERVICE in gets(firmware)
    assert [path for _method, path, _payload in writes(firmware)] == [
        "/redfish/v1" + EXPECTED["xfusion"]["attach"][0]]


# A controller that echoes the image in another spelling of the same URL, a
# host in capitals, a default port dropped, or a non-default port dropped as
# the captured iBMC does, presents it, so nothing is sent.
@pytest.mark.parametrize("image, echoed", [
    (IMAGE, "https://SERVER.test:8443/os/m/install.iso"),
    ("https://server.test:443/os/m/install.iso", "https://server.test/os/m/install.iso"),
    (IMAGE, "https://server.test/os/m/install.iso"),
], ids=["host case", "default port", "non-default port dropped"])
def test_an_image_echoed_in_another_spelling_is_not_attached_again(monkeypatch, image, echoed):
    firmware = emulator()
    firmware.image = echoed
    assert run(monkeypatch, firmware, "insert", image=image) == (False, "Off", echoed)
    assert not writes(firmware)


# The entity tag comes from the ETag header whatever its spelling, and a 412 is
# retried exactly once with `*`.
@pytest.mark.parametrize("name", ["etag", "ETag"])
def test_the_precondition_comes_from_the_etag_header(monkeypatch, name):
    firmware = emulator()
    serve = firmware.resources[firmware.system]
    firmware.resources[firmware.system] = lambda f: (200, serve(f), {name: 'W/"7"'})
    firmware.script("PATCH", firmware.system, (412, None, {}))
    assert run(monkeypatch, firmware, "boot") == (True, "Off", "")
    assert [call[2].get("if-match") for call in firmware.calls if call[0] == "PATCH"] == ['W/"7"', "*"]


# Firmware that carries its entity tag only in the body is sent that tag, and a
# header, when there is one, is the tag rather than the body's.
@pytest.mark.parametrize("header, expected", [({}, 'W/"b"'), ({"ETag": 'W/"h"'}, 'W/"h"')])
def test_the_precondition_falls_back_to_the_body_tag(monkeypatch, header, expected):
    firmware = emulator()
    serve = firmware.resources[firmware.system]
    firmware.resources[firmware.system] = lambda f: (200, dict(serve(f), **{"@odata.etag": 'W/"b"'}), header)
    assert run(monkeypatch, firmware, "boot") == (True, "Off", "")
    assert [call[2].get("if-match") for call in firmware.calls if call[0] == "PATCH"] == [expected]


def advertise_boot(firmware, enabled):
    """The emulator, but advertising which override frequencies it offers."""
    serve = firmware.resources[firmware.system]

    def system(f):
        body = serve(f)
        body["Boot"]["BootSourceOverrideEnabled@Redfish.AllowableValues"] = enabled
        return body

    firmware.resources[firmware.system] = system
    return firmware


# A PATCH that was accepted is not a selection: only the system reporting the
# target, once, or continuously on firmware that offers nothing else, is.
def test_a_boot_selection_is_read_back(monkeypatch):
    kept = emulator()
    kept.script("PATCH", kept.system, (204, None, {}))
    with pytest.raises(ControllerError, match="does not report Cd"):
        run(monkeypatch, kept, "boot")
    assert gets(kept).count(kept.system) == 1 + redfish_control.BOOT_POLLS
    assert kept.pauses == [redfish_control.BOOT_POLL_DELAY] * (redfish_control.BOOT_POLLS - 1)

    assert run(monkeypatch, emulator(), "boot") == (True, "Off", "")
    assert run(monkeypatch, manager_scoped(), "boot") == (True, "Off", "")

    refused = advertise_boot(emulator(), ["Once", "Continuous", "Disabled"])
    with pytest.raises(ControllerError, match="does not report Cd"):
        run(monkeypatch, refused, "boot")

    silent = emulator()

    def applied_without_an_answer(firmware):
        firmware.boot["BootSourceOverrideTarget"] = "Cd"
        return TimeoutError("no answer")

    silent.script("PATCH", silent.system, applied_without_an_answer)
    assert run(monkeypatch, silent, "boot") == (True, "Off", "")


INSUFFICIENT = (403, {"error": {"@Message.ExtendedInfo": [{"MessageId": "Base.1.8.InsufficientPrivilege"}]}}, {})
REFUSED = {
    "reset": ("power-on", "POST", "/Actions/ComputerSystem.Reset"),
    "attach": ("insert", "POST", INSERT),
    "eject": ("eject", "POST", EJECT),
    "boot": ("boot", "PATCH", ""),
}


# A refused privilege is final: nothing is retried, detached, read back or
# waited for, and the line names the status and the controller's message.
@pytest.mark.parametrize("case", sorted(REFUSED))
def test_a_refused_write_fails_at_once(monkeypatch, case):
    operation, method, suffix = REFUSED[case]
    firmware = emulator()
    firmware.image = IMAGE if operation == "eject" else ""
    firmware.script(method, firmware.system + suffix, INSUFFICIENT)
    with pytest.raises(ControllerError) as failure:
        run(monkeypatch, firmware, operation, image="https://server.test:8443/os/other.iso")
    assert "HTTP 403" in str(failure.value) and "InsufficientPrivilege" in str(failure.value)
    assert writes(firmware) == [(method, firmware.system + suffix, firmware.calls[-1][3])]
    assert firmware.calls[-1][0] == method and not firmware.pauses


def advertise_resets(firmware, allowed):
    """The emulator, but advertising other reset types."""
    serve = firmware.resources[firmware.system]

    def system(f):
        body = serve(f)
        body["Actions"]["#ComputerSystem.Reset"]["ResetType@Redfish.AllowableValues"] = allowed
        return body

    firmware.resources[firmware.system] = system
    return firmware


# A stop asks for exactly the kind it names; a controller that does not offer
# it refuses rather than receiving a harsher or a gentler one.
def test_an_unadvertised_stop_is_never_substituted(monkeypatch):
    firmware = dual_view()
    with pytest.raises(ControllerError, match="no GracefulShutdown"):
        run(monkeypatch, firmware, "shutdown")
    graceful_only = advertise_resets(emulator(), ["On", "GracefulShutdown"])
    graceful_only.power = "On"
    with pytest.raises(ControllerError, match="no ForceOff"):
        run(monkeypatch, graceful_only, "power-off")
    assert not writes(firmware) and not writes(graceful_only)


# The reset types an action refers to its metadata for are read before any
# reset is sent, and metadata that cannot be read, or lives elsewhere, refuses.
@pytest.mark.parametrize("info", ["/redfish/v1/Systems/1/ResetActionInfo", "https://elsewhere.test/ResetActionInfo"])
def test_reset_metadata_that_cannot_be_read_refuses(monkeypatch, info):
    firmware = manager_scoped()
    serve = firmware.resources[firmware.system]
    firmware.resources[firmware.system] = lambda f: dict(serve(f), Actions={"#ComputerSystem.Reset": {
        "target": f.system + "/Actions/ComputerSystem.Reset", "@Redfish.ActionInfo": info}})
    firmware.resources[info] = (500, None, {})
    with pytest.raises(ControllerError, match="resetting"):
        run(monkeypatch, firmware, "power-on")
    assert not writes(firmware)
    assert all(call[1].startswith(firmware.origin + "/") for call in firmware.calls)


RESET = "/Actions/ComputerSystem.Reset"


def replace_reset(firmware, action):
    """The emulator, but advertising another reset action."""
    serve = firmware.resources[firmware.system]

    def system(f):
        body = serve(f)
        body["Actions"]["#ComputerSystem.Reset"] = action
        return body

    firmware.resources[firmware.system] = system
    return firmware


# Reset types listed only in the action's metadata constrain the reset as much
# as inline ones: power-on takes ForceOn from them, and a graceful stop they do
# not list is refused before anything is sent.
def test_reset_types_listed_in_readable_metadata_are_the_ones_sent(monkeypatch):
    firmware = emulator()
    info = firmware.system + "/ResetActionInfo"
    replace_reset(firmware, {"target": firmware.system + RESET, "@Redfish.ActionInfo": info})
    firmware.resources[info] = {"Parameters": [{"Name": "ResetType", "AllowableValues": ["ForceOn", "ForceOff"]}]}
    assert run(monkeypatch, firmware, "power-on") == (True, "On", "")
    assert writes(firmware) == [("POST", firmware.system + RESET, {"ResetType": "ForceOn"})]
    mark = len(firmware.calls)
    with pytest.raises(ControllerError, match="no GracefulShutdown, only ForceOn, ForceOff"):
        run(monkeypatch, firmware, "shutdown")
    assert not writes(firmware, mark) and gets(firmware, mark) == [firmware.system, info]


# The reset goes to the target the system advertises, and nowhere else.
def test_a_reset_goes_to_its_advertised_target(monkeypatch):
    firmware = emulator()
    target = firmware.system + "/Actions/Oem/Acme.Reset"
    replace_reset(firmware, {"target": target, "ResetType@Redfish.AllowableValues": ["On", "ForceOff"]})
    standard = firmware.write

    def write(firmware, method, path, payload, headers):
        if path == firmware.system + RESET:
            return 404, None, {}
        return standard(firmware, method, firmware.system + RESET if path == target else path, payload, headers)

    firmware.write = write
    assert run(monkeypatch, firmware, "power-on") == (True, "On", "")
    assert writes(firmware) == [("POST", target, {"ResetType": "On"})]


# A reset nothing answered may still have happened, so the poll decides.
def test_a_reset_nothing_answered_is_decided_by_the_poll(monkeypatch):
    firmware = emulator()

    def applied_without_an_answer(firmware):
        firmware.power = "On"
        return TimeoutError("no answer")

    firmware.script("POST", firmware.system + RESET, applied_without_an_answer)
    assert run(monkeypatch, firmware, "power-on") == (True, "On", "")
    assert writes(firmware) == [("POST", firmware.system + RESET, {"ResetType": "ForceOn"})]


# A poll reads an unreadable answer as not yet the state; only running out of
# attempts fails it.
def test_a_poll_outlasts_an_unreadable_answer(monkeypatch):
    firmware = emulator()
    client = connect(monkeypatch, firmware)
    client.power_state()
    firmware.script("GET", firmware.system, (503, None, {}))
    assert redfish_boot.drive(client, "power-on", 3) == (True, "On", "")
    assert firmware.pauses == [redfish_control.POLL_DELAY]

    firmware = emulator()
    client = connect(monkeypatch, firmware)
    client.power_state()
    firmware.script("GET", firmware.system, *[(503, None, {})] * 3)
    with pytest.raises(ControllerError, match="HTTP 503"):
        redfish_boot.drive(client, "power-on", 3)
    assert gets(firmware).count(firmware.system) == 4


# The boot, insert and eject read-backs are polls too: a device or system that
# answers 503 once has not yet reached the state, and the next answer decides.
@pytest.mark.parametrize("operation", ["boot", "insert", "eject"])
def test_a_read_back_outlasts_an_unreadable_answer(monkeypatch, operation):
    firmware = emulator()
    firmware.image = IMAGE if operation == "eject" else ""
    client = connect(monkeypatch, firmware)
    client.power_state()
    client.inserted()
    unavailable = (503, None, {})
    firmware.script("GET", firmware.system, unavailable)
    firmware.script("GET", firmware.system + "/VirtualMedia/Cd", unavailable)
    assert redfish_boot.drive(client, operation, 3, IMAGE)[0]
    assert len(writes(firmware)) == 1 and len(firmware.pauses) == 1


@pytest.mark.parametrize("required", [True, False])
def test_a_vmm_disconnect_carries_what_its_action_info_requires(monkeypatch, required):
    firmware = manager_scoped()
    firmware.image = IMAGE
    firmware.resources["/redfish/v1/Managers/1/VirtualMedia/CD/VmmControlInfo"] = {"Parameters": [
        {"Name": "Image", "Required": required},
        {"Name": "VmmControlType", "AllowableValues": ["Connect", "Disconnect"]}]}
    assert run(monkeypatch, firmware, "eject") == (True, "Off", "")
    expected = {"VmmControlType": "Disconnect", "Image": IMAGE} if required else {"VmmControlType": "Disconnect"}
    assert [payload for method, path, payload in writes(firmware)] == [expected]


# A vendor extension is used only where no standard action exists, and only
# because its own metadata declared what it accepts.
def test_a_vendor_extension_is_used_only_when_its_metadata_proves_it_fits(monkeypatch):
    firmware = manager_scoped()
    run(monkeypatch, firmware, "insert", image=IMAGE)
    assert writes(firmware)[0][2] == {"Image": IMAGE, "VmmControlType": "Connect"}

    unproved = manager_scoped()
    unproved.resources["/redfish/v1/Managers/1/VirtualMedia/CD/VmmControlInfo"] = {"Parameters": [{"Name": "Image"}]}
    with pytest.raises(ControllerError):
        run(monkeypatch, unproved, "insert", image=IMAGE)
    assert writes(unproved) and not [path for method, path, payload in writes(unproved) if "VmmControl" in path]


# An ejected device is one that reports nothing, and a controller that removes
# the device entirely has ejected it too.
def test_an_eject_is_proved_by_the_device_reporting_nothing(monkeypatch):
    firmware = emulator()
    firmware.image = IMAGE
    assert run(monkeypatch, firmware, "eject") == (True, "Off", "")

    vanishing = emulator()
    vanishing.image = IMAGE
    standard = vanishing.write

    def removing(firmware, method, path, payload, headers):
        answer = standard(firmware, method, path, payload, headers)
        if path.endswith("VirtualMedia.EjectMedia"):
            firmware.resources[firmware.system + "/VirtualMedia/Cd"] = None
        return answer

    vanishing.write = removing
    assert run(monkeypatch, vanishing, "eject") == (True, "Off", "")


# A detach whose answer is an error or never arrives may still have happened,
# so the device read back decides rather than the answer.
@pytest.mark.parametrize("answer", [(500, None, {}), TimeoutError("no answer")], ids=["500", "no answer"])
def test_an_eject_is_decided_by_the_device_not_by_its_answer(monkeypatch, answer):
    firmware = emulator()
    firmware.image = IMAGE

    def detached(firmware):
        firmware.image = ""
        return answer

    firmware.script("POST", firmware.system + EJECT, detached)
    assert run(monkeypatch, firmware, "eject") == (True, "Off", "")
    assert [path for method, path, payload in writes(firmware)] == [firmware.system + EJECT]


def test_an_insert_without_an_image_is_refused(monkeypatch):
    firmware = emulator()
    with pytest.raises(ControllerError, match="insert needs an image"):
        run(monkeypatch, firmware, "insert", image="")
    assert not firmware.calls


# A graceful stop asks the operating system to shut down; forcing the power off
# does not. The two are separate requests, never one with a silent fallback.
def test_a_graceful_stop_and_a_forced_stop_are_different_requests(monkeypatch):
    firmware = emulator()
    for operation in ("shutdown", "power-off"):
        firmware.power = "On"
        assert run(monkeypatch, firmware, operation) == (True, "Off", "")
    assert [payload["ResetType"] for method, path, payload in writes(firmware)] == ["GracefulShutdown", "ForceOff"]


def test_a_machine_already_in_the_requested_state_is_left_alone(monkeypatch):
    firmware = emulator()
    assert run(monkeypatch, firmware, "power-off") == (False, "Off", "")
    assert run(monkeypatch, firmware, "shutdown") == (False, "Off", "")
    firmware.power = "On"
    assert run(monkeypatch, firmware, "power-on") == (False, "On", "")
    assert not writes(firmware)


def test_a_state_that_never_arrives_is_unproved(monkeypatch):
    firmware = emulator()
    firmware.power = "On"
    firmware.script("POST", firmware.system + "/Actions/ComputerSystem.Reset", (204, None, {}))
    with pytest.raises(ControllerError, match="does not report Off"):
        run(monkeypatch, firmware, "shutdown")


# The trust a Machine declares reaches every call this operation makes. A
# controller with an internal certificate authority is reached exactly as the
# declaration says, rather than always verified or never, and a declared
# bundle is the anchor of every one of those calls.
def test_declared_trust_reaches_every_call(monkeypatch):
    firmware = dual_view()
    redfish_boot.drive(connect(monkeypatch, firmware, verify=False), "insert", 3, IMAGE)
    redfish_boot.drive(connect(monkeypatch, firmware, verify=False), "power-off", 3)
    assert len(firmware.trust) == len(firmware.calls) > 1
    assert all(value is False for value in firmware.trust)

    bundled = dual_view()
    redfish_boot.drive(connect(monkeypatch, bundled, ca_data=BUNDLE), "insert", 3, IMAGE)
    redfish_boot.drive(connect(monkeypatch, bundled, ca_data=BUNDLE), "power-off", 3)
    assert len(bundled.bundles) == len(bundled.calls) > 1
    assert all(value is True for value in bundled.trust) and all(value == BUNDLE for value in bundled.bundles)


# The target proof reads identity, the interface inventory and power from one
# system read and never looks for media; the power role's identity check (B3)
# reads its uuid, serial and failures.
@pytest.mark.parametrize("shape", sorted(SHAPES))
def test_the_inspection_contract_is_kept(monkeypatch, shape):
    firmware = SHAPES[shape]()
    observation = redfish_system_inspect.observe(connect(monkeypatch, firmware))
    assert sorted(observation) == ["addresses", "failures", "manufacturer", "model", "power", "serial", "uuid"]
    assert observation["addresses"] and not observation["failures"]
    assert observation["power"] == firmware.power and observation["uuid"]
    assert gets(firmware).count(firmware.system) == 1
    assert not [path for path in gets(firmware) if "/VirtualMedia" in path or "/Managers/" in path]

    unreadable = SHAPES[shape]()
    unreadable.resources[unreadable.system] = (500, None, {})
    observation = redfish_system_inspect.observe(connect(monkeypatch, unreadable))
    assert [observation[key] for key in ("manufacturer", "model", "power", "serial", "uuid")] == [""] * 5
    assert gets(unreadable)[:2] == [unreadable.system, unreadable.system + "/EthernetInterfaces"]
    assert gets(unreadable).count(unreadable.system) == 1

    refused = SHAPES[shape]()
    refused.resources[refused.system + "/EthernetInterfaces"] = {"Members": [
        {"@odata.id": "https://elsewhere.test" + refused.system + "/EthernetInterfaces/1"}]}
    observation = redfish_system_inspect.observe(connect(monkeypatch, refused))
    assert observation["addresses"] == [] and observation["failures"] == ["member[0] could not be read"]
    assert all(call[1].startswith(refused.origin + "/") for call in refused.calls)


# An interface collection that cannot be read in full proves nothing, so a
# member that fails is reported rather than quietly shrinking the address set.
def test_an_unreadable_interface_member_is_a_failure_not_an_absence(monkeypatch):
    firmware = emulator()
    collection = firmware.system + "/EthernetInterfaces"
    firmware.resources[collection] = {"Members": [{"@odata.id": collection + "/1"}, {"@odata.id": collection + "/2"}]}
    firmware.resources[collection + "/1"] = {"MACAddress": "AA-BB-CC-DD-EE-01"}
    firmware.resources[collection + "/2"] = (200, b"[]", {})
    observation = redfish_system_inspect.observe(connect(monkeypatch, firmware))
    assert observation["addresses"] == ["aa:bb:cc:dd:ee:01"] and observation["failures"]

    firmware.resources[collection + "/2"] = {"PermanentMACAddress": "aabbccddee02"}
    observation = redfish_system_inspect.observe(connect(monkeypatch, firmware))
    assert observation["addresses"] == ["aa:bb:cc:dd:ee:01", "aa:bb:cc:dd:ee:02"] and not observation["failures"]


# A failure line is built from what was being done, where and the status, so a
# body the controller wrote and user information in a reference never reach it.
def test_a_failure_line_never_carries_the_credential_or_a_body(monkeypatch):
    marker = "body-marker-7d1f"
    lines = []
    failing = emulator()
    failing.resources[failing.system] = (500, {"error": {"message": marker, "@Message.ExtendedInfo": [
        {"MessageId": "Base.1.8.InternalError", "Message": marker}]}}, {})
    with pytest.raises(ControllerError) as failure:
        read(monkeypatch, failing)
    lines.append(str(failure.value))

    smuggling = emulator()
    smuggling.resources[smuggling.system + "/VirtualMedia"] = {"Members": [
        {"@odata.id": "http://intruder:s3cret@bmc.test:8000" + smuggling.system + "/VirtualMedia/Cd"}]}
    with pytest.raises(ControllerError) as failure:
        read(monkeypatch, smuggling)
    lines.append(str(failure.value))

    endless = emulator()
    endless.resources[endless.system + "/VirtualMedia"] = {"Members": [{"@odata.id": "/" + "x" * 400}]}
    endless.resources["/" + "x" * 400] = (500, None, {})
    with pytest.raises(ControllerError) as failure:
        read(monkeypatch, endless)
    lines.append(str(failure.value))

    assert "Base.1.8.InternalError" in lines[0] and "bmc.test:8000" in lines[1]
    for line in lines:
        assert PASSWORD not in line and marker not in line and "intruder" not in line and "s3cret" not in line
        assert len(line) <= 200 and "\n" not in line


CERTIFICATES = "/VirtualMedia/Cd/Certificates"
DEVICE = "/VirtualMedia/Cd"
IMPORT = {"trust": redfish_control.TRUST_IMPORT, "certificate": SERVER}


def trust_writes(firmware, since=0):
    """The writes that set the device's trust, in order: everything but the
    attach and the detach."""
    return [(method, path[len(firmware.system):], payload) for method, path, payload in writes(firmware, since)
            if not path.endswith((INSERT, EJECT))]


def refused_before_attach(firmware):
    return not [path for _method, path, _payload in writes(firmware) if path.endswith(INSERT)]


# Importing adds the server's certificate to the device's own collection and
# turns verification on, both before the device is asked to fetch anything.
def test_import_certificate_adds_the_server_certificate_and_enables_verification_before_insert(monkeypatch):
    firmware = emulator()
    assert run(monkeypatch, firmware, "insert", image=IMAGE, **IMPORT) == (True, "Off", IMAGE)
    assert trust_writes(firmware) == [
        ("POST", CERTIFICATES, {"CertificateString": SERVER, "CertificateType": "PEM"}),
        ("PATCH", DEVICE, {"VerifyCertificate": True})]
    assert [path for _method, path, _payload in writes(firmware)][-1].endswith(INSERT)
    assert firmware.certificates == {"Default": SERVER} and firmware.verify_certificate is True


# The certificate a server presents may arrive with its chain. The device is
# given the server's own certificate, the first block, and compared by it.
def test_only_the_leaf_is_imported(monkeypatch):
    firmware = emulator()
    run(monkeypatch, firmware, "insert", image=IMAGE, trust=redfish_control.TRUST_IMPORT, certificate=SERVER + ISSUER)
    assert firmware.certificates == {"Default": SERVER}
    mark = len(firmware.calls)
    firmware.image = ""
    run(monkeypatch, firmware, "insert", image=IMAGE, trust=redfish_control.TRUST_IMPORT, certificate=SERVER + ISSUER)
    assert trust_writes(firmware, mark) == []


# A device already holding the certificate, with verification on, is asked
# for nothing more; the certificate is compared by its DER, not its spelling.
def test_an_identical_certificate_is_not_added_twice(monkeypatch):
    firmware = emulator()
    firmware.certificates["Default"], firmware.verify_certificate = SERVER.replace("\n", "\r\n"), True
    assert run(monkeypatch, firmware, "insert", image=IMAGE, **IMPORT) == (True, "Off", IMAGE)
    assert trust_writes(firmware) == []


# Import has no fallback: a device that offers no certificate collection
# refuses, naming the exceptions an operator may declare instead, and nothing
# is attached.
def test_a_member_without_a_certificate_collection_refuses_naming_the_exception(monkeypatch):
    for shape in (emulator, dual_view, xfusion):
        firmware = shape()
        if shape is emulator:
            serve = firmware.resources[firmware.system + DEVICE]
            firmware.resources[firmware.system + DEVICE] = lambda f, serve=serve: {
                key: value for key, value in serve(f).items() if key != "Certificates"}
        with pytest.raises(ControllerError) as failure:
            run(monkeypatch, firmware, "insert", image=IMAGE, **IMPORT)
        line = str(failure.value)
        assert line.endswith(": no Certificates; no fallback: established, or disable-verification on this Machine,"
                             " which private delivery refuses") and len(line) < 200
        assert not writes(firmware)


# A controller whose VerifyCertificate cannot be written answers 400, 405 or
# 501. Import then refuses rather than attaching an image it would fetch
# unverified, and names the exceptions.
@pytest.mark.parametrize("status", [400, 405, 501])
def test_a_read_only_verify_certificate_refuses_import(monkeypatch, status):
    firmware = emulator()
    firmware.script("PATCH", firmware.system + DEVICE, (status, None, {}))
    with pytest.raises(ControllerError) as failure:
        run(monkeypatch, firmware, "insert", image=IMAGE, **IMPORT)
    assert str(failure.value).endswith(
        "HTTP %d, no fallback: established, or disable-verification on this Machine, which private delivery refuses"
        % status)
    assert refused_before_attach(firmware) and firmware.verify_certificate is False


# 401 and 403 mean the account's role lacks the privilege, which is what the
# line says, whether the certificate or the setting was refused.
@pytest.mark.parametrize("method, path", [("POST", CERTIFICATES), ("PATCH", DEVICE)])
def test_a_forbidden_write_names_the_privilege(monkeypatch, method, path):
    firmware = emulator()
    firmware.script(method, firmware.system + path, INSUFFICIENT)
    with pytest.raises(ControllerError, match="HTTP 403, the account's role lacks the privilege"):
        run(monkeypatch, firmware, "insert", image=IMAGE, **IMPORT)
    assert refused_before_attach(firmware) and not firmware.pauses


# A device that holds another certificate and refuses a second has its own
# remedy: nothing here removes a certificate it did not import.
def test_a_different_certificate_present_refuses_with_its_own_remedy(monkeypatch):
    firmware = emulator()
    firmware.certificates["Default"] = STALE
    with pytest.raises(ControllerError) as failure:
        run(monkeypatch, firmware, "insert", image=IMAGE, **IMPORT)
    assert str(failure.value) == (
        "importing: HTTP 409; remove the stale certificate from the controller's virtual-media"
        " certificate collection, or set removeCertificateAfterBoot")
    assert firmware.certificates == {"Default": STALE} and refused_before_attach(firmware)


# Once the device is proved empty, removal deletes the one member that is the
# given certificate and leaves every other alone.
def test_removal_deletes_only_the_given_certificate(monkeypatch):
    firmware = emulator()
    firmware.image, firmware.certificates = IMAGE, {"Other": STALE, "Default": SERVER}
    assert run(monkeypatch, firmware, "eject", remove_certificate=True, certificate=SERVER) == (True, "Off", "")
    assert trust_writes(firmware) == [("DELETE", CERTIFICATES + "/Default", None)]
    assert [path[len(firmware.system):] for _method, path, _payload in writes(firmware)] == [
        EJECT, CERTIFICATES + "/Default"]
    assert firmware.certificates == {"Other": STALE}
    mark = len(firmware.calls)
    assert run(monkeypatch, firmware, "eject", remove_certificate=True, certificate=SERVER) == (False, "Off", "")
    assert not writes(firmware, mark)


# Restoring is a target, not the inverse of this attempt's own write: the eject
# after an interrupted attempt turns verification on whatever left it off, and
# one that finds it on writes nothing. A controller that cannot write the
# setting answers 400, 405 or 501, which a restore passes.
def test_restore_turns_verification_on_after_an_interrupted_attempt(monkeypatch):
    for inserted in (IMAGE, ""):
        firmware = emulator()
        firmware.image = inserted
        assert run(monkeypatch, firmware, "eject", restore_verification=True) == (True, "Off", "")
        assert trust_writes(firmware) == [("PATCH", DEVICE, {"VerifyCertificate": True})]
        assert [path[len(firmware.system):] for _method, path, _payload in writes(firmware)] == (
            [EJECT] if inserted else []) + [DEVICE]
        assert firmware.verify_certificate is True
        mark = len(firmware.calls)
        assert run(monkeypatch, firmware, "eject", restore_verification=True) == (False, "Off", "")
        assert not writes(firmware, mark)
    for status in (400, 405, 501):
        firmware = emulator()
        firmware.script("PATCH", firmware.system + DEVICE, (status, None, {}))
        assert run(monkeypatch, firmware, "eject", restore_verification=True) == (False, "Off", "")


# The trust is settled only once the device is proved empty. An eject whose
# device still presents media fails with the device's trust as it found it,
# because the controller may still be fetching under that trust.
def test_an_eject_that_is_not_proved_settles_nothing(monkeypatch):
    firmware = emulator()
    firmware.image, firmware.certificates = IMAGE, {"Default": SERVER}
    firmware.script("POST", firmware.system + EJECT, (204, None, {}))
    with pytest.raises(ControllerError, match="the device still presents media"):
        run(monkeypatch, firmware, "eject", restore_verification=True, remove_certificate=True, certificate=SERVER)
    assert trust_writes(firmware) == [] and [path for _method, path, _payload in writes(firmware)] == [
        firmware.system + EJECT]
    assert firmware.certificates == {"Default": SERVER} and firmware.verify_certificate is False


# A write the device answers is not the evidence that it took. A device that
# accepts VerifyCertificate and still reports the other value, as a
# SecurityPolicy lock or firmware ignoring the property would, is refused: the
# insert before anything is attached, and the eject's restore alike.
def test_verification_is_read_back_rather_than_taken_from_the_answer(monkeypatch):
    firmware = emulator()
    firmware.script("PATCH", firmware.system + DEVICE, (204, None, {}))
    with pytest.raises(ControllerError) as failure:
        run(monkeypatch, firmware, "insert", image=IMAGE, **IMPORT)
    assert str(failure.value).endswith("HTTP 200, " + redfish_control.NO_IMPORT)
    assert refused_before_attach(firmware) and firmware.verify_certificate is False

    firmware = emulator()
    firmware.verify_certificate = True
    firmware.script("PATCH", firmware.system + DEVICE, (204, None, {}))
    with pytest.raises(ControllerError) as failure:
        run(monkeypatch, firmware, "insert", image=IMAGE, trust=redfish_control.TRUST_DISABLED)
    assert str(failure.value).endswith("HTTP 200, " + redfish_control.NO_DISABLE)
    assert refused_before_attach(firmware) and firmware.verify_certificate is True

    firmware = emulator()
    firmware.image = IMAGE
    firmware.script("PATCH", firmware.system + DEVICE, (204, None, {}))
    with pytest.raises(ControllerError, match="HTTP 200, the device does not report VerifyCertificate true"):
        run(monkeypatch, firmware, "eject", restore_verification=True)
    assert firmware.image == "" and firmware.verify_certificate is False


# The VerifyCertificate write carries the device's own entity tag, from its
# ETag header or its body, and a 412 is retried once with `*`; a device that
# carries no tag is sent no precondition. The insert's import and the eject's
# restore are the two writes of that setting that must reach the device.
@pytest.mark.parametrize("header, body, expected", [
    ({"ETag": 'W/"m"'}, {}, ['W/"m"', "*"]),
    ({}, {"@odata.etag": 'W/"m"'}, ['W/"m"', "*"]),
    ({}, {}, [None]),
], ids=["header", "body", "none"])
def test_the_verification_write_carries_the_device_precondition(monkeypatch, header, body, expected):
    for operation, image, trust in (("insert", IMAGE, IMPORT), ("eject", "", {"restore_verification": True})):
        firmware = emulator()
        serve = firmware.resources[firmware.system + DEVICE]
        firmware.resources[firmware.system + DEVICE] = lambda f, serve=serve: (200, dict(serve(f), **body), header)
        if len(expected) > 1:
            firmware.script("PATCH", firmware.system + DEVICE, (412, None, {}))
        assert run(monkeypatch, firmware, operation, image=image, **trust) == (True, "Off", image)
        assert [call[2].get("if-match") for call in firmware.calls if call[0] == "PATCH"] == expected
        assert firmware.verify_certificate is True and firmware.image == image


# Disabling writes only a setting that reads on, and a controller that refuses
# the write leaves verification on, so the insert stops there, naming the two
# trusts that need no write.
def test_disable_verification_writes_only_when_verification_is_on(monkeypatch):
    disabling = {"trust": redfish_control.TRUST_DISABLED}
    firmware = emulator()
    run(monkeypatch, firmware, "insert", image=IMAGE, **disabling)
    assert trust_writes(firmware) == []
    firmware = emulator()
    firmware.verify_certificate = True
    run(monkeypatch, firmware, "insert", image=IMAGE, **disabling)
    assert trust_writes(firmware) == [("PATCH", DEVICE, {"VerifyCertificate": False})]
    firmware = emulator()
    firmware.verify_certificate = True
    firmware.script("PATCH", firmware.system + DEVICE, (405, None, {}))
    with pytest.raises(ControllerError, match="verification stays on; use import-certificate or established"):
        run(monkeypatch, firmware, "insert", image=IMAGE, **disabling)
    assert refused_before_attach(firmware) and firmware.verify_certificate is True


# established is a trust the controller already holds, so neither the insert
# nor the eject asks anything of the device, and those are the defaults.
def test_established_changes_no_setting(monkeypatch):
    firmware = emulator()
    firmware.certificates["Default"] = STALE
    run(monkeypatch, firmware, "insert", image=IMAGE, trust=redfish_control.TRUST_ESTABLISHED, certificate=SERVER)
    run(monkeypatch, firmware, "eject")
    run(monkeypatch, firmware, "insert", image=IMAGE)
    assert trust_writes(firmware) == [] and len(writes(firmware)) == 3
    assert not [path for path in gets(firmware) if path.endswith(CERTIFICATES)]


# The trust is set once, before the first attach, and never inside the retry:
# an attach that fails and is retried does not import or write again.
def test_trust_is_set_once_outside_the_insert_retry(monkeypatch):
    firmware = emulator()
    attach = firmware.system + INSERT
    firmware.script("POST", attach, (500, None, {}), (500, None, {}))
    assert run(monkeypatch, firmware, "insert", image=IMAGE, **IMPORT) == (True, "Off", IMAGE)
    assert trust_writes(firmware) == [
        ("POST", CERTIFICATES, {"CertificateString": SERVER, "CertificateType": "PEM"}),
        ("PATCH", DEVICE, {"VerifyCertificate": True})]
    assert [path for _method, path, _payload in writes(firmware)].count(attach) == 3
    assert gets(firmware).count(firmware.system + CERTIFICATES) == 1


# A collection is read in full before anything is added to it, so one listing
# more members than the bound refuses rather than being read in part.
def test_a_certificate_collection_beyond_its_bound_refuses(monkeypatch):
    firmware = emulator()
    for index in range(redfish_control.CERTIFICATE_MEMBERS + 1):
        firmware.certificates["c%d" % index] = certificate(b"member %d" % index)
    with pytest.raises(ControllerError, match="lists 9 certificates, more than the 8 read"):
        run(monkeypatch, firmware, "insert", image=IMAGE, **IMPORT)
    assert not writes(firmware)
    assert not [path for path in gets(firmware) if path.startswith(firmware.system + CERTIFICATES + "/")]
    firmware.certificates.pop("c0")
    firmware.certificates["c8"] = SERVER
    assert run(monkeypatch, firmware, "insert", image=IMAGE, **IMPORT) == (True, "Off", IMAGE)
    assert len([path for path in gets(firmware) if path.startswith(firmware.system + CERTIFICATES + "/")]) == 8
    assert trust_writes(firmware) == [("PATCH", DEVICE, {"VerifyCertificate": True})]


# The new options ask nothing by default: an insert trusts what is established
# and an eject settles nothing, so an existing consumer is driven as before.
def test_the_trust_options_default_to_no_action(monkeypatch):
    monkeypatch.setattr(redfish_boot, "AnsibleModule", built)
    with pytest.raises(Built) as spec:
        redfish_boot.main()
    options = spec.value.args[0]["argument_spec"]
    assert options["trust"]["default"] == "established" and options["certificate"]["default"] == ""
    assert options["restore_verification"]["default"] is False and options["remove_certificate"]["default"] is False
    assert options["ca_data"] == {"type": "str", "default": ""}
    assert options["private_delivery"]["default"] is False
    with pytest.raises(ControllerError, match="needs the server's certificate"):
        run(monkeypatch, emulator(), "insert", image=IMAGE, trust=redfish_control.TRUST_IMPORT)
    with pytest.raises(ControllerError, match="needs that certificate"):
        run(monkeypatch, emulator(), "eject", remove_certificate=True)


# Every module hands the declared bundle to the one client it builds, and a
# bundle that cannot be used fails the module through its own failure rather
# than as a traceback.
@pytest.mark.parametrize("module, extra", [
    (redfish_boot, {"operation": "boot", "image": None, "target": "Cd", "attempts": 3, "trust": "established",
                    "certificate": "", "restore_verification": False, "remove_certificate": False,
                    "private_delivery": False}),
    (redfish_system_read, {"media": False}),
    (redfish_system_inspect, {}),
])
def test_the_bundle_reaches_the_client_from_every_module(monkeypatch, module, extra):
    firmware = emulator()
    params = dict({"endpoint": firmware.endpoint, "user": "operator", "password": PASSWORD, "verify": True,
                   "ca_data": BUNDLE}, **extra)
    built_with = []
    monkeypatch.setattr(redfish_control, "_opener", firmware.opener)
    monkeypatch.setattr(redfish_control, "_trust_context", lambda verify, ca_data: built_with.append(ca_data))
    ending = Module(params)
    monkeypatch.setattr(module, "AnsibleModule", ending.build)
    module.main()
    assert ending.arguments["argument_spec"]["ca_data"] == {"type": "str", "default": ""}
    assert ending.ended[0] == "exit" and built_with == [BUNDLE]
    assert firmware.bundles and all(value == BUNDLE for value in firmware.bundles)

    monkeypatch.undo()
    refusing = Module(dict(params, verify=False))
    monkeypatch.setattr(module, "AnsibleModule", refusing.build)
    monkeypatch.setattr(redfish_control, "_opener", firmware.opener)
    module.main()
    assert refusing.ended[0] == "fail" and "requires verification" in refusing.ended[1]["msg"]


# An inspection is the proof before an erasure. A controller whose certificate
# the declared trust refuses must not read as a system that could not be read,
# which reports an empty identity; the inspection fails instead.
def test_an_inspection_fails_on_an_unverified_certificate(monkeypatch):
    firmware = emulator()
    refused = urllib.error.URLError(ssl.SSLCertVerificationError(1, "certificate verify failed"))
    firmware.resources[firmware.system] = refused
    with pytest.raises(redfish_control.UnverifiedCertificate):
        redfish_system_inspect.observe(connect(monkeypatch, firmware))

    class Refusing:
        """A client whose identity read is refused and whose inventory reads."""

        def identity(self):
            raise redfish_control.UnverifiedCertificate("reading /redfish/v1/Systems/1: refused")

        def power_state(self):
            return "Off"

        def hardware_addresses(self):
            return ["aa:bb:cc:dd:ee:01"], []

    with pytest.raises(redfish_control.UnverifiedCertificate):
        redfish_system_inspect.observe(Refusing())
    ending = Module({"endpoint": firmware.endpoint, "user": "operator", "password": PASSWORD, "verify": True,
                     "ca_data": ""})
    monkeypatch.setattr(redfish_system_inspect, "AnsibleModule", ending.build)
    redfish_system_inspect.main()
    assert ending.ended[0] == "fail"
    assert ending.ended[1]["msg"].endswith("did not verify against the system trust store")
