"""The guest RHSM worker registers once, owns only what it registered and never reflects service text.

The D-Bus surface the fake models is subscription-manager's
src/rhsmlib/dbus/objects/register.py (RegisterServer.Start/Stop and the
private Register.RegisterWithActivationKeys returning the consumer as JSON) and
src/rhsmlib/dbus/util.py, which carries every service exception as a D-Bus
error whose message is JSON {"exception", "severity", "message"}. The collision
text is Katello's app/services/katello/registration_manager.rb.
"""

from __future__ import annotations

import ast
import builtins
import io
import json
import os
import re
import sys
import types

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import managedos_rhsm_worker as worker

DIGEST = "0123456789abcdef" * 4
OTHER = "fedcba9876543210" * 4
U1 = "11111111-2222-4333-8444-555555555555"
U2 = "22222222-3333-4444-8555-666666666666"
UNEW = "33333333-4444-4555-8666-777777777777"
ORG = "org-sentinel-7f3a"
KEY = "key-sentinel-91c2"
SM = worker.SUBSCRIPTION_MANAGER


class DBusException(Exception):
    def __init__(self, message="", name="org.freedesktop.DBus.Python.Exception"):
        super().__init__(message)
        self.message = message
        self.name = name

    def get_dbus_name(self):
        return self.name

    def get_dbus_message(self):
        return self.message


class Array(list):
    def __init__(self, values, signature):
        super().__init__(values)
        self.signature = signature


class Dictionary(dict):
    def __init__(self, values, signature):
        super().__init__(values)
        self.signature = signature


class FakeBus:
    """A recording python3-dbus module; register writes the consumer certificate as the service would."""

    def __init__(self, cert, consumer=UNEW, error=None, observe=None):
        self.calls = []
        self.cert = cert
        self.consumer = consumer
        self.error = error
        self.observe = observe
        self.exceptions = types.SimpleNamespace(DBusException=DBusException)
        self.connection = types.SimpleNamespace(Connection=self.private)
        self.Array = Array
        self.Dictionary = Dictionary

    def SystemBus(self):
        self.calls.append(("SystemBus",))
        return self

    def private(self, address):
        self.calls.append(("Connection", address))
        return types.SimpleNamespace(get_object=self.get_object, close=lambda: self.calls.append(("close",)))

    def get_object(self, name, path, introspect=True):
        self.calls.append(("get_object", name, path, introspect))
        return path

    def Interface(self, path, interface):
        if path == worker.REGISTER_SERVER_PATH:
            assert interface == worker.REGISTER_SERVER_INTERFACE
            return types.SimpleNamespace(Start=self.start, Stop=self.stop)
        assert (path, interface) == (worker.REGISTER_PATH, worker.REGISTER_INTERFACE)
        return types.SimpleNamespace(RegisterWithActivationKeys=self.register)

    def start(self, locale, timeout):
        self.calls.append(("Start", locale, timeout))
        return "unix:abstract=/tmp/dbus-private"

    def stop(self, locale, timeout):
        self.calls.append(("Stop", locale, timeout))
        return True

    def register(self, organization, keys, options, connection, locale, timeout):
        self.calls.append(("Register", organization, list(keys), keys.signature, dict(options), options.signature,
                           dict(connection), connection.signature, locale, timeout))
        if self.observe is not None:
            self.observe()
        if self.error is not None:
            raise self.error
        with open(self.cert, "w", encoding="ascii") as handle:
            handle.write(self.consumer)
        return json.dumps({"uuid": self.consumer, "name": "guest", "owner": {"key": "redacted"}})

    def registrations(self):
        return [call for call in self.calls if call[0] == "Register"]


class Runner:
    def __init__(self, code=0, effect=None):
        self.calls = []
        self.code = code
        self.effect = effect

    def __call__(self, argv, timeout):
        self.calls.append((list(argv), timeout))
        if self.effect is not None:
            self.effect(argv)
        return self.code


def fake_identity(path):
    with open(path, encoding="ascii") as handle:
        text = handle.read().strip()
    return text if worker.UUID.fullmatch(text) else None


class Setup:
    def __init__(self, tmp_path, consumer=UNEW, error=None, code=0, effect=None, observe=None):
        tmp_path.mkdir(parents=True, exist_ok=True)
        self.cert = str(tmp_path / "cert.pem")
        self.directory = str(tmp_path / "etc")
        self.record = os.path.join(self.directory, "registration.json")
        self.bus = FakeBus(self.cert, consumer=consumer, error=error, observe=observe)
        self.run = Runner(code, effect)
        self.guest = worker.Guest(cert=self.cert, record=self.record, directory=self.directory, owner=os.getuid(),
                                  run=self.run, bus=lambda: self.bus, identity=fake_identity)

    def with_cert(self, consumer):
        with open(self.cert, "w", encoding="ascii") as handle:
            handle.write(consumer)

    def with_record(self, consumer, request=DIGEST, raw=None):
        os.makedirs(self.directory, 0o700, exist_ok=True)
        data = raw if raw is not None else json.dumps({"consumer": consumer, "request": request}).encode()
        with open(self.record, "wb") as handle:
            handle.write(data)
        os.chmod(self.record, 0o600)

    def record_bytes(self):
        try:
            with open(self.record, "rb") as handle:
                return handle.read()
        except FileNotFoundError:
            return None

    def call(self, operation, **values):
        payload = {"operation": operation, "request": DIGEST}
        if operation == "register":
            payload.update({"organization": ORG, "activationKey": KEY, "enable": [], "disable": []})
        payload.update(values)
        output = io.StringIO()
        assert worker.main(stdin=io.BytesIO(json.dumps(payload).encode()), stdout=output, guest=self.guest) == 0
        text = output.getvalue()
        assert text.endswith("\n") and text.count("\n") == 1
        return json.loads(text), text


def canonical(consumer, request=DIGEST):
    return json.dumps({"consumer": consumer, "request": request}, sort_keys=True, separators=(",", ":")).encode()


def test_registers_once_and_records_the_consumer(tmp_path):
    seen = []
    setup = Setup(tmp_path, observe=lambda: seen.append(setup.record_bytes()))
    answer = setup.call("register")[0]
    assert answer == {"ok": True, "result": {"changed": True, "consumer": UNEW, "owned": True,
                                             "repositories": {"disable": [], "enable": []}, "state": "registered"}}
    assert seen == [canonical("")]
    assert setup.bus.registrations() == [("Register", ORG, [KEY], "s", {}, "sv", {}, "sv", "", worker.REGISTER_CEILING)]
    starts = [call for call in setup.bus.calls if call[0] in ("Start", "Stop")]
    assert starts == [("Start", "", worker.REGISTER_CEILING), ("Stop", "", worker.REGISTER_CEILING)]
    assert ("get_object", None, worker.REGISTER_PATH, False) in setup.bus.calls
    assert setup.record_bytes() == canonical(UNEW)
    assert os.stat(setup.record).st_mode & 0o777 == 0o600
    assert os.stat(setup.directory).st_mode & 0o777 == 0o700
    assert not os.path.exists(os.path.join(setup.directory, ".registration.json.tmp"))
    assert setup.run.calls == []


@pytest.mark.parametrize("record", ["none", "other-consumer", "other-request", "unreadable"])
def test_retains_a_foreign_registration(tmp_path, record):
    setup = Setup(tmp_path)
    setup.with_cert(U2)
    if record == "other-consumer":
        setup.with_record(U1)
    elif record == "other-request":
        setup.with_record("", request=OTHER)
    elif record == "unreadable":
        setup.with_record("", raw=b"{not json")
    before = setup.record_bytes()
    answer = setup.call("register", enable=["rhel-9-for-x86_64-baseos-rpms"])[0]
    assert answer == {"ok": False, "reason": "registration-foreign"}
    assert setup.bus.calls == [] and setup.run.calls == []
    assert setup.record_bytes() == before
    answer = setup.call("unregister")[0]
    assert answer["result"]["state"] == "retained" and answer["result"]["changed"] is False
    assert answer["result"]["consumer"] == "" and answer["result"]["owned"] is False
    assert setup.run.calls == [] and setup.record_bytes() == before
    answer = setup.call("inspect")[0]
    assert answer["result"]["state"] == "foreign" and answer["result"]["consumer"] == ""


def test_a_record_another_account_could_write_is_not_proof(tmp_path):
    setup = Setup(tmp_path)
    setup.with_cert(U1)
    setup.with_record(U1)
    os.chmod(setup.record, 0o644)
    answer = setup.call("register")[0]
    assert answer == {"ok": False, "reason": "registration-foreign"}
    os.chmod(setup.record, 0o600)
    os.symlink(setup.record, setup.record + ".link")
    setup.guest.record = setup.record + ".link"
    answer = setup.call("inspect")[0]
    assert answer["result"]["state"] == "foreign"


def test_replays_an_owned_registration_unchanged(tmp_path):
    setup = Setup(tmp_path)
    setup.with_cert(U1)
    setup.with_record(U1)
    before = setup.record_bytes()
    answer = setup.call("register", enable=["rhel-9-for-x86_64-baseos-rpms"])[0]
    assert answer == {"ok": True, "result": {"changed": False, "consumer": U1, "owned": True,
                                             "repositories": {"disable": [], "enable": ["rhel-9-for-x86_64-baseos-rpms"]},
                                             "state": "unchanged"}}
    assert setup.bus.calls == []
    assert setup.run.calls == [([SM, "repos", "--enable=rhel-9-for-x86_64-baseos-rpms"], worker.REPOSITORIES_CEILING)]
    assert setup.record_bytes() == before


def test_registers_again_when_its_record_survives_without_identity(tmp_path):
    setup = Setup(tmp_path / "a")
    setup.with_record(U1)
    answer = setup.call("register")[0]
    assert answer["ok"] is True and answer["result"]["state"] == "registered"
    assert len(setup.bus.registrations()) == 1 and setup.record_bytes() == canonical(UNEW)

    setup = Setup(tmp_path / "b")
    setup.with_record(U1, request=OTHER)
    answer = setup.call("register")[0]
    assert answer == {"ok": False, "reason": "registration-foreign"} and setup.bus.calls == []

    setup = Setup(tmp_path / "c")
    setup.with_cert(U1)
    setup.with_record("")
    answer = setup.call("register")[0]
    assert answer["ok"] is True and answer["result"]["state"] == "adopted"
    assert answer["result"]["changed"] is True and answer["result"]["consumer"] == U1
    assert setup.bus.calls == [] and setup.record_bytes() == canonical(U1)


def test_a_reply_that_disagrees_with_the_certificate_stays_pending(tmp_path):
    setup = Setup(tmp_path)
    setup.bus.register = lambda *arguments, **options: json.dumps({"uuid": U2})
    answer = setup.call("register")[0]
    assert answer == {"ok": False, "reason": "transport"}
    assert setup.record_bytes() == canonical("")


def test_unregisters_only_its_own_consumer_and_proves_absence(tmp_path):
    setup = Setup(tmp_path / "owned", effect=lambda argv: os.unlink(setup.cert))
    setup.with_cert(U1)
    setup.with_record(U1)
    answer = setup.call("unregister")[0]
    assert answer == {"ok": True, "result": {"changed": True, "consumer": U1, "owned": False,
                                             "repositories": {"disable": [], "enable": []}, "state": "unregistered"}}
    assert setup.run.calls == [([SM, "unregister"], worker.UNREGISTER_CEILING)]
    assert setup.record_bytes() is None

    stays = Setup(tmp_path / "stays")
    stays.with_cert(U1)
    stays.with_record(U1)
    kept = stays.record_bytes()
    answer = stays.call("unregister")[0]
    assert answer == {"ok": False, "reason": "registration-unavailable", "cause": "unregister"}
    assert stays.run.calls == [([SM, "unregister"], worker.UNREGISTER_CEILING)] and stays.record_bytes() == kept

    failed = Setup(tmp_path / "failed", code=1)
    failed.with_cert(U1)
    failed.with_record("")
    answer = failed.call("unregister")[0]
    assert answer == {"ok": False, "reason": "registration-unavailable", "cause": "unregister"}
    assert failed.record_bytes() is not None

    none = Setup(tmp_path / "none")
    answer = none.call("unregister")[0]
    assert answer["result"]["state"] == "absent" and answer["result"]["changed"] is False and none.run.calls == []

    leftover = Setup(tmp_path / "leftover")
    leftover.with_record(U1)
    answer = leftover.call("unregister")[0]
    assert answer["result"]["state"] == "absent" and answer["result"]["changed"] is True
    assert leftover.run.calls == [] and leftover.record_bytes() is None

    other = Setup(tmp_path / "other")
    other.with_record(U1, request=OTHER)
    answer = other.call("unregister")[0]
    assert answer["result"]["state"] == "absent" and answer["result"]["changed"] is False
    assert other.record_bytes() is not None

    for each in (setup, stays, failed, none, leftover, other):
        for argv in (call[0] for call in each.run.calls):
            assert "clean" not in argv and "--force" not in argv


COLLISION = json.dumps({"exception": "RestlibException", "severity": "error",
                        "message": "The DMI UUID of this host (AAAAAAAA-0000-4000-8000-000000000001) "
                                   "matches other registered hosts: host-a.example.test"})


@pytest.mark.parametrize("error,expected", [
    (DBusException(COLLISION), {"ok": False, "reason": "registration-identity-collision"}),
    (DBusException(json.dumps({"exception": "UnauthorizedException", "severity": "error", "message": "Invalid credentials"})),
     {"ok": False, "reason": "registration-refused"}),
    (DBusException(json.dumps({"exception": "ConnectionException", "severity": "error", "message": "unreachable"})),
     {"ok": False, "reason": "registration-unavailable", "cause": "service"}),
    (DBusException("", name="org.freedesktop.DBus.Error.NoReply"),
     {"ok": False, "reason": "registration-unavailable", "cause": "timeout"}),
    (DBusException("", name="org.freedesktop.DBus.Error.ServiceUnknown"),
     {"ok": False, "reason": "registration-unavailable", "cause": "tooling"}),
    (DBusException("This system is already registered."), {"ok": False, "reason": "registration-foreign"}),
], ids=["collision", "unauthorized", "connection", "noreply", "service-unknown", "already-registered"])
def test_classifies_a_dmi_uuid_collision_without_reflecting_server_text(tmp_path, error, expected):
    setup = Setup(tmp_path, error=error)
    answer, text = setup.call("register")
    assert answer == expected
    for leaked in ("AAAAAAAA", "host-a.example.test", "DMI", "Invalid credentials", "unreachable", "already"):
        assert leaked not in text
    assert ("Stop", "", worker.REGISTER_CEILING) in setup.bus.calls
    assert setup.record_bytes() == canonical("")


def test_repository_changes_disable_before_enable_and_purge_on_star(tmp_path):
    setup = Setup(tmp_path)
    setup.with_cert(U1)
    setup.with_record(U1)
    enable = ["rhel-9-for-x86_64-baseos-rpms", "rhel-9-for-x86_64-appstream-rpms"]
    answer = setup.call("register", enable=enable, disable=["*"])[0]
    assert answer["ok"] is True and answer["result"]["repositories"] == {"disable": ["*"], "enable": enable}
    assert setup.run.calls == [([SM, "repos", "--disable=*", "--enable=rhel-9-for-x86_64-baseos-rpms",
                                 "--enable=rhel-9-for-x86_64-appstream-rpms"], worker.REPOSITORIES_CEILING)]

    empty = Setup(tmp_path / "empty")
    empty.with_cert(U1)
    empty.with_record(U1)
    assert empty.call("register")[0]["ok"] is True and empty.run.calls == []

    failing = Setup(tmp_path / "failing", code=1)
    failing.with_cert(U1)
    failing.with_record(U1)
    assert failing.call("register", enable=enable)[0] == {"ok": False, "reason": "registration-repositories"}

    for enable_list, disable_list in ((["*"], []), (["a"], ["a"]), (["a b"], []), (["a,b"], []), (["%x"], []),
                                      (["a", "a"], []), ([], ["x/y"]), (["é"], []), (["x" * 256], [])):
        refused = Setup(tmp_path / "refused")
        refused.with_cert(U1)
        refused.with_record(U1)
        assert refused.call("register", enable=enable_list, disable=disable_list)[0] == {"ok": False, "reason": "registration-input"}
        assert refused.run.calls == [] and refused.bus.calls == []


def test_worker_answers_transport_for_any_unexpected_failure(tmp_path):
    setup = Setup(tmp_path, error=RuntimeError("boom " + KEY))
    answer, text = setup.call("register")
    assert answer == {"ok": False, "reason": "transport"} and KEY not in text and "boom" not in text

    for data in (b"{not json", b"[]", json.dumps({"operation": "inspect"}).encode(),
                 json.dumps({"operation": "inspect", "request": "ABC"}).encode(),
                 json.dumps({"operation": "inspect", "request": DIGEST, "extra": 1}).encode(),
                 json.dumps({"operation": "register", "request": DIGEST, "organization": "a b", "activationKey": KEY,
                             "enable": [], "disable": []}).encode(),
                 b" " * (worker.MAX_INPUT + 1)):
        output = io.StringIO()
        assert worker.main(stdin=io.BytesIO(data), stdout=output, guest=setup.guest) == 0
        assert output.getvalue() == '{"ok":false,"reason":"registration-input"}\n'

    def explode():
        raise KeyboardInterrupt

    setup.guest.certificate = explode
    output = io.StringIO()
    assert worker.main(stdin=io.BytesIO(json.dumps({"operation": "inspect", "request": DIGEST}).encode()),
                       stdout=output, guest=setup.guest) == 0
    assert output.getvalue() == '{"ok":false,"reason":"transport"}\n'


def test_worker_source_loads_with_the_standard_library_only(monkeypatch):
    path = worker.__file__
    with open(path, encoding="utf-8") as handle:
        source = handle.read()
    real_import = builtins.__import__

    def blocked(name, *arguments, **options):
        if name.split(".")[0] in ("dbus", "rhsm", "ansible", "ansible_collections"):
            raise ImportError(name)
        return real_import(name, *arguments, **options)

    monkeypatch.setattr(builtins, "__import__", blocked)
    for name in ("dbus", "rhsm"):
        monkeypatch.setitem(sys.modules, name, None)
    namespace = {"__name__": "fixed_rhsm_worker"}
    exec(compile(source, "<fixed-rhsm-worker>", "exec"), namespace)  # pylint: disable=exec-used
    assert callable(namespace["main"])
    with pytest.raises(namespace["Refused"]) as refused:
        namespace["load_dbus"]()
    assert (refused.value.reason, refused.value.cause) == ("registration-unavailable", "tooling")
    monkeypatch.setattr(builtins, "__import__", real_import)

    tree = ast.parse(source)
    allowed = {"__future__", "json", "os", "re", "stat", "subprocess", "sys"}
    for node in tree.body:
        if isinstance(node, ast.Import):
            assert {alias.name.split(".")[0] for alias in node.names} <= allowed
        elif isinstance(node, ast.ImportFrom):
            assert node.module in allowed
    for node in ast.walk(tree):
        if isinstance(node, ast.Constant) and isinstance(node.value, str):
            assert not re.search(r"-v[0-9]+\b", node.value), node.value
