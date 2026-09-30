"""Every Redfish effect, and every read a consumer decides from, goes through one client.

Three firmware shapes answer through urllib itself, so every test runs through
the client's single request path: the pinned emulator, which keeps virtual
media under the system and attaches synchronously; a manager-scoped controller
that attaches through a vendor extension and reports the outcome in a task; and
one that exposes the same device under both views and demands a precondition on
a write. No test names a vendor in the client.
"""

from __future__ import annotations

import ast
import http.client
import io
import json
import pathlib
import re
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
        self.scripts, self.calls, self.trust, self.pauses = {}, [], [], []
        self.power, self.image, self.pending = "Off", "", ""
        self.boot = {"BootSourceOverrideEnabled": "Continuous", "BootSourceOverrideTarget": "Hdd"}

    def opener(self, verify=True):
        self.trust.append(verify)
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
        if resource is None:
            return 404, {"error": {"@Message.ExtendedInfo": [{"MessageId": "Base.1.8.ResourceMissingAtURI"}]}}, {}
        if isinstance(resource, (tuple, BaseException)):
            return resource
        return 200, resource, {}


def gets(firmware, since=0):
    return [urlsplit(call[1]).path for call in firmware.calls[since:] if call[0] == "GET"]


def writes(firmware, since=0):
    return [(call[0], urlsplit(call[1]).path, call[3]) for call in firmware.calls[since:] if call[0] != "GET"]


def connect(monkeypatch, firmware, verify=True):
    monkeypatch.setattr(redfish_control, "_opener", firmware.opener)
    client = redfish_control.Client(firmware.endpoint, "operator", PASSWORD, verify=verify)
    client.sleep = firmware.pauses.append
    return client


def run(monkeypatch, firmware, operation, attempts=3, image="", target="Cd"):
    """One module invocation: a fresh client, as main() builds one."""
    return redfish_boot.drive(connect(monkeypatch, firmware), operation, attempts, image, target)


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
    """
    system, manager = "/redfish/v1/Systems/" + UUID, "/redfish/v1/Managers/" + UUID
    media, nic = system + "/VirtualMedia", system + "/EthernetInterfaces/52:54:00:aa:bb:01"

    def write(firmware, method, path, payload, headers):
        if path == media + "/Cd/Actions/VirtualMedia.InsertMedia":
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

    return Firmware("http://bmc.test:8000" + system, {
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
            "Actions": {
                "#VirtualMedia.EjectMedia": {"target": media + "/Cd/Actions/VirtualMedia.EjectMedia"},
                "#VirtualMedia.InsertMedia": {"target": media + "/Cd/Actions/VirtualMedia.InsertMedia"},
                "Oem": {}}},
    }, write)


def manager_scoped():
    """Virtual media only under the manager, attached by a vendor extension.

    The shape .agents/knowledge/redfish-physical-bmc.md records: the system's
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


SHAPES = {"emulator": emulator, "manager-scoped": manager_scoped, "dual-view": dual_view}
CONNECT = {"Image": IMAGE, "Inserted": True, "WriteProtected": True, "TransferProtocolType": "HTTPS"}
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
}


# Each operation a consumer runs, on each shape, is one client call whose
# writes are exactly the ones the controller advertises, and whose report is
# what the controller said last.
@pytest.mark.parametrize("shape", sorted(SHAPES))
def test_every_operation_goes_through_the_client(monkeypatch, shape):
    firmware, expected = SHAPES[shape](), EXPECTED[shape]
    power = firmware.power

    assert read(monkeypatch, firmware) == (power, "")
    assert not writes(firmware) and gets(firmware).count(firmware.system) == 1

    mark = len(firmware.calls)
    assert run(monkeypatch, firmware, "insert", image=IMAGE) == (True, power, IMAGE)
    [(method, path, payload)] = writes(firmware, mark)
    assert (method, path.endswith(expected["attach"][0]), payload) == ("POST", True, expected["attach"][1])
    if shape == "manager-scoped":
        assert "/redfish/v1/TaskService/Tasks/9" in gets(firmware, mark)
    mark = len(firmware.calls)
    assert run(monkeypatch, firmware, "insert", image=IMAGE) == (False, power, IMAGE)
    assert read(monkeypatch, firmware) == (power, IMAGE)
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
                         "verify": True, "media": True})
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


# A second attach is sent only to a device proved empty; one that could not be
# released ends the insert, naming both what failed and that it stayed attached.
def test_a_retry_starts_from_an_empty_device(monkeypatch):
    firmware = emulator()
    firmware.image = "https://server.test:8443/os/old.iso"
    firmware.script("POST", firmware.system + INSERT, (500, None, {}))
    firmware.script("POST", firmware.system + EJECT, (204, None, {}))
    with pytest.raises(ControllerError) as failure:
        run(monkeypatch, firmware, "insert", image=IMAGE)
    assert "HTTP 500" in str(failure.value) and "not released" in str(failure.value)
    assert [path for method, path, payload in writes(firmware)] == [firmware.system + INSERT, firmware.system + EJECT]


# A controller that echoes the image in another spelling of the same URL, a
# host in capitals or a default port dropped, presents it, so nothing is sent.
@pytest.mark.parametrize("image, echoed", [
    (IMAGE, "https://SERVER.test:8443/os/m/install.iso"),
    ("https://server.test:443/os/m/install.iso", "https://server.test/os/m/install.iso"),
], ids=["host case", "default port"])
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
# declaration says, rather than always verified or never.
def test_declared_trust_reaches_every_call(monkeypatch):
    firmware = dual_view()
    redfish_boot.drive(connect(monkeypatch, firmware, verify=False), "insert", 3, IMAGE)
    redfish_boot.drive(connect(monkeypatch, firmware, verify=False), "power-off", 3)
    assert len(firmware.trust) == len(firmware.calls) > 1
    assert all(value is False for value in firmware.trust)


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
