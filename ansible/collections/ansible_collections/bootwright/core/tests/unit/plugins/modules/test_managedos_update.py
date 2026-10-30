"""The update module runs the fixed worker over the pinned session with only the operation, the digest, the release and the pin."""

from __future__ import annotations

import json
import shlex

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import bounded_process, managedos_guest_ssh
from ansible_collections.bootwright.core.plugins.modules import managedos_update as module

DIGEST = "0123456789abcdef" * 4
KERNEL = "5.14.0-611.5.1.el9_7.x86_64"
RESULT = {"changed": True, "kernel": KERNEL, "packages": 212, "pin": "release", "rebooted": False, "release": "9.8", "state": "upgraded", "transaction": 3}


class FakePopen:
    """A recording bounded runner that answers with fixed bytes."""

    def __init__(self, stdout=b"", stderr=b"", code=0, timeout=False, failure=None):
        self.calls = []
        self.stdout, self.stderr, self.code, self.timeout, self.failure = stdout, stderr, code, timeout, failure

    def __call__(self, argv, data, timeout, limit, environment, **options):
        self.calls.append({"argv": list(argv), "stdin": data, "timeout": timeout, "env": environment})
        if self.timeout:
            raise bounded_process.Failure("timeout")
        if self.failure:
            raise bounded_process.Failure(self.failure)
        return bounded_process.Result(self.code, self.stdout, self.stderr)


def session(popen):
    return lambda argv, data, timeout: managedos_guest_ssh.run(argv, data, timeout, runner=popen)


def params(tmp_path, operation="apply", **values):
    paths = {}
    for name, data in (("ssh_config", b"Ciphers aes256-gcm@openssh.com\n"), ("known_hosts", b"192.0.2.10 ssh-ed25519 AAAA\n"),
                       ("identity", b"private\n")):
        path = tmp_path / name
        path.write_bytes(data)
        paths[name] = str(path)
    value = {"operation": operation, "address": "192.0.2.10", "user": "fleet", "identity": paths["identity"],
             "ssh_config": paths["ssh_config"], "known_hosts": paths["known_hosts"],
             "host_key_algorithms": "ecdsa-sha2-nistp256", "policy_host_key_algorithms": "ecdsa-sha2-nistp256,rsa-sha2-512",
             "request": DIGEST, "release": "9.8", "pin": "" if operation == "inspect" else "release", "timeout": 1800}
    value.update(values)
    return value


def encoded(value):
    return (json.dumps(value) + "\n").encode()


def test_only_the_operation_digest_and_release_reach_the_worker_over_the_pinned_session(tmp_path):
    popen = FakePopen(stdout=encoded({"ok": True, "result": RESULT}))
    outcome = module.execute(params(tmp_path), run=session(popen))
    update = {key: value for key, value in RESULT.items() if key != "changed"}
    assert outcome == {"changed": True, "evidence": update, "ok": True, "updated": True}
    call = popen.calls[0]
    assert json.loads(call["stdin"]) == {"operation": "apply", "pin": "release", "release": "9.8", "request": DIGEST}
    assert call["timeout"] == 1800 and call["env"] == {"PATH": "/usr/bin:/usr/sbin", "LC_ALL": "C"}
    remote = shlex.split(call["argv"][-1])
    assert remote == ["/usr/bin/sudo", "-n", "/usr/bin/python3", "-I", "-c", module.worker_source()]
    assert remote[-1].endswith("\nraise SystemExit(main())\n")
    assert call["argv"][:3] == [managedos_guest_ssh.SSH, "-F", str(tmp_path / "ssh_config")]
    assert "UserKnownHostsFile=" + str(tmp_path / "known_hosts") in call["argv"]
    assert "HostKeyAlgorithms=ecdsa-sha2-nistp256" in call["argv"] and "StrictHostKeyChecking=yes" in call["argv"]


ABSENT = {"changed": False, "kernel": KERNEL, "packages": 0, "pin": "", "rebooted": False, "release": "", "state": "absent", "transaction": 0}


@pytest.mark.parametrize("state", ["pending", "upgraded", "complete"])
def test_an_inspection_preserves_the_metadata_of_an_owned_update(tmp_path, state):
    native = dict(RESULT, changed=False, state=state, rebooted=state == "complete")
    popen = FakePopen(stdout=encoded({"ok": True, "result": native}))
    outcome = module.execute(params(tmp_path, "inspect"), run=session(popen))
    assert outcome == {"changed": False, "evidence": {key: value for key, value in native.items() if key != "changed"},
                       "ok": True, "updated": False}
    assert len(popen.calls) == 1


@pytest.mark.parametrize("operation, popen, expected", [
    ("apply", FakePopen(timeout=True), {"changed": False, "ok": False, "reason": "transport", "cause": "timeout"}),
    ("apply", FakePopen(failure="start"), {"changed": False, "ok": False, "reason": "transport", "cause": "session"}),
    ("apply", FakePopen(code=255, stderr=b"Host key verification failed.\n"),
     {"changed": False, "ok": False, "reason": "transport", "cause": "host-key"}),
    ("apply", FakePopen(code=255), {"changed": False, "ok": False, "reason": "transport", "cause": "unreachable"}),
    ("apply", FakePopen(code=1), {"changed": False, "ok": False, "reason": "transport", "cause": "exit"}),
    ("apply", FakePopen(stdout=b"Traceback\n"), {"changed": False, "ok": False, "reason": "transport", "cause": "answer"}),
    ("apply", FakePopen(stdout=encoded({"ok": True, "result": dict(RESULT, state="absent")})),
     {"changed": False, "ok": False, "reason": "transport", "cause": "answer"}),
    ("apply", FakePopen(stdout=encoded({"ok": True, "result": dict(RESULT, release="9.7")})),
     {"changed": False, "ok": False, "reason": "transport", "cause": "answer"}),
    ("apply", FakePopen(stdout=encoded({"ok": True, "result": dict(RESULT, rebooted=True)})),
     {"changed": False, "ok": False, "reason": "transport", "cause": "answer"}),
    ("apply", FakePopen(stdout=encoded({"ok": True, "result": dict(RESULT, kernel="a b")})),
     {"changed": False, "ok": False, "reason": "transport", "cause": "answer"}),
    ("apply", FakePopen(stdout=encoded({"ok": True, "result": dict(RESULT, transaction=True)})),
     {"changed": False, "ok": False, "reason": "transport", "cause": "answer"}),
    ("apply", FakePopen(stdout=encoded({"ok": True, "result": dict(RESULT, extra=1)})),
     {"changed": False, "ok": False, "reason": "transport", "cause": "answer"}),
    ("complete", FakePopen(stdout=encoded({"ok": True, "result": RESULT})),
     {"changed": False, "ok": False, "reason": "transport", "cause": "answer"}),
    ("inspect", FakePopen(stdout=encoded({"ok": True, "result": ABSENT})),
     {"changed": False, "evidence": {key: value for key, value in dict(ABSENT, kernel="").items() if key != "changed"},
      "ok": True, "updated": False}),
    ("inspect", FakePopen(stdout=encoded({"ok": True, "result": dict(ABSENT, release="9.8")})),
     {"changed": False, "ok": False, "reason": "transport", "cause": "answer"}),
    ("apply", FakePopen(stdout=encoded({"ok": False, "reason": "update-release-drift", "detail": "9.9@rhel-9-baseos"})),
     {"changed": False, "ok": False, "reason": "update-release-drift", "detail": "9.9@rhel-9-baseos"}),
    ("apply", FakePopen(stdout=encoded({"ok": False, "reason": "update-release-mismatch", "detail": "9.6"})),
     {"changed": False, "ok": False, "reason": "update-release-mismatch", "detail": "9.6"}),
    ("apply", FakePopen(stdout=encoded({"ok": False, "reason": "update-release-mismatch", "detail": "9.6@x"})),
     {"changed": False, "ok": False, "reason": "transport", "cause": "answer"}),
    ("apply", FakePopen(stdout=encoded({"ok": False, "reason": "update-release-drift", "detail": "https://x/y"})),
     {"changed": False, "ok": False, "reason": "transport", "cause": "answer"}),
    ("apply", FakePopen(stdout=encoded({"ok": False, "reason": "update-foreign", "detail": "9.6"})),
     {"changed": False, "ok": False, "reason": "transport", "cause": "answer"}),
    ("apply", FakePopen(stdout=encoded({"ok": False, "reason": "update-unavailable", "cause": "metadata"})),
     {"changed": False, "ok": False, "reason": "update-unavailable", "cause": "metadata"}),
    ("apply", FakePopen(stdout=encoded({"ok": False, "reason": "update-unavailable", "cause": "transaction"})),
     {"changed": False, "ok": False, "reason": "transport", "cause": "answer"}),
    ("apply", FakePopen(stdout=encoded({"ok": False, "reason": "update-failed"})),
     {"changed": False, "ok": False, "reason": "transport", "cause": "answer"}),
    ("apply", FakePopen(stdout=encoded({"ok": False, "reason": "update-no-repository"})),
     {"changed": False, "ok": False, "reason": "update-no-repository"}),
    ("apply", FakePopen(stdout=encoded({"ok": False, "reason": "registration-foreign"})),
     {"changed": False, "ok": False, "reason": "transport", "cause": "answer"}),
    ("apply", FakePopen(stdout=encoded({"ok": True, "result": dict(RESULT, pin="contentView")})),
     {"changed": False, "ok": False, "reason": "transport", "cause": "answer"}),
    ("inspect", FakePopen(stdout=encoded({"ok": True, "result": dict(ABSENT, pin="release")})),
     {"changed": False, "ok": False, "reason": "transport", "cause": "answer"}),
    ("inspect", FakePopen(stdout=encoded({"ok": True, "result": dict(RESULT, changed=False, pin="contentView")})),
     {"changed": False, "evidence": {key: value for key, value in dict(RESULT, pin="contentView").items() if key != "changed"},
      "ok": True, "updated": False}),
    ("inspect", FakePopen(stdout=encoded({"ok": True, "result": dict(RESULT, changed=False, pin="")})),
     {"changed": False, "ok": False, "reason": "transport", "cause": "answer"}),
], ids=["timeout", "start-failure", "changed-key", "unreachable", "exit", "non-json", "wrong-state", "other-release",
        "rebooted-upgraded", "bad-kernel", "boolean-transaction", "extra-key", "complete-not-complete", "inspect-absent",
        "absent-with-release", "drift", "mismatch", "mismatch-with-repository", "drift-url", "foreign-detail",
        "unavailable", "wrong-cause", "missing-cause", "no-repository", "other-reason", "other-pin", "absent-with-pin",
        "inspect-recorded-pin", "inspect-recorded-without-pin"])
def test_module_accepts_only_the_closed_worker_answer(tmp_path, operation, popen, expected):
    assert module.execute(params(tmp_path, operation), run=session(popen)) == expected
    assert len(popen.calls) == 1


@pytest.mark.parametrize("values, message", [
    ({"request": "ABC"}, "request is not 64 lowercase hexadecimal characters"),
    ({"release": "9"}, "release is not a <major>.<minor> release"),
    ({"release": "9.8; reboot"}, "release is not a <major>.<minor> release"),
    ({"timeout": 1801}, "timeout is not between 1 and 1800 seconds"),
    ({"timeout": 0}, "timeout is not between 1 and 1800 seconds"),
    ({"pin": ""}, "pin is release, contentView or variable for apply and complete, and empty for inspect"),
    ({"pin": "minor"}, "pin is release, contentView or variable for apply and complete, and empty for inspect"),
    ({"operation": "inspect", "pin": "release"}, "pin is release, contentView or variable for apply and complete, and empty for inspect"),
])
def test_bad_arguments_refuse_before_any_session(tmp_path, values, message):
    popen = FakePopen()
    with pytest.raises(module.ArgumentError) as refused:
        module.execute(params(tmp_path, **values), run=session(popen))
    assert str(refused.value) == message and popen.calls == []


def test_a_session_the_builder_refuses_is_named_without_running(tmp_path):
    popen = FakePopen()
    outcome = module.execute(params(tmp_path, ssh_config="/dev/null"), run=session(popen))
    assert outcome["reason"] == "transport" and outcome["cause"] == "session" and popen.calls == []
