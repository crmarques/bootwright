"""BMC drive inventory first; otherwise the controller pulls from a RAM boot."""

from email.message import Message
import ssl
from types import SimpleNamespace
from unittest.mock import Mock

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import canonical_json
from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_bmc as bmc
from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_controller as controller
from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_media as media
from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_worker as worker


def drive(serial="EXAMPLE-OS", size=480103981056, **changes):
    body = {
        "@odata.type": "#Drive.v1_9_0.Drive", "Status": {"State": "Enabled"}, "Protocol": "SATA",
        "SerialNumber": serial, "CapacityBytes": size, "Model": "EXAMPLE SSD", "Links": {"Volumes": []},
    }
    body.update(changes)
    return body


class Controller:
    """Answers reads by path only; every write is a test failure."""

    def __init__(self, drives, volumes=(), storage=True):
        self.resources = {"/Storage": {"Members": [{"@odata.id": "/Storage/1"}]}}
        self.resources["/Storage/1"] = {
            "Drives": [{"@odata.id": "/Drives/%d" % index} for index in range(len(drives))],
            "Volumes": {"@odata.id": "/Storage/1/Volumes"},
        }
        self.resources["/Storage/1/Volumes"] = {"Members": [{"@odata.id": "/Volumes/%d" % index} for index in range(len(volumes))]}
        for index, body in enumerate(drives):
            self.resources["/Drives/%d" % index] = body
        self.storage = storage

    def system(self):
        return {"Storage": {"@odata.id": "/Storage"}} if self.storage else {}

    def _read(self, reference, doing):
        if reference not in self.resources:
            raise bmc.redfish_control.ControllerError("missing " + reference)
        return self.resources[reference], {}

    def _own(self, reference, doing):
        return reference


def test_a_complete_plain_drive_inventory_needs_no_boot():
    disks = bmc.inventory(Controller([drive(), drive("EXAMPLE-DATA", 960197124096, Protocol="NVMe", Model=" DATA ")]))
    assert disks == [
        {"controllerPCIAddress": "", "fixed": True, "hidden": False, "model": "EXAMPLE SSD", "serialNumber": "EXAMPLE-OS",
         "sizeBytes": 480103981056, "transport": "sata", "whole": True, "writable": True},
        {"controllerPCIAddress": "", "fixed": True, "hidden": False, "model": "DATA", "serialNumber": "EXAMPLE-DATA",
         "sizeBytes": 960197124096, "transport": "nvme", "whole": True, "writable": True},
    ]
    assert all("wwn" not in disk for disk in disks)


def test_an_absent_bay_is_skipped_not_reported():
    assert len(bmc.inventory(Controller([drive(), drive("", None, Status={"State": "Absent"})]))) == 1


@pytest.mark.parametrize("change", [
    {"SerialNumber": ""}, {"SerialNumber": "bad serial"}, {"SerialNumber": None},
    {"CapacityBytes": None}, {"CapacityBytes": True}, {"CapacityBytes": 4097}, {"CapacityBytes": 1 << 50},
    {"Protocol": "FC"}, {"Protocol": None}, {"Status": {"State": "UnavailableOffline"}}, {"Status": None},
    {"Links": {"Volumes": [{"@odata.id": "/Volumes/0"}]}}, {"@odata.type": "#Volume.v1_0_0.Volume"},
])
def test_an_unprovable_drive_sends_discovery_to_the_boot(change):
    assert bmc.inventory(Controller([drive(), drive("EXAMPLE-DATA", **change)])) is None


@pytest.mark.parametrize("controller_", [
    Controller([]), Controller([drive()], volumes=("raid",)), Controller([drive()], storage=False),
])
def test_no_drives_logical_volumes_or_no_storage_resource_boot(controller_):
    assert bmc.inventory(controller_) is None


def test_a_paged_or_unreadable_inventory_boots():
    paged = Controller([drive()])
    paged.resources["/Storage"]["Members@odata.nextLink"] = "/Storage?page=2"
    assert bmc.inventory(paged) is None
    missing = Controller([drive()])
    del missing.resources["/Drives/0"]
    assert bmc.inventory(missing) is None


def custody(tmp_path, monkeypatch, inventory="automatic"):
    instance = controller.Controller.__new__(controller.Controller)
    instance.root = tmp_path
    instance.binding = {"nonce": "a" * 64}
    instance.digest = "b" * 64
    instance.request = {"inventory": inventory, "network": {"address": "192.0.2.10"}, "deadlineSeconds": 1800,
                        "private": {"path": str(tmp_path / "served")}}
    instance.lock = None
    monkeypatch.setattr(controller.os, "fsync", Mock())
    monkeypatch.setattr(controller.records, "read", lambda path, limit=32768: path.read_bytes())

    def write(path, data):
        with path.open("xb") as stream:
            stream.write(data)

    monkeypatch.setattr(controller.records, "write", write)
    return instance


def test_a_bmc_report_is_bound_durable_and_names_its_source(tmp_path, monkeypatch):
    instance = custody(tmp_path, monkeypatch)
    client = Controller([drive()])
    instance.client = lambda: client
    instance.target = Mock(return_value="Off")
    data = instance.read_bmc()
    report = instance.validate_report(data)
    assert report["source"] == "bmc" and report["binding"] == instance.binding
    assert (tmp_path / "report.json").read_bytes() == data
    instance.target.assert_called_once_with(client)


def test_an_unqualified_bmc_inventory_writes_nothing(tmp_path, monkeypatch):
    instance = custody(tmp_path, monkeypatch)
    instance.client = lambda: Controller([])
    instance.target = Mock(return_value="Off")
    assert instance.read_bmc() is None
    assert not (tmp_path / "report.json").exists()


def test_the_bmc_path_seals_completion_without_any_boot_effect(tmp_path, monkeypatch):
    instance = custody(tmp_path, monkeypatch)
    instance.prepare = Mock()
    instance.evidence = Mock(return_value={"cleaned": False})
    instance.report = Mock(return_value=None)
    instance.read_bmc = Mock(return_value=b"qualified")
    instance.finish = Mock(return_value={"completed": True, "cleaned": True})
    for effect in ("start", "publish", "receive", "cleanup", "mark_boot"):
        setattr(instance, effect, Mock(side_effect=AssertionError(effect)))
    assert instance.apply()["completed"]
    instance.finish.assert_called_once_with(True)
    assert not instance.booted()


def test_boot_is_marked_durably_before_any_publication_or_bmc_write(tmp_path, monkeypatch):
    instance = custody(tmp_path, monkeypatch)
    order = []
    client = Mock()
    client.inserted.return_value = ""
    client.insert.side_effect = lambda *args, **kwargs: order.append("insert")
    instance.client = lambda: client
    instance.target = Mock(return_value="Off")
    instance.publication = lambda: (tmp_path / "private", "https://example.test/owned.iso")
    instance.material = {"artifactCertificate": str(tmp_path / "certificate.pem")}
    (tmp_path / "certificate.pem").write_bytes(b"certificate fixture")
    instance.request["controller"] = {"virtualMedia": {"trust": "established", "enableVerification": False}}
    instance.prepare_tls = lambda: order.append("tls")
    instance.publish = lambda: order.append("publish:" + str(instance.booted()))
    monkeypatch.setattr(controller, "listener_probe", Mock())
    instance.start()
    assert order == ["tls", "publish:True", "insert"]
    assert (tmp_path / "boot.json").exists()


def test_destroy_before_any_boot_stops_and_ejects_nothing(tmp_path, monkeypatch):
    instance = custody(tmp_path, monkeypatch)
    (tmp_path / "custody.json").write_bytes(b"fixture")
    instance.evidence = Mock(return_value={"cleaned": False})
    instance.cleanup = Mock(side_effect=AssertionError("stopped a machine discovery never booted"))
    instance.finish = Mock(return_value={"completed": False, "cleaned": True})
    assert instance.destroy()["cleaned"]
    instance.finish.assert_called_once_with(False)


class Connection:
    answers = []

    def __init__(self, host, leaf, port, context, timeout):
        self.host, self.port = host, port
        self.sent = None

    def connect(self):
        answer = self.answers.pop(0)
        if isinstance(answer, BaseException):
            raise answer
        self.answer = answer

    def request(self, method, path, headers):
        self.sent = (method, path, headers)

    def getresponse(self):
        status, body = self.answer
        return SimpleNamespace(status=status, read=lambda limit: body[:limit])

    def close(self):
        pass


def pull_fixture(tmp_path, monkeypatch, answers):
    instance = custody(tmp_path, monkeypatch)
    (tmp_path / "tls").mkdir()
    (tmp_path / "tls/server.pem").write_bytes(b"pinned certificate")
    instance.load = lambda: {"reportToken": "c" * 64, "token": "d" * 64}
    monkeypatch.setattr(controller.tls_trust, "context", lambda certificate, mode: (Mock(), b"leaf"))
    Connection.answers = list(answers)
    monkeypatch.setattr(controller.tls_trust, "CertificateConnection", Connection)
    return instance


def test_the_controller_pulls_from_the_declared_address_and_port(tmp_path, monkeypatch):
    seen = []
    original = Connection.request

    def request(self, method, path, headers):
        seen.append((self.host, self.port, method, path, headers["Authorization"]))
        original(self, method, path, headers)

    monkeypatch.setattr(Connection, "request", request)
    instance = pull_fixture(tmp_path, monkeypatch, [(200, b"report")])
    assert instance.pull() == b"report"
    assert seen == [("192.0.2.10", 9443, "GET", "/report", "Bearer " + "c" * 64)]


@pytest.mark.parametrize("answer,expected", [
    (ConnectionRefusedError(), None), (TimeoutError(), None), (OSError(), None),
])
def test_no_answer_yet_is_not_a_failure(tmp_path, monkeypatch, answer, expected):
    assert pull_fixture(tmp_path, monkeypatch, [answer]).pull() is expected


@pytest.mark.parametrize("answer,message", [
    (ssl.SSLCertVerificationError(), "foreign service"), ((403, b""), "foreign service"),
    ((409, worker.REFUSED), "inventory refused"), ((200, b"x" * (controller.MAX_REPORT + 1)), "report size"),
])
def test_a_definite_answer_ends_the_wait_with_its_cause(tmp_path, monkeypatch, answer, message):
    with pytest.raises(ValueError, match=message):
        pull_fixture(tmp_path, monkeypatch, [answer]).pull()


def test_waiting_rechecks_ownership_and_stops_when_the_machine_does(tmp_path, monkeypatch):
    instance = custody(tmp_path, monkeypatch)
    instance.report = Mock(return_value=None)
    instance.publication = lambda: (tmp_path, "https://example.test/owned.iso")
    instance.pull = Mock(return_value=None)
    clock = iter(range(0, 10000, 30))
    monkeypatch.setattr(controller.time, "monotonic", lambda: next(clock))
    monkeypatch.setattr(controller.time, "sleep", Mock())
    client = Mock()
    client.inserted.return_value = "https://example.test/owned.iso"
    instance.client = lambda: client
    instance.target = Mock(side_effect=["On", "Off"])
    with pytest.raises(ValueError, match="stopped before its inventory"):
        instance.receive()
    client.power.assert_not_called()


def test_waiting_has_a_total_deadline(tmp_path, monkeypatch):
    instance = custody(tmp_path, monkeypatch)
    instance.report = Mock(return_value=None)
    instance.publication = lambda: (tmp_path, "owned")
    instance.pull = Mock(return_value=None)
    instance.owned_running = Mock()
    clock = iter(range(0, 100000, 40))
    monkeypatch.setattr(controller.time, "monotonic", lambda: next(clock))
    monkeypatch.setattr(controller.time, "sleep", Mock())
    with pytest.raises(ValueError, match="report deadline"):
        instance.receive()


def test_a_pulled_report_must_be_a_linux_report_for_this_attempt(tmp_path, monkeypatch):
    instance = custody(tmp_path, monkeypatch)
    instance.report = Mock(return_value=None)
    instance.publication = lambda: (tmp_path, "owned")
    linux = canonical_json.canonical({"version": "machine-disk-inventory-v1", "source": "linux", "binding": instance.binding, "disks": [{}]})
    instance.pull = Mock(return_value=linux)
    assert instance.receive() == linux and (tmp_path / "report.json").read_bytes() == linux
    (tmp_path / "report.json").unlink()
    instance.pull = Mock(return_value=linux.replace(b'"linux"', b'"bmc"'))
    with pytest.raises(ValueError, match="foreign discovery report"):
        instance.receive()
    assert not (tmp_path / "report.json").exists()


def test_cleanup_forces_off_only_its_own_boot(tmp_path, monkeypatch):
    instance = custody(tmp_path, monkeypatch)
    instance.publication = lambda: (tmp_path / "private", "https://example.test/owned.iso")
    instance.material = {"artifactCertificate": str(tmp_path / "certificate.pem")}
    (tmp_path / "certificate.pem").write_bytes(b"certificate fixture")
    instance.request["controller"] = {"virtualMedia": {"restoreVerification": False, "removeCertificate": False}}
    monkeypatch.setattr(controller.time, "sleep", Mock())
    monkeypatch.setattr(controller.records, "directory", Mock())
    client = Mock()
    client.inserted.return_value = "https://example.test/owned.iso"
    instance.client = lambda: client
    instance.target = Mock(side_effect=["On", "Off"])
    instance.cleanup()
    client.power.assert_called_once_with("ForceOff", "Off")
    client.eject.assert_called_once()
    client.power.reset_mock()
    instance.target = Mock(return_value="On")
    client.inserted.return_value = "https://example.test/another.iso"
    with pytest.raises(ValueError, match="lost ownership"):
        instance.cleanup()
    client.power.assert_not_called()


def handler(token, path="/report", result=(200, b"report")):
    instance = worker.Handler.__new__(worker.Handler)
    instance.path = path
    instance.headers = Message()
    if token is not None:
        instance.headers["Authorization"] = "Bearer " + token
    instance.server = SimpleNamespace(token="c" * 64, result=result)
    instance.answer = Mock()
    instance.do_GET()
    return instance.answer


def test_the_worker_answers_only_the_authenticated_report_path():
    handler("c" * 64).assert_called_once_with(200, b"report")
    handler("c" * 64, result=(409, worker.REFUSED)).assert_called_once_with(409, worker.REFUSED)
    for token, path in (("d" * 64, "/report"), (None, "/report"), ("c" * 64, "/ack"), ("c" * 64, "/report/../x")):
        handler(token, path).assert_called_once_with(403)


def test_a_failed_inventory_serves_a_fixed_refusal_without_its_cause(monkeypatch):
    served = []
    monkeypatch.setattr(worker, "prepare_network", lambda: {"network": {"address": "192.0.2.10"}})
    monkeypatch.setattr(worker, "collect", Mock(side_effect=ValueError("SECRET serial ABC123")))
    monkeypatch.setattr(worker, "serve", lambda request, result: served.append(result))
    monkeypatch.setattr(worker, "phase", Mock())
    worker.run()
    assert served == [(409, worker.REFUSED)]


def test_the_worker_listens_on_its_declared_address_and_fixed_port(monkeypatch):
    bound = []

    class Server:
        def __init__(self, address, handler_class):
            bound.append(address)

        def __enter__(self):
            return self

        def __exit__(self, *args):
            return False

        def handle_request(self):
            raise KeyboardInterrupt

    monkeypatch.setattr(worker, "Server", Server)
    monkeypatch.setattr(worker.ssl, "SSLContext", Mock())
    with pytest.raises(KeyboardInterrupt):
        worker.serve({"network": {"address": "192.0.2.10"}, "reportToken": "c" * 64}, (200, b"report"))
    assert bound == [("192.0.2.10", 9443)]


def test_the_boot_tree_keeps_only_what_the_kernel_and_loaders_read(monkeypatch):
    listings = {
        "/": b"'/' '/.discinfo' '/EFI' '/images' '/isolinux' '/BaseOS' '/AppStream' '/EULA'",
        "/images": b"'/images' '/images/efiboot.img' '/images/install.img' '/images/pxeboot'",
    }
    monkeypatch.setattr(media, "native", lambda argv, **kwargs: listings[argv[argv.index("-find") + 1]])
    assert media.outside_boot_tree("/held") == ["/BaseOS", "/AppStream", "/EULA", "/images/install.img"]
    listings["/images"] = b"'/images' '/images/efiboot.img' '/images/pxeboot'"
    with pytest.raises(ValueError, match="initrd layout"):
        media.outside_boot_tree("/held")


def test_controller_and_worker_share_one_port_and_refusal():
    assert controller.PORT == worker.PORT and controller.REFUSED == worker.REFUSED
