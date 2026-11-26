"""A staged destroy blanks a kept libvirt machine's root disk and nothing else.

The reset touches only this context's own domain, shut off, and only the root
image the apply created for it: the Machine's own directory under the media
pool, resolving to itself, its `root.qcow2`, never a link, and the image the
domain's root target reads. It creates the root image again, empty, at its
frozen path and size, and marks it with the staged destroy and step only while
the domain is still shut off, so a reading tells a reset that completed from
one that never ran or was cut short, and a repeated reset changes nothing. A
reset cut short never leaves a mark over a disk it did not blank. The domain
is never redefined, so its boot order, root disk first and optical media
second, sends the next boot of a blank disk to the installer media the restore
inserts. Losing any of this is invisible to lint and to a syntax check.
"""

from __future__ import annotations

import json
import os
import pathlib

import pytest
import yaml
from ansible.module_utils.testing import patch_module_args

from ansible_collections.bootwright.core.plugins.action.substrate_libvirt_machine_protocol import concluded
from ansible_collections.bootwright.core.plugins.module_utils import libvirt_root_reset
from ansible_collections.bootwright.core.plugins.module_utils.libvirt_root_reset import Refused, completed, observe, read_mark, reset
from ansible_collections.bootwright.core.plugins.modules import libvirt_root_reset as module

ROLE = pathlib.Path(__file__).resolve().parents[2] / "roles" / "substrate_libvirt_machine"
URI = "qemu:///system"
DOMAIN = "bootwright-lab-node-1"
UUID = "4c0a4300-aa43-458c-86d7-ac2256d1fc01"
REMOVAL = "op-0123456789abcdef0123456789abcdef"
STEP = "cluster-install-edge"
MARK = REMOVAL + "/" + STEP
DIGEST = "a" * 64


def domain_xml(root, context="lab", machine="node-1"):
    return """<domain type="kvm">
  <name>%s</name>
  <uuid>%s</uuid>
  <metadata>
    <bw:owner xmlns:bw="https://bootwright.io/substrate/v1">
      <bw:context>%s</bw:context>
      <bw:machine>%s</bw:machine>
    </bw:owner>
  </metadata>
  <devices>
    <disk type="file" device="disk"><source file="%s"/><target dev="vda" bus="virtio"/></disk>
  </devices>
</domain>""" % (DOMAIN, UUID, context, machine, root)


class Host:
    """A provider host answering virsh and qemu-img for one domain, recording every command.

    states are the domain's answers to successive state reads, the last one
    repeating; root is the file the domain's root target reads.
    """

    def __init__(self, directory, state="shut off", owner=("lab", "node-1"), create=True, root=None):
        self.states, self.owner, self.create = [state] if isinstance(state, str) else list(state), owner, create
        self.root = str(directory / "root.qcow2") if root is None else root
        self.commands = []

    def __call__(self, argv, check_rc=False, environ_update=None):
        del check_rc, environ_update
        self.commands.append(argv)
        if argv[0] == "/usr/bin/virsh" and argv[3] == "dumpxml":
            return 0, domain_xml(self.root, *self.owner), ""
        if argv[0] == "/usr/bin/virsh" and argv[3] == "domstate":
            state = self.states.pop(0) if len(self.states) > 1 else self.states[0]
            return 0, state + "\n", ""
        if argv[:2] == ["/usr/bin/qemu-img", "create"]:
            if self.create:
                pathlib.Path(argv[4]).write_text("blank %s" % argv[5])
            return (0, "", "") if self.create else (1, "", "error\n")
        if argv[:2] == ["/usr/bin/qemu-img", "info"]:
            content = pathlib.Path(argv[-1]).read_text()
            size = 40 if content.startswith("blank 40G") or "installed" in content else 0
            return 0, json.dumps({"virtual-size": size << 30}), ""
        return 1, "", "error: unexpected\n"

    def writes(self):
        reads = ("dumpxml", "domstate", "list")
        return [argv for argv in self.commands if argv[:2] == ["/usr/bin/qemu-img", "create"] or argv[0] == "/usr/bin/virsh" and argv[3] not in reads]


def request_in(directory):
    return {
        "directory": str(directory), "domain": DOMAIN, "uri": URI, "uuid": UUID,
        "identity": {"context": "lab", "object": "node-1", "block": "machine-node-1"},
        "disks": [{"name": "root", "path": str(directory / "root.qcow2"), "sizeGiB": 40, "target": "vda"},
                  {"name": "data", "path": str(directory / "data.qcow2"), "sizeGiB": 10, "target": "vdb"}],
    }


@pytest.fixture(name="installed")
def fixture_installed(tmp_path, monkeypatch):
    """The Machine's own disk directory under a media pool the test owns, holding an installed root and a data disk."""
    pool = tmp_path.resolve() / "images"
    monkeypatch.setattr(libvirt_root_reset, "IMAGES", str(pool) + "/")
    directory = pool / "lab" / "machines" / "node-1"
    directory.mkdir(parents=True)
    (directory / "root.qcow2").write_text("installed system")
    (directory / "data.qcow2").write_text("data")
    return directory


def test_a_reset_blanks_only_the_root_disk_then_marks_it(installed):
    host = Host(installed)
    observation, changed = reset(host, request_in(installed), REMOVAL, STEP)
    assert changed and observation["mark"] == MARK and observation["disks"] == [{"name": "root", "present": True, "sizeGiB": 40}]
    assert (installed / "root.qcow2").read_text() == "blank 40G"
    assert (installed / "data.qcow2").read_text() == "data"
    assert host.writes() == [["/usr/bin/qemu-img", "create", "-f", "qcow2", str(installed / "root.qcow2"), "40G"]]
    assert (installed / libvirt_root_reset.MARK_NAME).read_text() == MARK + "\n"
    assert (installed / libvirt_root_reset.MARK_NAME).stat().st_mode & 0o777 == 0o600


def test_a_repeated_reset_under_its_own_mark_changes_nothing(installed):
    reset(Host(installed), request_in(installed), REMOVAL, STEP)
    (installed / "root.qcow2").write_text("blank 40G, first boot")
    again = Host(installed)
    observation, changed = reset(again, request_in(installed), REMOVAL, STEP)
    assert not changed and observation["mark"] == MARK and again.writes() == []
    assert (installed / "root.qcow2").read_text() == "blank 40G, first boot"


def test_another_removals_mark_is_dropped_before_the_disk_is_blanked_again(installed):
    reset(Host(installed), request_in(installed), "op-ffffffffffffffffffffffffffffffff", STEP)
    (installed / "root.qcow2").write_text("installed system")
    failing = Host(installed, create=False)
    with pytest.raises(Refused, match="could not create"):
        reset(failing, request_in(installed), REMOVAL, STEP)
    assert read_mark(request_in(installed)) == ""
    assert not (installed / "root.qcow2").exists()


@pytest.mark.parametrize("host, reason", [
    (lambda directory: Host(directory, state="running"), "not shut off"),
    (lambda directory: Host(directory, owner=("other", "node-1")), "not this context's own"),
], ids=["running", "foreign"])
def test_a_reset_refuses_before_any_effect_on_a_running_or_foreign_domain(installed, host, reason):
    refused = host(installed)
    with pytest.raises(Refused, match=reason):
        reset(refused, request_in(installed), REMOVAL, STEP)
    assert refused.writes() == []
    assert (installed / "root.qcow2").read_text() == "installed system"
    assert read_mark(request_in(installed)) == ""


def traversal(directory, request):
    request["directory"] = str(directory / ".." / "node-1")
    request["disks"][0]["path"] = request["directory"] + "/root.qcow2"
    return request


def symlinked_directory(directory, request):
    elsewhere = directory.parent / "elsewhere"
    directory.rename(elsewhere)
    directory.symlink_to(elsewhere, target_is_directory=True)
    return request


def symlinked_root(directory, request):
    (directory / "kept.qcow2").write_text("installed system")
    (directory / "root.qcow2").unlink()
    (directory / "root.qcow2").symlink_to(directory / "kept.qcow2")
    return request


def another_root(directory, request):
    request["disks"][0]["path"] = str(directory / "data.qcow2")
    return request


@pytest.mark.parametrize("arrange, root, reason", [
    (traversal, None, "disk directory is not the Machine's own"),
    (symlinked_directory, None, "resolves elsewhere"),
    (symlinked_root, None, "not a regular file"),
    (another_root, None, "not the Machine's own image"),
    (lambda directory, request: request, "/var/lib/libvirt/images/other.qcow2", "root target does not read the frozen root disk"),
], ids=["traversal", "symlinked-directory", "symlinked-root", "another-root", "mismatched-vda"])
def test_a_reset_refuses_a_root_disk_that_is_not_the_machines_own_with_nothing_deleted(installed, arrange, root, reason):
    request = arrange(installed, request_in(installed))
    refused = Host(installed, root=root)
    files = {path.name: path.read_text() for path in installed.iterdir()}
    with pytest.raises(Refused, match=reason):
        reset(refused, request, REMOVAL, STEP)
    assert refused.writes() == []
    assert {path.name: path.read_text() for path in installed.iterdir()} == files
    assert not os.path.lexists(installed / libvirt_root_reset.MARK_NAME)


def test_a_domain_started_during_the_reset_leaves_the_blank_disk_unmarked(installed):
    host = Host(installed, state=["shut off", "running"])
    with pytest.raises(Refused, match="did not stay shut off"):
        reset(host, request_in(installed), REMOVAL, STEP)
    assert read_mark(request_in(installed)) == ""
    assert not completed(observe(Host(installed, state="running"), request_in(installed)), request_in(installed), MARK)


def created_unmarked(directory):
    (directory / "root.qcow2").write_text("blank 40G")


def marked_over_zero_size(directory):
    (directory / "root.qcow2").write_text("zero")
    (directory / libvirt_root_reset.MARK_NAME).write_text(MARK + "\n")


def deleted_not_recreated(directory):
    (directory / "root.qcow2").unlink()
    (directory / libvirt_root_reset.MARK_NAME).write_text(MARK + "\n")


@pytest.mark.parametrize("cut", [created_unmarked, marked_over_zero_size, deleted_not_recreated],
                         ids=["created-unmarked", "marked-over-zero-size", "deleted-not-recreated"])
def test_a_reset_cut_short_reads_not_completed_and_is_redone(installed, cut):
    cut(installed)
    request = request_in(installed)
    observation = observe(Host(installed), request)
    evidence, refusal = published(observation, True)
    assert not completed(observation, request, MARK) and refusal is None and not evidence["postcondition"]
    host = Host(installed)
    observation, changed = reset(host, request, REMOVAL, STEP)
    assert changed and completed(observation, request, MARK)
    assert host.writes() == [["/usr/bin/qemu-img", "create", "-f", "qcow2", str(installed / "root.qcow2"), "40G"]]
    assert (installed / "root.qcow2").read_text() == "blank 40G" and read_mark(request) == MARK


def test_a_mark_that_is_not_one_reads_as_none(installed):
    (installed / libvirt_root_reset.MARK_NAME).write_text("not a mark\n")
    assert read_mark(request_in(installed)) == ""
    with pytest.raises(ValueError):
        libvirt_root_reset.mark_of("not-an-operation", STEP)


def module_result(arguments, capsys):
    capsys.readouterr()
    with patch_module_args(arguments), pytest.raises(SystemExit):
        module.main()
    return json.loads(capsys.readouterr().out)


def test_check_mode_reads_the_root_disk_and_refuses_a_reset(installed, monkeypatch, capsys):
    host = Host(installed)
    monkeypatch.setattr(module, "command_runner", lambda: host)
    arguments = {"request": request_in(installed), "removal": REMOVAL, "step": STEP, "_ansible_check_mode": True}
    read = module_result(dict(arguments, reset=False), capsys)
    assert read.get("failed") is not True and read["changed"] is False and read["observation"]["mark"] == ""
    refused = module_result(dict(arguments, reset=True), capsys)
    assert refused["failed"] is True and "check mode" in refused["msg"]
    assert host.writes() == [] and (installed / "root.qcow2").read_text() == "installed system"


def published(observation, observed, mark=MARK, size=40):
    return concluded({"root_reset": mark, "root_size": size, "observed": observed, "digest": DIGEST, "observation": observation})


def test_the_protocol_proves_a_reset_only_under_its_mark_at_the_frozen_size_shut_off(installed):
    observation, _changed = reset(Host(installed), request_in(installed), REMOVAL, STEP)
    evidence, refusal = published(observation, False)
    assert refusal is None and evidence["postcondition"] and evidence["rootReset"] == MARK and evidence["system"] == UUID
    assert set(evidence) == {"absent", "answered", "disks", "domain", "owned", "postcondition", "request", "rootReset", "state", "system"}
    _evidence, refusal = published(observation, False, size=41)
    assert "rootReset" in refusal
    _evidence, refusal = published(dict(observation, state="running"), False)
    assert "rootReset" in refusal
    evidence, refusal = published(dict(observation, mark=""), True)
    assert refusal is None and not evidence["postcondition"] and evidence["rootReset"] == ""


def tasks_of(name):
    return yaml.safe_load((ROLE / "tasks" / name).read_text())


def test_the_reset_entry_point_proves_its_target_writes_only_the_root_disk_and_publishes_its_mark():
    tasks = tasks_of("root_reset.yml")
    modules = [key for task in tasks for key in task if key.startswith(("bootwright.", "ansible."))]
    assert modules == ["ansible.builtin.assert", "bootwright.core.substrate_libvirt_machine_protocol", "bootwright.core.hosted_area",
                       "bootwright.core.libvirt_root_reset", "bootwright.core.substrate_libvirt_machine_protocol"]
    asserted = "\n".join(tasks[0]["ansible.builtin.assert"]["that"])
    assert "'/var/lib/libvirt/images/bootwright/'" in asserted and "'/machines/'" in asserted and "'/root.qcow2'" in asserted
    assert tasks[2]["bootwright.core.hosted_area"]["state"] == "proved"
    assert tasks[3]["bootwright.core.libvirt_root_reset"]["reset"] is True
    assert "observed" not in tasks[4]["bootwright.core.substrate_libvirt_machine_protocol"]


def test_the_observation_reads_the_root_disk_without_writing_it():
    readings = [task["bootwright.core.libvirt_root_reset"] for task in tasks_of("observe.yml") if "bootwright.core.libvirt_root_reset" in task]
    assert readings and all(reading["reset"] is False for reading in readings)


def test_a_blank_root_disk_falls_through_to_the_inserted_media():
    template = (ROLE / "templates" / "domain.xml.j2").read_text()
    assert template.index('<boot dev="hd"/>') < template.index('<boot dev="cdrom"/>')
    entry_points = [task for name in ("root_reset.yml", "observe.yml") for task in tasks_of(name)]
    assert not any("virsh" in json.dumps(task) or "domain.xml" in json.dumps(task) for task in entry_points)
