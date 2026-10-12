"""Shared publication replay, refusal and interrupted removal use the production tasks.

The tasks run against a host rooted in the test's directory: every absolute
path the frozen request and the role's defaults name is mapped beneath it, so
the request keeps the exact shape the role validates. Each assert is evaluated
as Ansible does, so a refusal the role makes stops the run where it would. The
stat module's result is read from the file it names: given follow false it
reports the link's own type, and given get_checksum with checksum_algorithm
sha256 a regular file's SHA-256 as checksum (ansible/modules/stat.py, main,
ansible-core 2.21).
"""
from __future__ import annotations

import errno
import hashlib
import os
import pathlib
import shutil
import stat
from types import SimpleNamespace

import pytest
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar
from ansible_collections.bootwright.core.plugins.module_utils.package_tree import observe
from ansible_collections.bootwright.core.plugins.action import managedos_tree_protocol as protocol

ROLE = pathlib.Path(__file__).resolve().parents[2] / "roles" / "managedos_package_tree"
LOADER = DataLoader()
SOURCE = b"the frozen installation DVD"
SHA = hashlib.sha256(SOURCE).hexdigest()
PUBLICATION = "/var/lib/bootwright-services/lab/public/os/rhel-9-8/tree"
MEDIA = "rhel-9.8-x86_64-dvd.iso"
MAPPED = ("/var/lib/bootwright-services/", "/var/lib/bootwright/media/")


class Interrupted(Exception):
    pass


class Refused(Exception):
    """An assert of the role failed, which ends the run as Ansible would."""


class Host:
    def __init__(self, root, monkeypatch):
        self.root = root
        self.tree = self.local(PUBLICATION)
        self.stage = pathlib.Path(str(self.tree) + ".staging")
        self.source = self.local("/var/lib/bootwright/media/" + MEDIA)
        self.source.parent.mkdir(parents=True)
        self.source.write_bytes(SOURCE)
        self.extracts, self.kill, self.evidence = 0, False, []
        self.values = dict(LOADER.load_from_file(str(ROLE / "defaults/main.yml"), trusted_as_template=True))
        self.values.update({"bootwright_os_tree_digest": "1" * 64, "bootwright_os_tree_material": {},
                            "bootwright_os_tree_request": {
                                "version": "os-package-tree-v1",
                                "identity": {"context": "lab", "profile": "rhel-9-8"},
                                "publication": {"path": PUBLICATION, "url": "https://artifacts.example.test/os/rhel-9-8/tree"},
                                "media": {"name": MEDIA, "sha256": SHA, "size": len(SOURCE)}}})
        monkeypatch.setattr(protocol, "emit", lambda value, **kwargs: self.evidence.append(value))

    def local(self, path):
        """The test host's path for one absolute path the role names."""
        path = str(path)
        assert path.startswith(MAPPED), "a task would act on %s, outside the paths the role owns" % path
        return self.root / path.lstrip("/")

    def run(self, name):
        self.tasks(LOADER.load_from_file(str(ROLE / "tasks" / name), trusted_as_template=True))

    def refusals(self):
        return [record["reason"] for record in self.evidence if record.get("phase") == "refused"]

    def completed(self):
        return [record for record in self.evidence if record.get("phase") == "completed"]

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
            if not key or key.endswith("uri"):
                continue
            if not all(templar.evaluate_conditional(item) for item in conditions):
                result = {"skipped": True, "changed": False}
            elif key.endswith("assert"):
                failed = [item for item in task[key]["that"] if not templar.evaluate_conditional(item)]
                if failed:
                    raise Refused("%s: %s" % (task.get("name"), failed))
                result = {"changed": False}
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
            request = args["request"]
            return {"changed": False, "observation": observe(str(self.local(request["publication"]["path"])), request["media"]["sha256"])}
        if key.endswith("managedos_tree_protocol"):
            action = protocol.ActionModule.__new__(protocol.ActionModule)
            action._task = SimpleNamespace(args=args)
            result = action.run(task_vars={})
            assert not result.get("failed"), result
            return result
        if key.endswith("stat"):
            return {"changed": False, "stat": self.stat(args)}
        if key.endswith("copy"):
            self.local(args["dest"]).write_text(args["content"])
            return {"changed": True}
        if key.endswith("file"):
            return self.file(args)
        assert key.endswith("command"), key
        argv = args["argv"]
        if argv[0].endswith("xorriso"):
            assert self.local(argv[argv.index("-indev") + 1]) == self.source
            self.extracts += 1
            self.stage.mkdir(parents=True, exist_ok=True)
            (self.stage / ".treeinfo").write_text("complete")
            (self.stage / "Packages").mkdir()
        elif argv[0].endswith("mv"):
            os.rename(self.local(argv[-2]), self.local(argv[-1]))
        elif argv[0].endswith("rmdir"):
            try:
                os.rmdir(self.local(argv[-1]))
            except OSError as error:
                if error.errno != errno.ENOTEMPTY:
                    raise
        else:
            assert argv[0].endswith("chcon")
        return {"changed": True}

    def stat(self, args):
        assert args.get("follow") is False, args
        path = self.local(args["path"])
        try:
            info = os.lstat(path)
        except FileNotFoundError:
            return {"exists": False}
        result = {"exists": True, "isreg": stat.S_ISREG(info.st_mode), "islnk": stat.S_ISLNK(info.st_mode),
                  "isdir": stat.S_ISDIR(info.st_mode), "size": info.st_size}
        if args.get("get_checksum") and result["isreg"]:
            assert args.get("checksum_algorithm") == "sha256", args
            result["checksum"] = hashlib.sha256(path.read_bytes()).hexdigest()
        return result

    def file(self, args):
        path = self.local(args["path"])
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


def contents(directory):
    return {str(path.relative_to(directory)): path.read_bytes() for path in sorted(directory.rglob("*")) if path.is_file()}


def test_apply_refuses_a_tree_published_from_another_source(tmp_path, monkeypatch):
    host = Host(tmp_path, monkeypatch)
    host.tree.mkdir(parents=True)
    (host.tree / ".treeinfo").write_text("[general]\n")
    (host.tree / ".bootwright-tree-identity").write_text("a" * 64 + "\n")
    before = contents(host.tree)
    with pytest.raises(Refused, match="Refuse an existing tree from another source"):
        host.run("apply.yml")
    assert host.refusals() == ["tree-source-drift"]
    assert host.extracts == 0 and not host.stage.exists()
    assert contents(host.tree) == before
    assert not host.completed()


@pytest.mark.parametrize("change", ["size", "checksum", "link", "absent"])
def test_apply_refuses_source_media_changed_after_planning_before_extraction(tmp_path, monkeypatch, change):
    host = Host(tmp_path, monkeypatch)
    if change == "size":
        host.source.write_bytes(SOURCE + b"appended")
    elif change == "checksum":
        host.source.write_bytes(SOURCE.upper())
        assert len(SOURCE.upper()) == len(SOURCE)
    elif change == "link":
        frozen = host.source.with_name("elsewhere.iso")
        host.source.rename(frozen)
        host.source.symlink_to(frozen)
    else:
        host.source.unlink()
    with pytest.raises(Refused, match="Refuse source media that changed after planning"):
        host.run("apply.yml")
    assert host.refusals() == ["media-changed-tree"]
    assert host.extracts == 0 and not host.tree.exists() and not host.stage.exists()
    assert not host.completed()


def test_apply_from_the_frozen_source_names_no_refusal(tmp_path, monkeypatch):
    host = Host(tmp_path, monkeypatch)
    host.run("apply.yml")
    assert not host.refusals()
    assert host.extracts == 1 and host.completed()[-1]["evidence"]["postcondition"]
