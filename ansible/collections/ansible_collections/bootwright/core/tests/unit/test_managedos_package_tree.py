"""Shared publication replay and interrupted removal use the production tasks."""
from __future__ import annotations

import errno
import os
import pathlib
import shutil
from types import SimpleNamespace

import pytest
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar
from ansible_collections.bootwright.core.plugins.module_utils.package_tree import observe
from ansible_collections.bootwright.core.plugins.action import managedos_tree_protocol as protocol

ROLE = pathlib.Path(__file__).resolve().parents[2] / "roles" / "managedos_package_tree"
LOADER = DataLoader()
SHA = "d" * 64


class Interrupted(Exception):
    pass


class Host:
    def __init__(self, root, monkeypatch):
        self.root, self.tree = root, root / "public" / "os" / "profile" / "tree"
        self.stage = pathlib.Path(str(self.tree) + ".staging")
        self.extracts, self.kill, self.evidence = 0, False, []
        self.values = dict(LOADER.load_from_file(str(ROLE / "defaults/main.yml"), trusted_as_template=True))
        self.values.update({"managedos_package_tree_media": str(root / "source.iso"), "bootwright_os_tree_digest": "1" * 64, "bootwright_os_tree_material": {},
                           "bootwright_os_tree_request": {"publication": {"path": str(self.tree)}, "media": {"name": "source.iso", "sha256": SHA}}})
        monkeypatch.setattr(protocol, "emit", lambda value, **kwargs: self.evidence.append(value))

    def run(self, name):
        self.tasks(LOADER.load_from_file(str(ROLE / "tasks" / name), trusted_as_template=True))

    def tasks(self, tasks, inherited=()):
        for task in tasks:
            condition = task.get("when", [])
            conditions = list(inherited) + (condition if isinstance(condition, list) else [condition])
            templar = Templar(loader=LOADER, variables=self.values)
            if "block" in task:
                if all(templar.evaluate_conditional(item) for item in conditions):
                    try:
                        self.tasks(task["block"])
                    finally:
                        self.tasks(task.get("always", []))
                continue
            key = next((key for key in task if key.startswith(("ansible.builtin.", "bootwright.core."))), None)
            if not key or key.endswith(("assert", "uri")):
                continue
            if not all(templar.evaluate_conditional(item) for item in conditions):
                result = {"skipped": True, "changed": False}
            elif "loop" in task:
                results = []
                for item in templar.template(task["loop"]):
                    self.values["item"] = item
                    args = Templar(loader=LOADER, variables=self.values).template(task[key])
                    results.append(self.invoke(key, args))
                result = {"changed": any(value["changed"] for value in results), "results": results}
            else:
                result = self.invoke(key, templar.template(task[key]))
            if "register" in task:
                self.values[task["register"]] = result

    def invoke(self, key, args):
        if key.endswith("managedos_tree_inspect"):
            return {"changed": False, "observation": observe(str(self.tree), SHA)}
        if key.endswith("managedos_tree_protocol"):
            action = protocol.ActionModule.__new__(protocol.ActionModule)
            action._task = SimpleNamespace(args=args)
            result = action.run(task_vars={})
            assert not result.get("failed"), result
            return result
        if key.endswith("stat"):
            return {"changed": False, "stat": {"isreg": True, "exists": pathlib.Path(args["path"]).exists()}}
        if key.endswith("copy"):
            pathlib.Path(args["dest"]).write_text(args["content"])
            return {"changed": True}
        if key.endswith("file"):
            return self.file(args)
        assert key.endswith("command"), key
        argv = args["argv"]
        if argv[0].endswith("xorriso"):
            self.extracts += 1
            self.stage.mkdir(parents=True, exist_ok=True)
            (self.stage / ".treeinfo").write_text("complete")
            (self.stage / "Packages").mkdir()
        elif argv[0].endswith("mv"):
            os.rename(argv[-2], argv[-1])
        elif argv[0].endswith("rmdir"):
            try:
                os.rmdir(argv[-1])
            except OSError as error:
                if error.errno != errno.ENOTEMPTY:
                    raise
        else:
            assert argv[0].endswith("chcon")
        return {"changed": True}

    def file(self, args):
        path = pathlib.Path(args["path"])
        assert path.is_relative_to(self.root)
        exists = path.exists()
        if args["state"] == "directory":
            path.mkdir(parents=True, exist_ok=True)
            return {"changed": not exists}
        if self.kill and path == self.tree:
            self.kill = False
            raise Interrupted()
        if path.is_dir():
            shutil.rmtree(path)
        elif exists:
            path.unlink()
        return {"changed": exists}


@pytest.mark.parametrize("partial", ["staging", "unmarked", "none"])
def test_publication_recovers_partial_work_and_replays_without_extracting(tmp_path, monkeypatch, partial):
    host = Host(tmp_path, monkeypatch)
    if partial != "none":
        path = host.stage if partial == "staging" else host.tree
        path.mkdir(parents=True)
        (path / "partial").write_text("partial")
    host.run("apply.yml")
    assert observe(str(host.tree), SHA) == {"content": True, "tree": True, "identity": SHA, "staging": False}
    host.run("apply.yml")
    assert host.extracts == 1
    assert host.evidence[-1]["outcome"] == "unchanged"


def test_interrupted_removal_withdraws_marker_and_retry_proves_absence(tmp_path, monkeypatch):
    host = Host(tmp_path, monkeypatch)
    host.run("apply.yml")
    host.kill = True
    with pytest.raises(Interrupted):
        host.run("destroy.yml")
    assert not (host.tree / ".treeinfo").exists()
    assert observe(str(host.tree), SHA)["content"]
    host.run("destroy.yml")
    assert not host.tree.exists() and not host.stage.exists()
    assert host.evidence[-1]["evidence"]["postcondition"]


def test_inspection_refuses_a_link_or_unreadable_identity(tmp_path):
    tree = tmp_path / "tree"
    tree.symlink_to(tmp_path, target_is_directory=True)
    with pytest.raises(OSError):
        observe(str(tree), SHA)
    tree.unlink()
    tree.mkdir()
    (tree / ".bootwright-tree-identity").write_text("malformed")
    with pytest.raises(OSError):
        observe(str(tree), SHA)


@pytest.mark.parametrize("sibling", [False, True])
def test_removal_takes_only_empty_profile_directory(tmp_path, monkeypatch, sibling):
    host = Host(tmp_path, monkeypatch)
    host.tree.parent.mkdir(parents=True)
    if sibling:
        (host.tree.parent / "unrelated").write_text("preserve")
    host.run("destroy.yml")
    assert host.tree.parent.exists() is sibling
    assert host.evidence[-1]["outcome"] == ("unchanged" if sibling else "changed")


def test_removal_withdraws_drifted_payload_only_from_owned_path(tmp_path, monkeypatch):
    host = Host(tmp_path, monkeypatch)
    host.run("apply.yml")
    (host.tree / ".bootwright-tree-identity").write_text("a" * 64 + "\n")
    (host.tree.parent / "unrelated").write_text("preserve")
    host.run("destroy.yml")
    assert not host.tree.exists()
    assert (host.tree.parent / "unrelated").read_text() == "preserve"
    assert host.evidence[-1]["evidence"]["postcondition"]
