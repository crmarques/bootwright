"""The registration module carries material only on the worker's stdin, over the pinned session only."""

from __future__ import annotations

import io
import json
import os
import shlex
import types

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import bounded_process, managedos_guest_ssh
from ansible_collections.bootwright.core.plugins.module_utils import managedos_rhsm_worker as worker
from ansible_collections.bootwright.core.plugins.modules import managedos_registration as module

DIGEST = "0123456789abcdef" * 4
U1 = "11111111-2222-4333-8444-555555555555"
ORG = "org-sentinel-7f3a"
KEY = "key-sentinel-91c2"
ANSWER = {"ok": True, "result": {"changed": True, "consumer": U1, "owned": True, "release": "",
                                 "repositories": {"disable": ["*"], "enable": ["rhel-9-for-x86_64-baseos-rpms"]},
                                 "state": "registered"}}


class FakePopen:
    """A recording bounded runner that answers with fixed bytes."""

    def __init__(self, stdout=b"", stderr=b"", code=0, timeout=False, failure=None):
        self.calls = []
        self.stdout = stdout
        self.stderr = stderr
        self.code = code
        self.timeout = timeout
        self.failure = failure

    def __call__(self, argv, data, timeout, limit, environment, **options):
        self.calls.append({"argv": list(argv), "options": dict(options, env=environment, timeout=timeout, limit=limit), "stdin": data})
        if self.timeout:
            raise bounded_process.Failure("timeout")
        if self.failure:
            raise bounded_process.Failure(self.failure)
        return bounded_process.Result(self.code, self.stdout, self.stderr)


def session(tmp_path, popen, monkeypatch=None):
    """The module's run, through the shared session runner, with the bounded runner recorded."""
    return lambda argv, data, timeout: managedos_guest_ssh.run(argv, data, timeout, runner=popen)


def files(tmp_path, organization=ORG.encode() + b"\n", key=KEY.encode() + b"\n"):
    paths = {}
    for name, data in (("ssh_config", b"Ciphers aes256-gcm@openssh.com\n"), ("known_hosts", b"192.0.2.10 ssh-ed25519 AAAA\n"),
                       ("identity", b"private\n"), ("organization", organization), ("activation", key)):
        path = tmp_path / name
        path.write_bytes(data)
        paths[name] = str(path)
    return paths


def params(paths, operation="register", **values):
    value = {"operation": operation, "address": "192.0.2.10", "user": "fleet", "identity": paths["identity"],
             "ssh_config": paths["ssh_config"], "known_hosts": paths["known_hosts"],
             "host_key_algorithms": "ecdsa-sha2-nistp256", "policy_host_key_algorithms": "ecdsa-sha2-nistp256,rsa-sha2-512",
             "request": DIGEST, "organization_file": paths["organization"], "activation_key_file": paths["activation"],
             "enable": ["rhel-9-for-x86_64-baseos-rpms"], "disable": ["*"], "release": "", "timeout": 630}
    if operation != "register":
        value.update(organization_file=None, activation_key_file=None, enable=[], disable=[])
    value.update(values)
    return value


def encoded(value):
    return (json.dumps(value) + "\n").encode()


class DBusException(Exception):
    def get_dbus_name(self):
        return "org.freedesktop.DBus.Python.Exception"

    def get_dbus_message(self):
        return json.dumps({"exception": "RestlibException", "severity": "error",
                           "message": "organization " + ORG + " has no key " + KEY})


def test_never_puts_payload_values_in_argv_env_or_errors(tmp_path, monkeypatch):
    paths = files(tmp_path)
    popen = FakePopen(stdout=encoded(ANSWER))
    outcome = module.execute(params(paths), run=session(tmp_path, popen, monkeypatch))
    assert outcome == {"changed": True, "ok": True, "registration": ANSWER["result"]}
    call = popen.calls[0]
    stdin = json.loads(call["stdin"])
    assert stdin == {"activationKey": KEY, "disable": ["*"], "enable": ["rhel-9-for-x86_64-baseos-rpms"],
                     "operation": "register", "organization": ORG, "release": "", "request": DIGEST, "satellite": None}
    released = FakePopen(stdout=encoded(ANSWER))
    module.execute(params(paths, release="9.8", timeout=750), run=session(tmp_path, released, monkeypatch))
    assert json.loads(released.calls[0]["stdin"])["release"] == "9.8" and released.calls[0]["options"]["timeout"] == 750
    refused_release = module.execute(params(paths, release="9.08"), run=session(tmp_path, released, monkeypatch))
    assert refused_release == {"changed": False, "ok": False, "reason": "registration-input",
                               "detail": "the release is not a <major>.<minor> release"} and len(released.calls) == 1
    for sentinel in (ORG, KEY):
        assert not any(sentinel in word for word in call["argv"])
        assert not any(sentinel in value for value in call["options"]["env"].values())
        assert sentinel not in json.dumps(outcome)
    assert call["options"]["env"] == {"PATH": "/usr/bin:/usr/sbin", "LC_ALL": "C"}
    remote = shlex.split(call["argv"][-1])
    assert remote == ["/usr/bin/sudo", "-n", "/usr/bin/python3", "-I", "-c", module.worker_source()]
    assert remote[-1].endswith("\nraise SystemExit(main())\n")

    bad = tmp_path / "bad"
    bad.mkdir()
    refused = module.execute(params(files(bad, organization=b"org sentinel-7f3a")), run=session(tmp_path, popen))
    assert refused == {"changed": False, "ok": False, "reason": "registration-input",
                       "detail": "the organization is not one line of printable ASCII without whitespace"}
    assert "sentinel" not in json.dumps(refused)

    calls = []

    def bus():
        def register(*arguments, **options):
            raise DBusException()
        return types.SimpleNamespace(
            SystemBus=lambda: types.SimpleNamespace(get_object=lambda *a, **k: None),
            Interface=lambda *a: types.SimpleNamespace(Start=lambda *a, **k: "unix:abstract=x", Stop=lambda *a, **k: True,
                                                       RegisterWithActivationKeys=register),
            connection=types.SimpleNamespace(Connection=lambda address: types.SimpleNamespace(
                get_object=lambda *a, **k: None, close=lambda: None)),
            exceptions=types.SimpleNamespace(DBusException=DBusException),
            Array=lambda values, signature: list(values), Dictionary=lambda values, signature: dict(values))

    guest = worker.Guest(cert=str(tmp_path / "cert.pem"), record=str(tmp_path / "etc" / "registration.json"),
                         directory=str(tmp_path / "etc"), owner=os.getuid(),
                         run=lambda argv, timeout: calls.append(argv) or 0, bus=bus, identity=lambda path: None)
    output = io.StringIO()
    worker.main(stdin=io.BytesIO(call["stdin"]), stdout=output, guest=guest)
    assert output.getvalue() == '{"ok":false,"reason":"registration-refused"}\n'
    for sentinel in (ORG, KEY):
        assert sentinel not in output.getvalue()
        assert not any(sentinel in word for argv in calls for word in argv)


@pytest.mark.parametrize("data", [b"a\nb", b"a b", b"a\t", b"", b"\n", b"x" * 256, "é".encode(), b"k\r\n", None],
                         ids=["inner-lf", "space", "tab", "empty", "lf", "long", "non-ascii", "crlf", "symlink"])
def test_rejects_multiline_or_whitespace_secret_values_without_echo(tmp_path, data):
    paths = files(tmp_path)
    if data is None:
        os.unlink(paths["activation"])
        os.symlink(paths["organization"], paths["activation"])
    else:
        with open(paths["activation"], "wb") as handle:
            handle.write(data)
    popen = FakePopen(stdout=encoded(ANSWER))
    outcome = module.execute(params(paths), run=session(tmp_path, popen))
    assert outcome == {"changed": False, "ok": False, "reason": "registration-input",
                       "detail": "the activation key is not one line of printable ASCII without whitespace"}
    assert popen.calls == []
    if data and len(data.strip()) >= 3:
        assert data.strip().decode("utf-8", "replace") not in json.dumps(outcome)


def test_a_value_with_one_final_line_feed_is_accepted(tmp_path):
    paths = files(tmp_path, key=b"key-sentinel-91c2\n")
    popen = FakePopen(stdout=encoded(ANSWER))
    assert module.execute(params(paths), run=session(tmp_path, popen))["ok"] is True
    assert json.loads(popen.calls[0]["stdin"])["activationKey"] == "key-sentinel-91c2"


def guest_argv(paths, **values):
    arguments = {"ssh_config": paths["ssh_config"], "known_hosts": paths["known_hosts"],
                 "host_key_algorithms": "ecdsa-sha2-nistp256",
                 "policy_host_key_algorithms": "ecdsa-sha2-nistp256,rsa-sha2-512",
                 "identity": paths["identity"], "user": "fleet", "address": "192.0.2.10", "remote_argv": ["true"]}
    arguments.update(values)
    return managedos_guest_ssh.guest_argv(**arguments)


def test_registration_session_intersects_pinned_algorithms_with_placement_policy(tmp_path):
    paths = files(tmp_path)
    argv = guest_argv(paths)
    assert argv[:3] == ["/usr/bin/ssh", "-F", paths["ssh_config"]]
    for option in ("UserKnownHostsFile=" + paths["known_hosts"], "GlobalKnownHostsFile=none",
                   "StrictHostKeyChecking=yes", "HostKeyAlgorithms=ecdsa-sha2-nistp256"):
        assert option in argv
        assert argv[argv.index(option) - 1] == "-o"
    assert argv[argv.index("StrictHostKeyChecking=yes") + 2] == "HostKeyAlgorithms=ecdsa-sha2-nistp256"
    assert not any("/dev/null" in word for word in argv)
    assert argv[-4:] == ["-i", paths["identity"], "fleet@192.0.2.10", "true"]

    spaced = tmp_path / "known hosts"
    spaced.write_bytes(b"")
    percent = tmp_path / "known%hosts"
    percent.write_bytes(b"")
    linked = tmp_path / "linked_config"
    os.symlink(paths["ssh_config"], str(linked))
    for refused in ({"ssh_config": "/dev/null"}, {"host_key_algorithms": ""}, {"host_key_algorithms": "+ssh-ed25519"},
                    {"host_key_algorithms": "ssh-dss"}, {"host_key_algorithms": "ssh-ed25519"},
                    {"host_key_algorithms": "ecdsa-sha2-nistp256,ecdsa-sha2-nistp256"},
                    {"host_key_algorithms": "ecdsa-sha2-nistp256,"}, {"policy_host_key_algorithms": ""},
                    {"known_hosts": str(spaced)}, {"known_hosts": str(percent)}, {"ssh_config": str(linked)},
                    {"known_hosts": "relative/known_hosts"}, {"identity": str(tmp_path / "missing")},
                    {"user": "Root"}, {"user": "-oProxyCommand"}, {"address": "-oProxyCommand=x"},
                    {"address": "host_a.example.test"}, {"remote_argv": []}, {"remote_argv": [""]}, {"remote_argv": ["a\0b"]},
                    {"remote_argv": "true"}, {"remote_argv": None}):
        with pytest.raises(managedos_guest_ssh.SessionError):
            guest_argv(paths, **refused)
    assert guest_argv(paths, address="host-a.example.test")[-2] == "fleet@host-a.example.test"
    assert guest_argv(paths, address="2001:db8::1")[-2] == "fleet@2001:db8::1"


@pytest.mark.parametrize("popen,expected", [
    (FakePopen(timeout=True), {"changed": False, "ok": False, "reason": "transport", "cause": "timeout"}),
    (FakePopen(failure="command"), {"changed": False, "ok": False, "reason": "transport", "cause": "session"}),
    (FakePopen(failure="output"), {"changed": False, "ok": False, "reason": "transport", "cause": "answer"}),
    (FakePopen(code=255, stderr=b"@@@\nREMOTE HOST IDENTIFICATION HAS CHANGED!\n"),
     {"changed": False, "ok": False, "reason": "transport", "cause": "host-key"}),
    (FakePopen(code=255, stderr=b"Host key verification failed.\n"),
     {"changed": False, "ok": False, "reason": "transport", "cause": "host-key"}),
    (FakePopen(code=255, stderr=b"ssh: connect to host 192.0.2.10 port 22: No route to host\n"),
     {"changed": False, "ok": False, "reason": "registration-unreachable"}),
    (FakePopen(code=1, stdout=encoded(ANSWER)), {"changed": False, "ok": False, "reason": "transport", "cause": "exit"}),
    (FakePopen(stdout=b"Traceback (most recent call last):\n"),
     {"changed": False, "ok": False, "reason": "transport", "cause": "answer"}),
    (FakePopen(stdout=encoded({"ok": False, "reason": "registration-something"})),
     {"changed": False, "ok": False, "reason": "transport", "cause": "answer"}),
    (FakePopen(stdout=encoded(dict(ANSWER, extra=1))), {"changed": False, "ok": False, "reason": "transport", "cause": "answer"}),
    (FakePopen(stdout=encoded({"ok": False, "reason": "registration-unavailable", "cause": "session"})),
     {"changed": False, "ok": False, "reason": "transport", "cause": "answer"}),
    (FakePopen(stdout=encoded({"ok": False, "reason": "registration-unavailable"})),
     {"changed": False, "ok": False, "reason": "transport", "cause": "answer"}),
    (FakePopen(stdout=encoded({"ok": True, "result": dict(ANSWER["result"], state="retained")})),
     {"changed": False, "ok": False, "reason": "transport", "cause": "answer"}),
    (FakePopen(stdout=encoded({"ok": True, "result": dict(ANSWER["result"], consumer="not-a-uuid")})),
     {"changed": False, "ok": False, "reason": "transport", "cause": "answer"}),
    (FakePopen(stdout=b" " * (managedos_guest_ssh.MAX_OUTPUT + 1)),
     {"changed": False, "ok": False, "reason": "transport", "cause": "answer"}),
    (FakePopen(stdout=encoded({"ok": False, "reason": "registration-unavailable", "cause": "service"})),
     {"changed": False, "ok": False, "reason": "registration-unavailable", "cause": "service"}),
    (FakePopen(stdout=encoded({"ok": False, "reason": "registration-foreign"})),
     {"changed": False, "ok": False, "reason": "registration-foreign"}),
    (FakePopen(stdout=encoded({"ok": False, "reason": "update-release-unavailable"})),
     {"changed": False, "ok": False, "reason": "update-release-unavailable"}),
    (FakePopen(stdout=encoded({"ok": False, "reason": "registration-trust"})),
     {"changed": False, "ok": False, "reason": "registration-trust"}),
    (FakePopen(stdout=encoded({"ok": True, "result": dict(ANSWER["result"], release="unset")})),
     {"changed": True, "ok": True, "registration": dict(ANSWER["result"], release="unset")}),
    (FakePopen(stdout=encoded({"ok": True, "result": dict(ANSWER["result"], release="9.8")})),
     {"changed": False, "ok": False, "reason": "transport", "cause": "answer"}),
    (FakePopen(stdout=encoded({"ok": True, "result": {key: value for key, value in ANSWER["result"].items() if key != "release"}})),
     {"changed": False, "ok": False, "reason": "transport", "cause": "answer"}),
], ids=["timeout", "start-failure", "output-overrun", "changed-key", "unverified-key", "unreachable", "exit", "non-json", "unknown-reason", "extra-key",
        "wrong-cause", "missing-cause", "wrong-state", "bad-consumer", "oversized", "unavailable", "foreign", "release-unavailable",
        "trust", "release-unset", "release-unknown", "release-missing"])
def test_module_accepts_only_the_closed_worker_answer(tmp_path, monkeypatch, popen, expected):
    paths = files(tmp_path)
    assert module.execute(params(paths), run=session(tmp_path, popen, monkeypatch)) == expected
    assert len(popen.calls) == 1


@pytest.mark.parametrize("values,message", [
    ({"organization_file": "/x"}, "organization_file is accepted only for the register operation"),
    ({"activation_key_file": "/x"}, "activation_key_file is accepted only for the register operation"),
    ({"enable": ["a"]}, "enable is accepted only for the register operation"),
    ({"disable": ["*"]}, "disable is accepted only for the register operation"),
    ({"release": "9.8"}, "release is accepted only for the register operation"),
    ({"satellite_hostname": "satellite.example.test"}, "satellite_hostname is accepted only for the register operation"),
    ({"satellite_ca_file": "/x"}, "satellite_ca_file is accepted only for the register operation"),
    ({"request": "ABC"}, "request is not 64 lowercase hexadecimal characters"),
    ({"timeout": 901}, "timeout is not between 1 and 900 seconds"),
])
def test_inspect_refuses_material_and_bad_arguments_before_any_session(tmp_path, values, message):
    popen = FakePopen(stdout=encoded(ANSWER))
    with pytest.raises(module.ArgumentError) as refused:
        module.execute(params(files(tmp_path), operation="inspect", **values), run=session(tmp_path, popen))
    assert str(refused.value) == message and popen.calls == []


PEM = "-----BEGIN CERTIFICATE-----\nMIIBszCCAVmgAwIBAgIUQ0FTRUxMSVRF\n-----END CERTIFICATE-----\n"


def satellite_params(paths, tmp_path, bundle=PEM.encode(), **values):
    ca = tmp_path / "satellite-ca"
    ca.write_bytes(bundle)
    value = params(paths, satellite_hostname="satellite.example.test",
                   satellite_content_base_url="https://capsule.example.test/pulp/content", satellite_ca_file=str(ca))
    value.update(values)
    return value


def test_a_satellite_reaches_the_worker_only_on_its_standard_input(tmp_path):
    paths = files(tmp_path)
    popen = FakePopen(stdout=encoded(ANSWER))
    assert module.execute(satellite_params(paths, tmp_path, timeout=690), run=session(tmp_path, popen))["ok"] is True
    call = popen.calls[0]
    assert json.loads(call["stdin"])["satellite"] == {"caBundle": PEM, "contentBaseURL": "https://capsule.example.test/pulp/content",
                                                      "hostname": "satellite.example.test"}
    assert call["options"]["timeout"] == 690
    assert not any("MIIBszCCAVmgAwIBAgIUQ0FTRUxMSVRF" in word or "capsule.example.test" in word for word in call["argv"])


@pytest.mark.parametrize("bundle", [b"", b"not a certificate", PEM.encode() + b"-----BEGIN PRIVATE KEY-----\nAA\n-----END PRIVATE KEY-----\n",
                                    PEM.encode() * 33, "é".encode(), None],
                         ids=["empty", "garbage", "private-key", "too-many", "non-ascii", "symlink"])
def test_a_bundle_outside_the_grammar_is_refused_before_any_session(tmp_path, bundle):
    paths = files(tmp_path)
    popen = FakePopen(stdout=encoded(ANSWER))
    value = satellite_params(paths, tmp_path, bundle=bundle or b"")
    if bundle is None:
        os.unlink(value["satellite_ca_file"])
        os.symlink(paths["organization"], value["satellite_ca_file"])
    outcome = module.execute(value, run=session(tmp_path, popen))
    assert outcome == {"changed": False, "ok": False, "reason": "registration-input",
                       "detail": "the Satellite CA bundle is not 1 to 32 PEM certificates"}
    assert popen.calls == []


@pytest.mark.parametrize("values", [{"satellite_hostname": None}, {"satellite_ca_file": None}, {"satellite_content_base_url": None}])
def test_satellite_options_come_together(tmp_path, values):
    paths = files(tmp_path)
    with pytest.raises(module.ArgumentError):
        module.execute(satellite_params(paths, tmp_path, **values), run=session(tmp_path, FakePopen()))


def test_a_satellite_content_root_outside_the_grammar_is_refused(tmp_path):
    paths = files(tmp_path)
    popen = FakePopen(stdout=encoded(ANSWER))
    outcome = module.execute(satellite_params(paths, tmp_path, satellite_content_base_url="http://capsule.example.test/pulp"),
                             run=session(tmp_path, popen))
    assert outcome["reason"] == "registration-input" and popen.calls == []
