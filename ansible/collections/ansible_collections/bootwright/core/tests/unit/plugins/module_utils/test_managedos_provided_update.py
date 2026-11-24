"""Exercise the update boundaries with a fake solver, history and boot identity."""

from contextlib import nullcontext
import os
import sys
from types import SimpleNamespace

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import managedos_provided_update as update
from ansible_collections.bootwright.core.plugins.module_utils.managedos_update_worker import Refused

OWNER = "1" * 64
DIGEST = "2" * 64
BOOT = "11223344-5566-7788-99aa-bbccddeeff00"
SECOND_BOOT = "11223344-5566-7788-99aa-bbccddeeff11"


def package(name, version, release="1.el9_8"):
    return SimpleNamespace(name=name, version=version, release=release, reponame="baseos")


def test_transaction_rejects_an_os_release_replacement_and_newer_tag():
    stable = package("redhat-release", "9.8")
    package_update = package("systemd", "1")
    exact = SimpleNamespace(install_set=[stable, package_update], remove_set=[stable])
    assert update.check_release_transaction(exact, "9.8", "9.8") == [stable, package_update]

    for changed, detail in (
        (SimpleNamespace(install_set=[package("redhat-release", "9.9")], remove_set=[stable]), "9.9@baseos"),
        (SimpleNamespace(install_set=[package_update], remove_set=[stable]), "removed@baseos"),
        (SimpleNamespace(install_set=[package("systemd", "1", "1.el9_9")], remove_set=[]), "9.9@baseos"),
        (SimpleNamespace(install_set=[package("systemd", "1", "1.el10_0")], remove_set=[]), "10.0@baseos"),
    ):
        with pytest.raises(Refused) as error:
            update.check_release_transaction(changed, "9.8", "9.8")
        assert error.value.reason == "update-release-drift"
        assert error.value.detail == detail


def test_a_move_from_an_earlier_minor_requires_the_release_identity_in_the_transaction():
    previous = package("redhat-release", "9.6", "1.el9")
    target = package("redhat-release", "9.8", "1.el9")
    systemd = package("systemd", "1")
    moving = SimpleNamespace(install_set=[target, systemd], remove_set=[previous])
    assert update.check_release_transaction(moving, "9.8", "9.6") == [target, systemd]

    for stuck in (
        SimpleNamespace(install_set=[package("systemd", "1", "1.el9_6")], remove_set=[]),
        SimpleNamespace(install_set=[], remove_set=[]),
    ):
        with pytest.raises(Refused) as error:
            update.check_release_transaction(stuck, "9.8", "9.6")
        assert error.value.reason == "update-release-unavailable"
        assert error.value.detail == "9.6"
    with pytest.raises(Refused) as error:
        update.check_release_transaction(SimpleNamespace(install_set=[package("redhat-release", "9.9")], remove_set=[previous]), "9.8", "9.6")
    assert error.value.reason == "update-release-drift"


def test_failed_or_incomplete_history_never_proves_a_transaction():
    event = SimpleNamespace(is_package=lambda: True, action="upgrade")
    entry = SimpleNamespace(comment=update.COMMENT + OWNER, return_code=1, tid=20, packages=lambda: [event])
    native = SimpleNamespace(
        Base=lambda: nullcontext(SimpleNamespace(
            conf=SimpleNamespace(read=lambda: None),
            history=SimpleNamespace(old=lambda: [entry]),
        )),
        transaction=SimpleNamespace(FORWARD_ACTIONS={"upgrade"}),
    )
    with pytest.raises(Refused) as error:
        update.history(OWNER, native)
    assert error.value.cause == "history"
    assert update.history("0" * 64, native) == (0, 0)
    entry.return_code = 0
    assert update.history(OWNER, native) == (20, 1)


class Receipt:
    digest = DIGEST

    def __init__(self):
        self.writes = []

    def write(self, value):
        self.writes.append(value)


def test_updated_receipt_waits_for_reboot_and_recovers_without_running_again(monkeypatch):
    current = {"boot": BOOT, "history": (0, 0), "fips": "1"}
    monkeypatch.setattr(update, "prove_release", lambda release, **kwargs: release)
    monkeypatch.setattr(update, "boot_id", lambda: current["boot"])
    monkeypatch.setattr(update, "fips_mode", lambda: current["fips"])
    monkeypatch.setattr(update, "history", lambda owner, dnf: current["history"])
    calls = []

    def upgrade(owner, release, installed, dnf):
        calls.append((owner, release))
        current["history"] = (31, 3)
        return True

    monkeypatch.setattr(update, "upgrade", upgrade)
    receipt = Receipt()
    machine = update.Update(receipt, OWNER, "9.8", "withinRelease", dnf=lambda: None)
    assert machine.apply(None) == {"changed": True, "state": "upgraded", "packages": 3, "transaction": 31, "rebooted": False}
    assert receipt.writes[-1]["boot"] == BOOT
    assert machine.observe(receipt.writes[-1])["state"] == "upgraded"

    # The network or controller can stop after the host restarts.
    current["boot"] = SECOND_BOOT
    assert machine.apply(receipt.writes[-1])["rebooted"]
    assert machine.observe(receipt.writes[-1])["state"] == "complete"
    assert machine.apply(receipt.writes[-1])["changed"] is False
    assert calls == [(OWNER, "9.8")]

    current["fips"] = "0"
    with pytest.raises(Refused) as error:
        machine.observe(receipt.writes[-1])
    assert error.value.reason == "update-reboot"


def test_pending_transaction_recovers_from_successful_history(monkeypatch):
    monkeypatch.setattr(update, "prove_release", lambda release, **kwargs: release)
    monkeypatch.setattr(update, "boot_id", lambda: BOOT)
    monkeypatch.setattr(update, "fips_mode", lambda: "0")
    monkeypatch.setattr(update, "history", lambda owner, dnf: (31, 3))
    monkeypatch.setattr(update, "upgrade", lambda *args: pytest.fail("re-executed a completed DNF transaction"))
    receipt = Receipt()
    machine = update.Update(receipt, OWNER, "9.8", "withinRelease", dnf=lambda: None)
    pending = {"request": DIGEST, "boot": BOOT, "fips": "0", "state": "pending", "transaction": 0, "packages": 0}
    assert machine.apply(pending)["state"] == "upgraded"
    assert receipt.writes[-1]["transaction"] == 31


def test_no_package_change_completes_without_reboot(monkeypatch):
    monkeypatch.setattr(update, "prove_release", lambda release, **kwargs: release)
    monkeypatch.setattr(update, "boot_id", lambda: BOOT)
    monkeypatch.setattr(update, "fips_mode", lambda: "0")
    monkeypatch.setattr(update, "history", lambda owner, dnf: (0, 0))
    monkeypatch.setattr(update, "upgrade", lambda *args: False)
    receipt = Receipt()
    machine = update.Update(receipt, OWNER, "9.8", "withinRelease", dnf=lambda: None)
    assert machine.apply(None)["state"] == "complete"
    assert machine.observe(receipt.writes[-1])["rebooted"] is False


def test_os_release_checks_distribution_as_well_as_minor(tmp_path):
    os_release = tmp_path / "os-release"
    os_release.write_text('ID="rhel"\nVERSION_ID="9.8"\n')
    update.prove_release("9.8", path=os_release)
    with pytest.raises(Refused):
        update.prove_release("9.9", path=os_release)
    os_release.write_text('ID="centos"\nVERSION_ID="9.8"\n')
    with pytest.raises(Refused):
        update.prove_release("9.8", path=os_release)


@pytest.mark.parametrize(("found", "admitted"), [
    ("9.6", True), ("9.0", True), ("9.8", True), ("9.9", False), ("8.10", False), ("10.0", False), ("9", False),
])
def test_only_a_move_admits_an_earlier_minor_of_the_same_major(tmp_path, found, admitted):
    os_release = tmp_path / "os-release"
    os_release.write_text('ID="rhel"\nVERSION_ID="%s"\n' % found)
    if admitted:
        assert update.prove_release("9.8", path=os_release, earlier=True) == found
    else:
        with pytest.raises(Refused) as error:
            update.prove_release("9.8", path=os_release, earlier=True)
        assert error.value.reason == "update-release-mismatch"
    if found != "9.8":
        with pytest.raises(Refused) as error:
            update.prove_release("9.8", path=os_release)
        assert error.value.detail == found


def test_a_move_upgrades_from_the_found_minor_and_proves_the_release_after(monkeypatch):
    current = {"release": "9.6", "boot": BOOT, "history": (0, 0)}

    def prove(release, earlier=False):
        if current["release"] != release and not (earlier and update.earlier_minor(current["release"], release)):
            raise Refused("update-release-mismatch", detail=current["release"])
        return current["release"]

    def upgrade(owner, release, installed, dnf):
        assert (release, installed) == ("9.8", "9.6")
        current.update(release="9.8", history=(40, 120))
        return True

    monkeypatch.setattr(update, "prove_release", prove)
    monkeypatch.setattr(update, "boot_id", lambda: current["boot"])
    monkeypatch.setattr(update, "fips_mode", lambda: "0")
    monkeypatch.setattr(update, "history", lambda owner, dnf: current["history"])
    monkeypatch.setattr(update, "upgrade", upgrade)

    within = update.Update(Receipt(), OWNER, "9.8", "withinRelease", dnf=lambda: None)
    with pytest.raises(Refused) as error:
        within.apply(None)
    assert error.value.detail == "9.6"
    assert within.receipt.writes == []

    receipt = Receipt()
    moving = update.Update(receipt, OWNER, "9.8", "toRelease", dnf=lambda: None)
    pending = {"request": DIGEST, "boot": BOOT, "fips": "0", "state": "pending", "transaction": 0, "packages": 0}
    assert moving.observe(pending)["state"] == "pending"
    assert moving.apply(None)["state"] == "upgraded"
    current["boot"] = SECOND_BOOT
    assert moving.apply(receipt.writes[-1])["rebooted"]

    # A completed receipt is proved against the release alone.
    current["release"] = "9.6"
    with pytest.raises(Refused) as error:
        moving.observe(receipt.writes[-1])
    assert error.value.reason == "update-release-mismatch"


class Substitutions(dict):
    def update_from_etc(self, installroot, varsdir):
        assert installroot == "/"
        assert varsdir == ["/etc/dnf/vars"]
        self["releasever"] = "9"


def test_repositories_keep_the_hosts_own_release_substitution():
    # A Satellite content view can serve RHEL only at the major path, rhel9/9/;
    # a minor substitution would ask it for rhel9/9.8/ and fail on metadata.
    events = []
    repo = SimpleNamespace()
    substitutions = Substitutions()
    conf = SimpleNamespace(read=lambda: events.append("conf"), installroot="/", varsdir=["/etc/dnf/vars"],
                           substitutions=substitutions)

    def read_repos():
        assert substitutions == {"releasever": "9"}
        events.append("read-repos")

    base = SimpleNamespace(
        conf=conf, repos=SimpleNamespace(iter_enabled=lambda: [repo]),
        init_plugins=lambda: events.append("init-plugins"), pre_configure_plugins=lambda: events.append("pre-configure"),
        read_all_repos=read_repos, configure_plugins=lambda: events.append("plugins"),
    )
    update.configure(base, OWNER)

    assert events == ["conf", "init-plugins", "pre-configure", "read-repos", "plugins"]
    assert conf.comment == update.COMMENT + OWNER
    assert conf.history_record is True
    assert repo.metadata_expire == 0
    assert repo.skip_if_unavailable is False


def test_only_the_inspected_solver_transaction_can_commit(monkeypatch):
    monkeypatch.setitem(sys.modules, "dnf.callback", SimpleNamespace(TransactionProgress=object))
    monkeypatch.setattr(update, "configure", lambda *args: None)
    monkeypatch.setattr(update, "prove_release", lambda release, **kwargs: release)
    steps = []
    transaction = SimpleNamespace(install_set=[], remove_set=[])

    def download(packages):
        assert packages == transaction.install_set
        steps.append("download")

    base = SimpleNamespace(
        transaction=transaction, fill_sack=lambda **kwargs: steps.append("input"),
        upgrade_all=lambda: steps.append("upgrade"), resolve=lambda: steps.append("resolve"),
        download_packages=download, do_transaction=lambda progress: steps.append("commit"),
    )

    def signatures(selected, packages):
        assert selected is base
        assert packages == transaction.install_set
        steps.append("signatures")

    monkeypatch.setattr(update, "check_signatures", signatures)
    dnf = SimpleNamespace(Base=lambda: nullcontext(base))

    transaction.install_set = [package("redhat-release", "9.9")]
    with pytest.raises(Refused):
        update.upgrade(OWNER, "9.8", "9.8", dnf)
    assert steps == ["input", "upgrade", "resolve"]

    steps.clear()
    transaction.install_set = [package("systemd", "1")]
    assert update.upgrade(OWNER, "9.8", "9.8", dnf)
    assert steps == ["input", "upgrade", "resolve", "download", "signatures", "commit"]


def test_a_metadata_failure_names_the_first_unreadable_repository(monkeypatch):
    monkeypatch.setitem(sys.modules, "dnf.callback", SimpleNamespace(TransactionProgress=object))
    monkeypatch.setattr(update, "configure", lambda *args: None)

    class Error(Exception):
        pass

    def repo(name, readable):
        def load():
            if not readable:
                raise Error(name)
        return SimpleNamespace(id=name, load=load)

    def fill_sack(**kwargs):
        raise Error("metadata")

    enabled = [repo("rhel-9-for-x86_64-baseos-rpms", True), repo("rhel-9-for-x86_64-appstream-rpms", False),
               repo("custom/elk", False)]
    base = SimpleNamespace(fill_sack=fill_sack, repos=SimpleNamespace(iter_enabled=lambda: enabled))
    dnf = SimpleNamespace(Base=lambda: nullcontext(base), exceptions=SimpleNamespace(Error=Error))
    for repositories, detail in (
        (enabled, "metadata@rhel-9-for-x86_64-appstream-rpms"),
        (enabled[2:], "metadata@unknown"),
        (enabled[:1], "metadata"),
    ):
        base.repos = SimpleNamespace(iter_enabled=lambda repositories=repositories: repositories)
        with pytest.raises(Refused) as error:
            update.upgrade(OWNER, "9.8", "9.8", dnf)
        assert (error.value.reason, error.value.cause, error.value.detail) == ("update-unavailable", "metadata", detail)


class RootView:
    """Map only the worker's view of this test directory to root ownership."""

    def __getattr__(self, name):
        return getattr(os, name)

    def fstat(self, descriptor):
        real = os.fstat(descriptor)
        return SimpleNamespace(st_uid=0, st_mode=real.st_mode, st_nlink=real.st_nlink)

    def geteuid(self):
        return 0


def test_receipt_namespace_is_read_only_until_apply_and_locked_across_owners(tmp_path, monkeypatch):
    monkeypatch.setattr(update, "os", RootView())
    root = tmp_path / "bootwright"
    assert update.execute("observe", OWNER, DIGEST, "9.8", "withinRelease", root=root)["state"] == "absent"
    assert not root.exists()

    pending = {"request": DIGEST, "boot": BOOT, "fips": "0", "state": "pending", "transaction": 0, "packages": 0}
    other = "0" * 64
    with update.directory(root, True) as descriptor:
        receipt = update.Receipt(descriptor, OWNER, DIGEST)
        assert receipt.read() is None
        receipt.write(pending)
        assert receipt.read() == pending
        with pytest.raises(Refused) as error:
            update.execute("observe", other, DIGEST, "9.8", "withinRelease", root=root)
        assert error.value.cause == "busy"

    assert update.execute("observe", other, DIGEST, "9.8", "withinRelease", root=root)["state"] == "absent"
    owned_file, = (root / update.DIRECTORY).iterdir()
    assert owned_file.name == OWNER + ".json"
    assert owned_file.stat().st_mode & 0o777 == 0o600


def test_foreign_receipts_are_never_replaced(tmp_path, monkeypatch):
    monkeypatch.setattr(update, "os", RootView())
    root = tmp_path / "bootwright"
    with update.directory(root, True):
        pass
    owned_file = root / update.DIRECTORY / (OWNER + ".json")
    outside = tmp_path / "outside"
    outside.write_bytes(b"another effect")
    owned_file.symlink_to(outside)
    with pytest.raises(Refused) as error:
        update.execute("apply", OWNER, DIGEST, "9.8", "withinRelease", root=root)
    assert error.value.reason == "update-foreign"
    assert outside.read_bytes() == b"another effect"

    owned_file.unlink()
    owned_file.write_bytes(b"not a receipt")
    for mode in (0o644, 0o600):
        owned_file.chmod(mode)
        with pytest.raises(Refused) as error:
            update.execute("apply", OWNER, DIGEST, "9.8", "withinRelease", root=root)
        assert error.value.reason == "update-foreign"
        assert owned_file.read_bytes() == b"not a receipt"

    owned_file.unlink()
    (root / update.DIRECTORY).chmod(0o744)
    with pytest.raises(Refused) as error:
        update.execute("observe", OWNER, DIGEST, "9.8", "withinRelease", root=root)
    assert error.value.reason == "update-foreign"
