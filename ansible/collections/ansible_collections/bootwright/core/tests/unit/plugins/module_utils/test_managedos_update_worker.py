"""The guest update worker updates within the release once, owns only its own record and never reflects repository text.

The dnf surface the fake models is dnf 4's public API as RHEL 9 and 10 ship
it: Base.conf.read, conf.substitutions.update_from_etc, read_all_repos,
repos.iter_enabled, fill_sack, upgrade_all, resolve, transaction.install_set,
download_packages and close (dnf/base.py), dnf.exceptions.Error and its
subclasses, and history.old with each transaction's tid, comment and packages
(dnf/db/history.py), whose items carry an action dnf.transaction.FORWARD_ACTIONS
classifies.
"""

from __future__ import annotations

import ast
import builtins
import io
import json
import os
import sys
import types

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import managedos_update_worker as worker

DIGEST = "0123456789abcdef" * 4
OTHER = "fedcba9876543210" * 4
KERNEL = "5.14.0-611.5.1.el9_7.x86_64"
INSTALL, UPGRADE, UPGRADED = 1, 7, 8


class DnfError(Exception):
    pass


class RepoError(DnfError):
    pass


class DepsolveError(DnfError):
    pass


class DownloadError(DnfError):
    pass


class Package:
    def __init__(self, name, version, reponame="rhel-9-for-x86_64-baseos-rpms"):
        self.name, self.version, self.reponame = name, version, reponame


class Repository:
    def __init__(self, identifier):
        self.id = identifier
        self.metadata_expire = 172800


class FakeDnf:
    """A recording dnf module: one Base per call, its failures and its transaction chosen by the test."""

    def __init__(self, repositories=("rhel-9-for-x86_64-baseos-rpms",), install=(), fail=None, history=()):
        self.exceptions = types.SimpleNamespace(Error=DnfError, RepoError=RepoError, DepsolveError=DepsolveError,
                                                DownloadError=DownloadError)
        self.transaction = types.SimpleNamespace(FORWARD_ACTIONS=[INSTALL, UPGRADE])
        self.repositories = [Repository(identifier) for identifier in repositories]
        self.install = list(install)
        self.fail = fail
        self.entries = list(history)
        self.calls = []
        self.downloaded = []

    def Base(self):
        fake = self
        calls = self.calls

        class Base:
            conf = types.SimpleNamespace(read=lambda: calls.append("conf.read"), installroot="/",
                                         substitutions=types.SimpleNamespace(update_from_etc=lambda root: calls.append(("vars", root))))
            repos = types.SimpleNamespace(iter_enabled=lambda: iter(fake.repositories))
            transaction = types.SimpleNamespace(install_set=set(fake.install))
            history = types.SimpleNamespace(old=lambda: list(fake.entries))

            def read_all_repos(self):
                calls.append("read_all_repos")

            def fill_sack(self, load_system_repo):
                calls.append(("fill_sack", load_system_repo, [repository.metadata_expire for repository in fake.repositories]))
                if fake.fail == "metadata":
                    raise RepoError("Cannot download repomd.xml: https://cdn.example.test/secret-token")

            def upgrade_all(self):
                calls.append("upgrade_all")

            def resolve(self, allow_erasing=False):
                calls.append(("resolve", allow_erasing))
                if fake.fail == "resolution":
                    raise DepsolveError("nothing provides libfoo")

            def download_packages(self, packages):
                fake.downloaded.append(sorted(package.name for package in packages))
                if fake.fail == "download":
                    raise DownloadError("Failed to download https://cdn.example.test/secret-token")

            def close(self):
                calls.append("close")

        return Base()


def entry(tid, request=DIGEST, packages=((UPGRADE, "kernel"), (UPGRADED, "kernel"), (INSTALL, "kernel-core"))):
    items = [types.SimpleNamespace(action=action, name=name) for action, name in packages]
    return types.SimpleNamespace(tid=tid, comment=worker.COMMENT + request, packages=lambda: list(items))


class Setup:
    """A guest root: its record directory, its os-release and the recorded dnf calls."""

    def __init__(self, tmp_path, version="9.8", install=(), upgrade_code=None, fail=None, history=None, ran=None, preview_seconds=0.0):
        tmp_path.mkdir(parents=True, exist_ok=True)
        self.directory = str(tmp_path / "etc")
        self.record = os.path.join(self.directory, "update.json")
        self.os_release = str(tmp_path / "os-release")
        with open(self.os_release, "w", encoding="ascii") as handle:
            handle.write('NAME="Red Hat Enterprise Linux"\nVERSION="9.8 (Plow)"\nID="rhel"\nVERSION_ID="%s"\n' % version)
        self.dnf = FakeDnf(install=install, fail=fail, history=history or [])
        self.upgrades = []
        self.now = 100.0
        self.preview_seconds = preview_seconds
        self.ran = ran
        self.upgrade_code = upgrade_code
        self.guest = worker.Guest(record=self.record, directory=self.directory, owner=os.getuid(), os_release=self.os_release,
                                  resolve=self.resolve,
                                  transactions=lambda request: worker.history(request, dnf=self.dnf),
                                  run=self.upgrade, kernel=lambda: KERNEL, clock=lambda: self.now)

    def resolve(self, release):
        counted = worker.preview(release, dnf=self.dnf)
        self.now += self.preview_seconds
        return counted

    def upgrade(self, request, timeout):
        self.upgrades.append((request, timeout, self.read()))
        if self.upgrade_code is not None:
            raise worker.Refused("update-failed", self.upgrade_code)
        if self.ran is not None:
            self.dnf.entries.append(self.ran)

    def with_record(self, state, transaction=0, request=DIGEST, raw=None, mode=0o600, pin="release"):
        os.makedirs(self.directory, 0o700, exist_ok=True)
        data = raw if raw is not None else json.dumps({"pin": pin, "request": request, "state": state, "transaction": transaction}).encode()
        with open(self.record, "wb") as handle:
            handle.write(data)
        os.chmod(self.record, mode)

    def read(self):
        try:
            with open(self.record, "rb") as handle:
                return json.loads(handle.read())
        except FileNotFoundError:
            return None

    def call(self, operation, release="9.8", request=DIGEST, pin=None):
        output = io.StringIO()
        if pin is None:
            pin = "" if operation == "inspect" else "release"
        payload = {"operation": operation, "pin": pin, "release": release, "request": request}
        assert worker.main(stdin=io.BytesIO(json.dumps(payload).encode()), stdout=output, guest=self.guest) == 0
        text = output.getvalue()
        assert text.endswith("\n") and text.count("\n") == 1
        return json.loads(text), text


def result(state, transaction=0, packages=0, changed=True, release="9.8", pin=None):
    if pin is None:
        pin = "release" if state in worker.RECORD_STATES else ""
    return {"ok": True, "result": {"changed": changed, "kernel": KERNEL, "packages": packages, "pin": pin,
                                   "rebooted": state == "complete" and transaction != 0, "release": release,
                                   "state": state, "transaction": transaction}}


def test_an_update_records_pending_before_dnf_and_upgraded_with_its_transaction(tmp_path):
    setup = Setup(tmp_path, install=[Package("kernel", "5.14.0"), Package("redhat-release", "9.8")], ran=entry(4))
    answer = setup.call("apply")[0]
    assert answer == result("upgraded", 4, 2)
    assert setup.upgrades == [(DIGEST, worker.SESSION_CEILING, {"pin": "release", "request": DIGEST, "state": "pending", "transaction": 0})]
    assert setup.read() == {"pin": "release", "request": DIGEST, "state": "upgraded", "transaction": 4}
    assert os.stat(setup.record).st_mode & 0o777 == 0o600 and os.stat(setup.directory).st_mode & 0o777 == 0o700
    assert not os.path.exists(os.path.join(setup.directory, ".update.json.tmp"))
    fill = [call for call in setup.dnf.calls if isinstance(call, tuple) and call[0] == "fill_sack"]
    assert fill == [("fill_sack", True, [0])] and ("resolve", False) in setup.dnf.calls
    assert ("vars", "/") in setup.dnf.calls and setup.dnf.calls.count("close") == 2
    assert setup.dnf.downloaded == [["kernel", "redhat-release"]]

    assert setup.call("complete")[0] == result("complete", 4, 2)
    assert setup.read() == {"pin": "release", "request": DIGEST, "state": "complete", "transaction": 4}
    assert setup.call("apply")[0] == result("complete", 4, 2, changed=False)
    assert len(setup.upgrades) == 1


def test_an_update_that_changes_nothing_completes_without_a_transaction(tmp_path):
    setup = Setup(tmp_path)
    assert setup.call("apply")[0] == result("upgraded")
    assert setup.call("complete")[0] == result("complete")
    assert setup.read() == {"pin": "release", "request": DIGEST, "state": "complete", "transaction": 0}


def test_a_pending_record_upgrades_again_and_finds_the_transaction_a_lost_reply_ran(tmp_path):
    setup = Setup(tmp_path, history=[entry(3, OTHER), entry(5), entry(9, OTHER)])
    setup.with_record("pending")
    assert setup.call("apply")[0] == result("upgraded", 5, 2)
    assert len(setup.upgrades) == 1
    assert setup.read() == {"pin": "release", "request": DIGEST, "state": "upgraded", "transaction": 5}


def test_an_upgraded_record_resumes_without_dnf(tmp_path):
    setup = Setup(tmp_path, history=[entry(5)])
    setup.with_record("upgraded", 5)
    assert setup.call("apply")[0] == result("upgraded", 5, 2)
    assert setup.upgrades == [] and "upgrade_all" not in setup.dnf.calls


@pytest.mark.parametrize("record", ["other-request", "malformed", "extra-key", "bad-state", "negative", "group-readable", "symlink"])
def test_a_record_of_another_installation_or_an_unreadable_one_is_foreign_and_left(tmp_path, record):
    setup = Setup(tmp_path)
    raw = {"malformed": b"{not json", "extra-key": json.dumps({"pin": "release", "request": DIGEST, "state": "pending", "transaction": 0, "x": 1}).encode(),
           "bad-state": json.dumps({"pin": "release", "request": DIGEST, "state": "done", "transaction": 0}).encode(),
           "negative": json.dumps({"pin": "release", "request": DIGEST, "state": "upgraded", "transaction": -1}).encode()}.get(record)
    if record == "symlink":
        os.makedirs(setup.directory, 0o700)
        target = str(tmp_path / "elsewhere.json")
        with open(target, "w", encoding="ascii") as handle:
            json.dump({"pin": "release", "request": DIGEST, "state": "complete", "transaction": 0}, handle)
        os.chmod(target, 0o600)
        os.symlink(target, setup.record)
    else:
        setup.with_record("pending", request=OTHER if record == "other-request" else DIGEST, raw=raw,
                          mode=0o640 if record == "group-readable" else 0o600)
    before = open(setup.record, "rb").read()
    for operation in ("apply", "complete"):
        assert setup.call(operation)[0] == {"ok": False, "reason": "update-foreign"}
    assert setup.call("inspect")[0] == result("foreign", changed=False, release="")
    assert open(setup.record, "rb").read() == before and setup.upgrades == [] and setup.dnf.calls == []


@pytest.mark.parametrize("found, detail", [("9.6", "9.6"), ("10.0", "10.0"), ("", "unknown"), ("9.8 beta", "unknown")])
def test_a_system_on_another_release_refuses_before_any_change(tmp_path, found, detail):
    setup = Setup(tmp_path, version=found)
    answer, text = setup.call("apply")
    assert answer == {"ok": False, "reason": "update-release-mismatch", "detail": detail}
    assert setup.read() is None and setup.dnf.calls == [] and "beta" not in text


def test_complete_proves_the_release_again(tmp_path):
    setup = Setup(tmp_path, version="9.9", history=[entry(5)])
    setup.with_record("upgraded", 5)
    assert setup.call("complete")[0] == {"ok": False, "reason": "update-release-mismatch", "detail": "9.9"}
    assert setup.read() == {"pin": "release", "request": DIGEST, "state": "upgraded", "transaction": 5}
    pending = Setup(tmp_path / "pending")
    pending.with_record("pending")
    assert pending.call("complete")[0] == {"ok": False, "reason": "transport"}
    assert Setup(tmp_path / "absent").call("complete")[0] == {"ok": False, "reason": "transport"}


@pytest.mark.parametrize("package, detail", [
    (Package("redhat-release", "9.9"), "9.9@rhel-9-for-x86_64-baseos-rpms"),
    (Package("redhat-release", "10.0", "mirror:rhel10"), "10.0@mirror:rhel10"),
    (Package("redhat-release", "9.9", "https://cdn.example.test/x"), "9.9@unknown"),
    (Package("redhat-release", "9.9", "r" * 91), "9.9@unknown"),
])
def test_a_transaction_moving_redhat_release_off_the_release_refuses_drift(tmp_path, package, detail):
    setup = Setup(tmp_path, install=[Package("kernel", "5.14.0"), package])
    answer, text = setup.call("apply")
    assert answer == {"ok": False, "reason": "update-release-drift", "detail": detail}
    assert setup.read() is None and setup.upgrades == [] and setup.dnf.downloaded == [] and "https" not in text


@pytest.mark.parametrize("fail, expected", [
    ("metadata", {"ok": False, "reason": "update-unavailable", "cause": "metadata"}),
    ("resolution", {"ok": False, "reason": "update-failed", "cause": "resolution"}),
    ("download", {"ok": False, "reason": "update-unavailable", "cause": "download"}),
])
def test_a_preview_failure_names_its_closed_cause_and_changes_nothing(tmp_path, fail, expected):
    setup = Setup(tmp_path, install=[Package("kernel", "5.14.0")], fail=fail)
    answer, text = setup.call("apply")
    assert answer == expected and "secret-token" not in text and "libfoo" not in text
    assert setup.read() is None and setup.upgrades == [] and setup.dnf.calls[-1] == "close"


def test_no_enabled_repository_refuses_before_metadata(tmp_path):
    setup = Setup(tmp_path)
    setup.dnf.repositories = []
    assert setup.call("apply")[0] == {"ok": False, "reason": "update-no-repository"}
    assert not any(isinstance(call, tuple) and call[0] == "fill_sack" for call in setup.dnf.calls) and setup.read() is None


@pytest.mark.parametrize("cause", ["transaction", "timeout"])
def test_a_failed_transaction_stays_pending(tmp_path, cause):
    setup = Setup(tmp_path, upgrade_code=cause)
    assert setup.call("apply")[0] == {"ok": False, "reason": "update-failed", "cause": cause}
    assert setup.read() == {"pin": "release", "request": DIGEST, "state": "pending", "transaction": 0}


def test_a_counted_upgrade_whose_history_lacks_the_comment_is_refused_and_stays_pending(tmp_path):
    setup = Setup(tmp_path, install=[Package("kernel", "5.14.0")], history=[entry(3, OTHER)])
    assert setup.call("apply")[0] == {"ok": False, "reason": "update-failed", "cause": "history"}
    assert len(setup.upgrades) == 1 and setup.read() == {"pin": "release", "request": DIGEST, "state": "pending", "transaction": 0}
    setup.dnf.entries.append(entry(4))
    assert setup.call("apply")[0] == result("upgraded", 4, 2)
    assert len(setup.upgrades) == 2 and setup.read() == {"pin": "release", "request": DIGEST, "state": "upgraded", "transaction": 4}


@pytest.mark.parametrize("pending", [False, True])
@pytest.mark.parametrize("seconds", [worker.SESSION_CEILING - worker.UPGRADE_FLOOR + 1, worker.SESSION_CEILING * 2])
def test_a_preview_that_overruns_leaves_too_little_session_and_starts_no_upgrade(tmp_path, monkeypatch, pending, seconds):
    started = []
    monkeypatch.setattr(worker.subprocess, "run", lambda argv, **options: started.append(argv))
    setup = Setup(tmp_path, install=[Package("kernel", "5.14.0")], preview_seconds=seconds)
    setup.guest.run = worker.upgrade
    if pending:
        setup.with_record("pending")
    assert setup.call("apply")[0] == {"ok": False, "reason": "update-unavailable", "cause": "timeout"}
    assert started == [] and setup.upgrades == []
    assert setup.read() == ({"pin": "release", "request": DIGEST, "state": "pending", "transaction": 0} if pending else None)


def test_an_upgrade_starts_with_exactly_the_floor_of_session_left(tmp_path):
    setup = Setup(tmp_path, install=[Package("kernel", "5.14.0")], ran=entry(4),
                  preview_seconds=worker.SESSION_CEILING - worker.UPGRADE_FLOOR)
    assert setup.call("apply")[0] == result("upgraded", 4, 2)
    assert [upgrade[1] for upgrade in setup.upgrades] == [worker.UPGRADE_FLOOR]


def test_the_upgrade_runs_the_fixed_dnf_command(monkeypatch):
    calls = []

    def run(argv, **options):
        calls.append((argv, options))
        return types.SimpleNamespace(returncode=0)

    monkeypatch.setattr(worker.subprocess, "run", run)
    worker.upgrade(DIGEST, 1234)
    argv, options = calls[0]
    assert argv == ["/usr/bin/dnf", "upgrade", "--refresh", "--assumeyes", "--comment=bootwright:" + DIGEST]
    assert not any(word in " ".join(argv) for word in ("nogpgcheck", "allowerasing", "repofrompath", "best"))
    assert options["env"] == {"PATH": "/usr/bin:/usr/sbin", "LC_ALL": "C"} and options["timeout"] == 1234
    assert options["stdout"] == options["stderr"] == worker.subprocess.DEVNULL and options["check"] is False
    monkeypatch.setattr(worker.subprocess, "run", lambda argv, **options: types.SimpleNamespace(returncode=1))
    with pytest.raises(worker.Refused) as refused:
        worker.upgrade(DIGEST, 1)
    assert (refused.value.reason, refused.value.cause) == ("update-failed", "transaction")

    def expire(argv, **options):
        raise worker.subprocess.TimeoutExpired(argv, 1)

    monkeypatch.setattr(worker.subprocess, "run", expire)
    with pytest.raises(worker.Refused) as refused:
        worker.upgrade(DIGEST, 1)
    assert (refused.value.reason, refused.value.cause) == ("update-failed", "timeout")


def test_inspect_reads_and_changes_nothing(tmp_path):
    setup = Setup(tmp_path, history=[entry(5)])
    assert setup.call("inspect")[0] == result("absent", changed=False, release="")
    for state, transaction in (("pending", 0), ("upgraded", 5), ("complete", 5)):
        setup.with_record(state, transaction)
        before = open(setup.record, "rb").read()
        assert setup.call("inspect")[0] == result(state, transaction, 2 if transaction else 0, changed=False)
        assert open(setup.record, "rb").read() == before
    assert setup.upgrades == [] and "upgrade_all" not in setup.dnf.calls and "read_all_repos" not in setup.dnf.calls
    moved = Setup(tmp_path / "moved", version="9.9", history=[entry(5)])
    moved.with_record("complete", 5)
    assert moved.call("inspect")[0] == result("upgraded", 5, 2, changed=False)
    lost = Setup(tmp_path / "lost")
    lost.with_record("complete", 5)
    assert lost.call("inspect")[0] == result("upgraded", 5, 0, changed=False)


def test_the_worker_answers_transport_for_any_unexpected_failure(tmp_path):
    setup = Setup(tmp_path)
    for data in (b"{not json", b"[]", json.dumps({"operation": "apply", "request": DIGEST}).encode(),
                 json.dumps({"operation": "remove", "request": DIGEST, "release": "9.8"}).encode(),
                 json.dumps({"operation": "apply", "pin": "release", "request": "ABC", "release": "9.8"}).encode(),
                 json.dumps({"operation": "apply", "pin": "release", "request": DIGEST, "release": "9.08"}).encode(),
                 json.dumps({"operation": "apply", "pin": "release", "request": DIGEST, "release": "9.8", "extra": 1}).encode(),
                 json.dumps({"operation": "apply", "pin": "minor", "request": DIGEST, "release": "9.8"}).encode(),
                 json.dumps({"operation": "apply", "pin": "", "request": DIGEST, "release": "9.8"}).encode(),
                 json.dumps({"operation": "inspect", "pin": "release", "request": DIGEST, "release": "9.8"}).encode(),
                 b" " * (worker.MAX_INPUT + 1)):
        output = io.StringIO()
        assert worker.main(stdin=io.BytesIO(data), stdout=output, guest=setup.guest) == 0
        assert output.getvalue() == '{"ok":false,"reason":"transport"}\n'
    assert setup.dnf.calls == []

    def explode(release):
        raise KeyboardInterrupt

    setup.guest.resolve = explode
    assert setup.call("apply")[0] == {"ok": False, "reason": "transport"}


def test_a_history_the_guest_cannot_read_is_named(tmp_path):
    setup = Setup(tmp_path)
    setup.dnf.entries = None
    assert setup.call("apply")[0] == {"ok": False, "reason": "update-failed", "cause": "history"}
    assert setup.read() == {"pin": "release", "request": DIGEST, "state": "pending", "transaction": 0}


def test_worker_source_loads_with_the_standard_library_only(monkeypatch):
    with open(worker.__file__, encoding="utf-8") as handle:
        source = handle.read()
    real_import = builtins.__import__

    def blocked(name, *arguments, **options):
        if name.split(".")[0] in ("dnf", "ansible", "ansible_collections"):
            raise ImportError(name)
        return real_import(name, *arguments, **options)

    monkeypatch.setattr(builtins, "__import__", blocked)
    monkeypatch.setitem(sys.modules, "dnf", None)
    namespace = {"__name__": "fixed_update_worker"}
    exec(compile(source, "<fixed-update-worker>", "exec"), namespace)  # pylint: disable=exec-used
    assert callable(namespace["main"])
    with pytest.raises(namespace["Refused"]) as refused:
        namespace["load_dnf"]()
    assert (refused.value.reason, refused.value.cause) == ("update-failed", "tooling")
    monkeypatch.setattr(builtins, "__import__", real_import)
    allowed = {"__future__", "json", "os", "re", "stat", "subprocess", "sys", "time"}
    for node in ast.parse(source).body:
        if isinstance(node, ast.Import):
            assert {alias.name.split(".")[0] for alias in node.names} <= allowed
        if isinstance(node, ast.ImportFrom):
            assert node.module.split(".")[0] in allowed


@pytest.mark.parametrize("pin", worker.PINS)
def test_the_release_pin_is_recorded_and_reported(tmp_path, pin):
    setup = Setup(tmp_path, install=[Package("kernel", "5.14.0")], ran=entry(4))
    assert setup.call("apply", pin=pin)[0] == result("upgraded", 4, 2, pin=pin)
    assert setup.read()["pin"] == pin
    assert setup.call("inspect")[0]["result"]["pin"] == pin
    assert setup.call("complete", pin=pin)[0]["result"]["pin"] == pin and setup.read()["state"] == "complete"


def test_a_pin_that_moved_is_recorded_on_the_next_run(tmp_path):
    setup = Setup(tmp_path)
    setup.with_record("upgraded", 5, pin="release")
    setup.dnf.entries.append(entry(5))
    answer = setup.call("apply", pin="contentView")[0]
    assert answer["result"]["pin"] == "contentView" and setup.read()["pin"] == "contentView"
    assert setup.upgrades == []
    setup.call("complete", pin="release")
    assert setup.read() == {"pin": "release", "request": DIGEST, "state": "complete", "transaction": 5}


def test_a_record_without_a_pin_is_foreign(tmp_path):
    setup = Setup(tmp_path)
    setup.with_record("pending", raw=json.dumps({"request": DIGEST, "state": "pending", "transaction": 0}).encode())
    assert setup.call("apply")[0] == {"ok": False, "reason": "update-foreign"}
    assert setup.call("inspect")[0]["result"]["state"] == "foreign"
