"""Interrupted image work belongs to one machine; observations use private scratch."""

from __future__ import annotations

import os
import pathlib
import shutil
from types import SimpleNamespace

import pytest
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar

from ansible_collections.bootwright.core.plugins.action import managedos_install_protocol as protocol
from ansible_collections.bootwright.core.plugins.modules.managedos_install_inspect import observe
from ansible_collections.bootwright.core.plugins.modules.lifecycle_scratch import manage

ROLE = pathlib.Path(__file__).resolve().parents[2] / "roles" / "managedos_install_anaconda"
LOADER = DataLoader()
DIGEST = "1" * 64
INSPECT = "bootwright.core.managedos_install_inspect"
PROTOCOL = "bootwright.core.managedos_install_protocol"
FILE = "ansible.builtin.file"
COMMAND = "ansible.builtin.command"
INCLUDE = "ansible.builtin.include_tasks"
STAT = "ansible.builtin.stat"
COPY = "ansible.builtin.copy"
SCRATCH = "bootwright.core.lifecycle_scratch"
MODELLED = (INSPECT, PROTOCOL, FILE, COMMAND, INCLUDE, STAT, COPY, SCRATCH)
BOOT = {"name": "rhel-9.8-x86_64-boot.iso", "sha256": "b" * 64, "size": 1105199104}
DVD = {"name": "rhel-9.8-x86_64-dvd.iso", "sha256": "d" * 64, "size": 13107200000}
BUILD = "Build this machine's own installer image"


class ReadFailed(Exception):
    """A read of the machine failed."""


class TaskFailed(Exception):
    """A task failed, by its failed_when or by a command's nonzero exit."""


def tasks(name):
    return [task for task in LOADER.load_from_file(str(ROLE / "tasks" / name), trusted_as_template=True)
            if isinstance(task, dict)]


def names(directory):
    return {entry.name for entry in directory.iterdir()}


class Host:
    """A directory standing in for the host an installation publishes through,
    and the role's tasks run over it."""

    def __init__(self, root, monkeypatch, machine="rhel-01", job=1):
        self.root = root
        self.tree = root / "public" / "os" / "rhel-9-8" / "tree"
        self.staging = self.tree.parent / "tree.staging"
        self.image = root / "public" / "os" / machine / "install.iso"
        self.parent = root / "var" / "lib" / "bootwright-install"
        self.work = self.parent / "lab" / machine / "work"
        self.scratch_root = root / "var" / "tmp" / ("bootwright-run-scratch-%d-1" % job)
        self.scratch_root.parent.mkdir(parents=True, exist_ok=True)
        self.scratch = self.scratch_root / "managedos-install"
        self.variables = dict(LOADER.load_from_file(str(ROLE / "defaults" / "main.yml"), trusted_as_template=True))
        self.variables.update({
            "bootwright_run_scratch": str(self.scratch_root),
            "bootwright_os_install_digest": DIGEST,
            "bootwright_os_install_material": {},
            "bootwright_os_install_request": {
                "identity": {"context": "lab", "object": machine},
                "placement": {"connection": "local"},
                "address": "198.51.100.11",
                "bootMedia": dict(BOOT),
                "image": {"path": str(self.image)},
                "installerArguments": ["ifname=enp1s0:52:54:00:00:00:01", "ifname=enp2s0:52:54:00:00:00:02"],
                "tree": {"path": str(self.tree)},
                "treeMedia": dict(DVD),
            },
            "managedos_install_anaconda_media": str(root / "media"),
            "managedos_install_anaconda_media_present": {"results": [
                {"item": {"size": media["size"], "sha256": media["sha256"]},
                 "stat": {"exists": True, "isreg": True, "size": media["size"], "checksum": media["sha256"]}}
                for media in (BOOT, DVD)
            ]},
            "managedos_install_anaconda_work_parent": str(self.parent),
            "managedos_install_anaconda_work": str(self.work),
            "managedos_install_anaconda_network_result": {"network": True, "networkReboot": True,
                                                          "networkDifferences": [], "changed": False},
        })
        self.emitted = []
        self.built = None
        self.reads = {}
        self.after = {}
        self.environment = {}
        self.monkeypatch = monkeypatch

    def run(self, name, through=None):
        """Run one task file in order, stopping after the task named through."""
        reached = self.perform(tasks(name), [], through)
        assert reached or through is None, "%s holds no task named %s" % (name, through)

    def perform(self, listed, inherited, through):
        """Run a list of tasks in order, and report whether the task named
        through ran."""
        for task in listed:
            if not isinstance(task, dict):
                continue
            conditions = task.get("when", [])
            conditions = inherited + (conditions if isinstance(conditions, list) else [conditions])
            if "block" in task:
                # This publication model assumes the read-only policy check
                # passed. Its early refusal and rescue have separate tests;
                # no native SSH process is part of this filesystem model.
                if len(task["block"]) == 1 and "bootwright.core.ssh_key_policy" in task["block"][0]:
                    continue
                assert "rescue" not in task, task.get("name")
                try:
                    reached = self.perform(task["block"], conditions, through)
                finally:
                    self.perform(task.get("always") or [], conditions, None)
                if reached:
                    return True
                continue
            module = next((candidate for candidate in MODELLED if candidate in task), None)
            if module is None or "loop" in task:
                continue
            if module == COPY and not str(task[COPY].get("dest", "")).endswith("/.bootwright-tree-identity"):
                continue
            templar = Templar(loader=LOADER, variables=self.variables)
            if not all(templar.evaluate_conditional(condition) for condition in conditions):
                result = {"changed": False, "skipped": True}
            else:
                self.environment = task.get("environment") or {}
                result = getattr(self, module.rsplit(".", 1)[-1])(templar.template(task[module]))
                self.failed(task, result)
            if "register" in task:
                self.variables[task["register"]] = result
            self.after.get(task.get("name"), lambda: None)()
            if task.get("name") == through:
                return True
        return False

    def failed(self, task, result):
        """Fail a task as ansible-core would: by its failed_when, evaluated
        over its registered result, or else by a command's nonzero exit."""
        if "failed_when" not in task:
            if result.get("rc", 0) != 0:
                raise TaskFailed("%s: %s" % (task.get("name"), result.get("stderr")))
            return
        variables = dict(self.variables)
        if "register" in task:
            variables[task["register"]] = result
        conditions = task["failed_when"]
        conditions = conditions if isinstance(conditions, list) else [conditions]
        templar = Templar(loader=LOADER, variables=variables)
        if all(templar.evaluate_conditional(condition) for condition in conditions):
            raise TaskFailed("%s: %s" % (task.get("name"), result.get("stderr")))

    def inside(self, path):
        path = pathlib.Path(path)
        assert path.is_relative_to(self.root), "a task would act on %s, outside the test's host" % path
        return path

    def managedos_install_inspect(self, arguments):
        return {"changed": False, "observation": observe(arguments["request"], arguments["staging"], arguments["work"])}

    def managedos_install_protocol(self, arguments):
        self.monkeypatch.setattr(protocol, "emit", lambda message, **kwargs: self.emitted.append(message))
        module = protocol.ActionModule.__new__(protocol.ActionModule)
        module._task = SimpleNamespace(args=arguments)
        result = module.run(task_vars={})
        assert not result.get("failed"), result.get("msg")
        return result

    def file(self, arguments):
        path = self.inside(arguments["path"])
        if arguments.get("state") == "directory":
            existed = path.is_dir()
            path.mkdir(parents=True, exist_ok=True)
            return {"changed": not existed}
        assert arguments.get("state") == "absent", arguments
        if path.is_dir() and not path.is_symlink():
            shutil.rmtree(path)
        elif os.path.lexists(path):
            path.unlink()
        else:
            return {"changed": False}
        return {"changed": True}

    def copy(self, arguments):
        path = self.inside(arguments["dest"])
        path.write_text(arguments["content"])
        return {"changed": True}

    def include_tasks(self, name):
        self.reads.get(name, lambda: None)()
        return {"changed": False}

    def stat(self, arguments):
        assert not arguments.get("follow"), arguments
        path = self.inside(arguments["path"])
        if not os.path.lexists(path):
            return {"changed": False, "stat": {"exists": False}}
        return {"changed": False, "stat": {"exists": True, "isdir": path.is_dir() and not path.is_symlink()}}

    def lifecycle_scratch(self, arguments):
        path = manage(arguments["root"], arguments["scope"], arguments.get("state", "present"),
                      str(self.scratch_root.parent), os.getuid())
        return {"changed": False, "path": path}

    def command(self, arguments):
        argv = [str(argument) for argument in arguments["argv"]]
        if argv[0] == "/usr/bin/mv":
            os.rename(self.inside(argv[-2]), self.inside(argv[-1]))
        elif argv[0] == "/usr/bin/mkksiso":
            self.built = argv
            output = self.inside(argv[-1])
            assert not output.exists(), "%s already exists" % output
            output.write_bytes(b"built")
        elif argv[0] == "/usr/bin/rmdir":
            # GNU rmdir --ignore-fail-on-non-empty leaves a directory that is
            # not empty and succeeds; any other failure still fails, and a
            # missing directory exits 1 naming strerror(ENOENT), as coreutils
            # src/rmdir.c ignorable_failure and remove_parents read: only
            # ENOTEMPTY and EEXIST are ignored. LC_ALL=C keeps that text.
            assert argv[1:-1] == ["--ignore-fail-on-non-empty"], argv
            assert self.environment.get("LC_ALL") == "C", "rmdir's diagnostic would be localized"
            directory = self.inside(argv[-1])
            assert directory != self.parent, "a removal would take the shared work root"
            if not os.path.lexists(directory):
                return {"changed": True, "rc": 1, "stdout": "",
                        "stderr": "/usr/bin/rmdir: failed to remove '%s': No such file or directory" % directory}
            if not any(directory.iterdir()):
                directory.rmdir()
        else:
            assert argv[0] == "/usr/bin/chcon", argv
        return {"changed": True, "rc": 0, "stdout": "", "stderr": ""}

    def completion(self):
        completed = [message for message in self.emitted if message.get("phase") == "completed"]
        assert len(completed) == 1, self.emitted
        return completed[0]


def killed_build(host):
    host.work.mkdir(parents=True)
    (host.work / "install.iso").write_bytes(b"stale")


def test_an_apply_clears_only_its_own_interrupted_build(tmp_path, monkeypatch):
    first, second = Host(tmp_path, monkeypatch), Host(tmp_path, monkeypatch, "other", 2)
    killed_build(first)
    killed_build(second)
    first.run("apply.yml", through=BUILD)
    assert (first.work / "install.iso").read_bytes() == b"built"
    assert (second.work / "install.iso").read_bytes() == b"stale"
    second.run("apply.yml", through=BUILD)
    assert (first.work / "install.iso").read_bytes() == b"built"
    assert (second.work / "install.iso").read_bytes() == b"built"
    second.run("destroy.yml")
    assert first.work.exists() and not second.work.exists()


def test_the_installer_image_boots_with_the_frozen_installer_arguments(tmp_path, monkeypatch):
    host = Host(tmp_path, monkeypatch)
    host.run("apply.yml", through=BUILD)
    assert host.built == [
        "/usr/bin/mkksiso", "--ks", str(host.work / "ks.cfg"),
        "--cmdline", "ifname=enp1s0:52:54:00:00:00:01 ifname=enp2s0:52:54:00:00:00:02",
        "--rm-args", "rd.live.check", str(tmp_path / "media" / BOOT["name"]), str(host.work / "install.iso"),
    ]


def test_an_install_never_changes_the_shared_tree(tmp_path, monkeypatch):
    host = Host(tmp_path, monkeypatch)
    host.tree.mkdir(parents=True)
    (host.tree / ".treeinfo").write_text("[general]\n")
    (host.tree / ".bootwright-tree-identity").write_text(DVD["sha256"] + "\n")
    host.staging.mkdir()
    (host.staging / "partial").write_text("other publication")
    before = {str(path): path.read_bytes() for path in host.tree.parent.rglob("*") if path.is_file()}
    host.run("apply.yml", through=BUILD)
    host.run("destroy.yml")
    after = {str(path): path.read_bytes() for path in host.tree.parent.rglob("*") if path.is_file()}
    assert before == after
    assert host.completion()["evidence"]["postcondition"] is True


def test_an_apply_completion_reads_after_its_work_area_is_gone():
    def flatten(listed):
        for task in listed:
            if "block" in task:
                yield from flatten(task["block"])
                yield from flatten(task.get("always", []))
            else:
                yield task
    applied = list(flatten(tasks("apply.yml")))
    removals = [index for index, task in enumerate(applied)
                if (task.get(FILE) or {}).get("state") == "absent"
                and task[FILE].get("path") == "{{ managedos_install_anaconda_work }}"]
    readings = [index for index, task in enumerate(applied) if INSPECT in task]
    assert len(removals) == 2 and len(readings) == 2
    assert readings[0] < removals[0] < removals[1] < readings[1]


@pytest.mark.parametrize("left", [False, True])
def test_a_removal_reports_its_own_work_only(tmp_path, monkeypatch, left):
    host = Host(tmp_path, monkeypatch)
    if left:
        killed_build(host)
    host.run("destroy.yml")
    assert not host.work.exists()
    assert host.completion()["outcome"] == ("changed" if left else "unchanged")
    assert host.completion()["evidence"]["postcondition"] is True


def test_a_removal_takes_back_the_directories_its_apply_created(tmp_path, monkeypatch):
    host = Host(tmp_path, monkeypatch)
    host.parent.mkdir(parents=True)
    host.run("apply.yml", through=BUILD)
    assert host.work.is_dir()
    host.run("destroy.yml")
    assert not os.path.lexists(host.work.parent) and not os.path.lexists(host.work.parent.parent)
    assert host.parent.is_dir()
    assert host.completion()["outcome"] == "changed"
    assert host.completion()["evidence"]["postcondition"] is True


# Directories an earlier removal left empty are still this Machine's to take
# back, so taking them back is a change even when no work area remains.
def test_a_removal_of_only_empty_directories_is_a_change(tmp_path, monkeypatch):
    host = Host(tmp_path, monkeypatch)
    host.work.parent.mkdir(parents=True)
    host.run("destroy.yml")
    assert not os.path.lexists(host.work.parent.parent) and host.parent.is_dir()
    assert host.completion()["outcome"] == "changed"


def test_a_removal_leaves_a_sibling_machines_work(tmp_path, monkeypatch):
    first, second = Host(tmp_path, monkeypatch), Host(tmp_path, monkeypatch, "rhel-02", 2)
    first.run("apply.yml", through=BUILD)
    second.run("apply.yml", through=BUILD)
    first.run("destroy.yml")
    assert not os.path.lexists(first.work.parent)
    assert second.work.parent.parent.is_dir()
    assert (second.work / "install.iso").read_bytes() == b"built"


# Sibling Machines' removals run concurrently on one installer host, so both can
# read the context directory present before either removes it; the one whose
# rmdir then finds it gone already holds the absence it wants.
def test_sibling_removals_racing_for_the_context_directory_both_complete(tmp_path, monkeypatch):
    first, second = Host(tmp_path, monkeypatch), Host(tmp_path, monkeypatch, "rhel-02", 2)
    first.run("apply.yml", through=BUILD)
    second.run("apply.yml", through=BUILD)
    second.after["Read this context's work directory before empty directory removal"] = (
        lambda: first.run("destroy.yml"))
    second.run("destroy.yml")
    assert first.variables["managedos_install_anaconda_context_dir_removed"]["rc"] == 0
    assert second.variables["managedos_install_anaconda_context_dir_before"]["stat"]["isdir"] is True
    assert second.variables["managedos_install_anaconda_context_dir_removed"]["rc"] == 1
    assert not os.path.lexists(first.work.parent.parent) and first.parent.is_dir()
    for host in (first, second):
        assert host.completion()["evidence"]["postcondition"] is True


# Any other rmdir failure is still the removal's failure.
def test_a_removal_fails_when_rmdir_fails_otherwise(tmp_path, monkeypatch):
    host = Host(tmp_path, monkeypatch)
    host.work.parent.mkdir(parents=True)
    denied = host.command

    def refused(arguments):
        if arguments["argv"][0] == "/usr/bin/rmdir":
            return {"changed": True, "rc": 1, "stdout": "",
                    "stderr": "/usr/bin/rmdir: failed to remove '%s': Permission denied" % arguments["argv"][-1]}
        return denied(arguments)

    host.command = refused
    with pytest.raises(TaskFailed, match="Permission denied"):
        host.run("destroy.yml")
    assert host.work.parent.is_dir()


# Observation leaves durable work as found and cleans its own connection files.
def test_an_observation_leaves_no_work_area_it_did_not_find(tmp_path, monkeypatch):
    host = Host(tmp_path, monkeypatch)
    host.run("observe.yml")
    assert not os.path.lexists(host.work)
    assert not host.scratch.exists()
    assert host.completion()["evidence"]["work"] is False


def test_an_observation_whose_read_fails_leaves_no_work_area_it_did_not_find(tmp_path, monkeypatch):
    host = Host(tmp_path, monkeypatch)

    def pinned_then_failed():
        (host.scratch / "known_hosts").write_text("198.51.100.11 ssh-ed25519 AAAAHOST\n")
        raise ReadFailed()

    host.reads["identity.yml"] = pinned_then_failed
    with pytest.raises(ReadFailed):
        host.run("observe.yml")
    assert not os.path.lexists(host.work)
    assert not host.scratch.exists()


# A work area it found is what an interrupted attempt left, which the evidence
# reports and a removal takes back, so the observation leaves it as it was.
def test_an_observation_leaves_the_work_area_it_found_as_it_was(tmp_path, monkeypatch):
    host = Host(tmp_path, monkeypatch)
    killed_build(host)
    host.run("observe.yml")
    assert names(host.work) == {"install.iso"}
    assert host.completion()["evidence"]["work"] is True
