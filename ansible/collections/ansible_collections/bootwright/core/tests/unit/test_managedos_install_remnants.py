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
        })
        self.emitted = []
        self.reads = {}
        monkeypatch.setattr(protocol, "emit", lambda message, **kwargs: self.emitted.append(message))

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
                result = getattr(self, module.rsplit(".", 1)[-1])(templar.template(task[module]))
            if "register" in task:
                self.variables[task["register"]] = result
            if task.get("name") == through:
                return True
        return False

    def inside(self, path):
        path = pathlib.Path(path)
        assert path.is_relative_to(self.root), "a task would act on %s, outside the test's host" % path
        return path

    def managedos_install_inspect(self, arguments):
        return {"changed": False, "observation": observe(arguments["request"], arguments["staging"], arguments["work"])}

    def managedos_install_protocol(self, arguments):
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
            output = self.inside(argv[-1])
            assert not output.exists(), "%s already exists" % output
            output.write_bytes(b"built")
        else:
            assert argv[0] == "/usr/bin/chcon", argv
        return {"changed": True, "rc": 0}

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
