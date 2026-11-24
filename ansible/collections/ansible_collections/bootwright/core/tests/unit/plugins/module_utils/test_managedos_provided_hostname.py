"""Exercise the provided hostname worker against a fake hostnamectl and kernel nodename."""

import os
from types import SimpleNamespace

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import bounded_process
from ansible_collections.bootwright.core.plugins.module_utils import managedos_provided_hostname as worker

DIGEST = "2" * 64
NAME = "witness.example.test"


class Host:
    """A host whose hostnamectl answers the static name and set-hostname changes both names."""

    def __init__(self, monkeypatch, nodename="other", static="other", write=0, sticky=False):
        self.nodename, self.static, self.write, self.sticky, self.calls = nodename, static, write, sticky, []
        monkeypatch.setattr(worker.os, "uname", lambda: SimpleNamespace(nodename=self.nodename))
        monkeypatch.setattr(worker.os, "geteuid", lambda: 0)
        monkeypatch.setattr(worker.bounded_process, "run", self.run)

    def run(self, argv, **keywords):
        assert argv[0] == "/usr/bin/hostnamectl" and keywords["timeout"] > 0 and keywords["environment"]["LC_ALL"] == "C"
        self.calls.append(argv[1:])
        if argv[1:] == ["--static"]:
            return bounded_process.Result(0, self.static.encode() + b"\n", b"")
        if self.write == 0 and not self.sticky:
            self.nodename = self.static = argv[2]
        return bounded_process.Result(self.write, b"", b"")


def test_apply_writes_a_different_hostname_and_proves_both_names(monkeypatch):
    host = Host(monkeypatch)
    assert worker.execute("apply", DIGEST, NAME) == {"changed": True, "state": "complete", "hostname": NAME}
    assert ["set-hostname", NAME] in host.calls


def test_apply_leaves_a_matching_host_untouched(monkeypatch):
    host = Host(monkeypatch, NAME, NAME)
    assert worker.execute("apply", DIGEST, NAME) == {"changed": False, "state": "complete", "hostname": NAME}
    assert host.calls == [["--static"]]


@pytest.mark.parametrize("nodename,static", [(NAME, "other"), ("other", NAME)])
def test_apply_repairs_one_name_that_differs(monkeypatch, nodename, static):
    Host(monkeypatch, nodename, static)
    assert worker.execute("apply", DIGEST, NAME)["changed"] is True


def test_observe_never_writes(monkeypatch):
    host = Host(monkeypatch)
    assert worker.execute("observe", DIGEST, NAME) == {"changed": False, "state": "absent", "hostname": NAME}
    assert host.calls == [["--static"]]
    Host(monkeypatch, NAME, NAME)
    assert worker.execute("observe", DIGEST, NAME)["state"] == "complete"


def test_a_failed_hostnamectl_refuses_the_write(monkeypatch):
    Host(monkeypatch, write=1)
    with pytest.raises(worker.Refused) as error:
        worker.execute("apply", DIGEST, NAME)
    assert error.value.reason == "hostname-write"


def test_a_name_something_else_keeps_rewriting_is_unproved(monkeypatch):
    Host(monkeypatch, sticky=True)
    with pytest.raises(worker.Refused) as error:
        worker.execute("apply", DIGEST, NAME)
    assert error.value.reason == "hostname-unproved"


def test_an_unreadable_static_hostname_is_unproved(monkeypatch):
    Host(monkeypatch)
    monkeypatch.setattr(worker.bounded_process, "run", lambda *arguments, **keywords: bounded_process.Result(1, b"", b""))
    with pytest.raises(worker.Refused) as error:
        worker.execute("observe", DIGEST, NAME)
    assert error.value.reason == "hostname-unproved"

    def stalled(*arguments, **keywords):
        raise bounded_process.Failure("timeout")

    monkeypatch.setattr(worker.bounded_process, "run", stalled)
    with pytest.raises(worker.Refused):
        worker.execute("observe", DIGEST, NAME)


@pytest.mark.parametrize("operation,digest,hostname", [
    ("remove", DIGEST, NAME), ("apply", "short", NAME), ("apply", DIGEST, ""), ("apply", DIGEST, "-bad.example"),
    ("apply", DIGEST, "bad..example"), ("apply", DIGEST, "bad name"), ("apply", DIGEST, "a" * 64 + ".test"),
    ("apply", DIGEST, ".".join(["abc"] * 80)),
])
def test_a_request_outside_the_closed_grammar_runs_nothing(monkeypatch, operation, digest, hostname):
    host = Host(monkeypatch)
    with pytest.raises(worker.Refused):
        worker.execute(operation, digest, hostname)
    assert host.calls == []


def test_the_worker_requires_root(monkeypatch):
    host = Host(monkeypatch)
    monkeypatch.setattr(os, "geteuid", lambda: 1000)
    with pytest.raises(worker.Refused):
        worker.execute("apply", DIGEST, NAME)
    assert host.calls == []
