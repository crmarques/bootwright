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
from ansible_collections.bootwright.core.plugins.module_utils import hosted_area
from ansible_collections.bootwright.core.plugins.module_utils.package_tree import observe
from ansible_collections.bootwright.core.plugins.action import managedos_tree_protocol as protocol

ROLE = pathlib.Path(__file__).resolve().parents[2] / "roles" / "managedos_package_tree"
LOADER = DataLoader()
SOURCE = b"the frozen installation DVD"
SHA = hashlib.sha256(SOURCE).hexdigest()
PUBLICATION = "/var/lib/bootwright-hosted/lab/services/artifact-server/artifacts/public/os/rhel-9-8/tree"
MEDIA = "rhel-9.8-x86_64-dvd.iso"
MAPPED = ("/var/lib/bootwright-hosted/", "/var/lib/bootwright/media/")


@pytest.fixture(autouse=True)
def private_directories():
    """Every directory the test host creates is 0700, as the hosted directories the role proves are."""
    previous = os.umask(0o077)
    yield
    os.umask(previous)


class Interrupted(Exception):
    pass


class Refused(Exception):
    """An assert of the role failed, which ends the run as Ansible would."""


class Host:
    def __init__(self, root, monkeypatch, area=True, server=True, context="lab"):
        self.root = root
        publication = PUBLICATION.replace("/lab/", "/%s/" % context)
        self.tree = self.local(publication)
        self.area, self.server, self.foreign = self.tree.parents[4], self.tree.parents[3], None
        if area:
            self.area.mkdir(parents=True)
            if server:
                self.server.mkdir()
        self.stage = pathlib.Path(str(self.tree) + ".staging")
        self.source = self.local("/var/lib/bootwright/media/" + MEDIA)
        self.source.parent.mkdir(parents=True)
        self.source.write_bytes(SOURCE)
        self.extracts, self.kill, self.evidence, self.proofs = 0, False, [], []
        self.values = dict(LOADER.load_from_file(str(ROLE / "defaults/main.yml"), trusted_as_template=True))
        self.values.update({"bootwright_os_tree_digest": "1" * 64, "bootwright_os_tree_material": {},
                            "bootwright_os_tree_request": {
                                "version": "os-package-tree-v1",
                                "identity": {"context": context, "profile": "rhel-9-8"},
                                "publication": {"path": publication, "url": "https://artifacts.example.test/os/rhel-9-8/tree"},
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
        if key.endswith("hosted_area"):
            assert args["state"] == "proved", args
            root = str(self.root / "var/lib/bootwright-hosted")
            try:
                exists = hosted_area.prove(args["context"], args["path"], root, os.getuid())
            except hosted_area.Refusal as refusal:
                raise Refused(str(refusal)) from None
            self.proofs.append(list(args["path"]))
            return {"changed": False, "exists": exists}
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
        owner = 4242 if path == self.foreign else 0 if info.st_uid == os.getuid() else info.st_uid
        result = {"exists": True, "isreg": stat.S_ISREG(info.st_mode), "islnk": stat.S_ISLNK(info.st_mode),
                  "isdir": stat.S_ISDIR(info.st_mode), "size": info.st_size, "uid": owner,
                  "wgrp": bool(info.st_mode & stat.S_IWGRP), "woth": bool(info.st_mode & stat.S_IWOTH)}
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


@pytest.mark.parametrize("operation", ["apply.yml", "destroy.yml", "observe.yml"])
def test_every_entry_point_proves_the_artifact_servers_hosted_directories_before_any_effect(tmp_path, monkeypatch, operation):
    host = Host(tmp_path, monkeypatch)
    host.area.chmod(0o755)
    host.tree.mkdir(parents=True)
    (host.tree / "preserved").write_text("kept")
    with pytest.raises(Refused, match="is not a directory only root may enter"):
        host.run(operation)
    assert host.extracts == 0 and not host.evidence[1:]
    assert not host.stage.exists() and (host.tree / "preserved").read_text() == "kept"


def test_the_proof_names_the_servers_service_directories(tmp_path, monkeypatch):
    host = Host(tmp_path, monkeypatch)
    host.run("apply.yml")
    assert host.proofs == [["services", "artifact-server"]]


# The artifact server creates its content root 0755, so its container can read
# what it serves; only the private directories above it are the hosted area
# (B1293).
@pytest.mark.parametrize("operation", ["apply.yml", "destroy.yml", "observe.yml"])
def test_the_servers_own_content_root_is_never_refused(tmp_path, monkeypatch, operation):
    host = Host(tmp_path, monkeypatch)
    host.server.chmod(0o755)
    host.run(operation)
    assert not host.refusals()


def test_the_tree_proves_exactly_the_area_the_artifact_server_proves():
    server = dict(LOADER.load_from_file(str(ROLE.parent / "infra_artifact_server_nginx/defaults/main.yml"), trusted_as_template=True))
    tree = dict(LOADER.load_from_file(str(ROLE / "defaults/main.yml"), trusted_as_template=True))
    content_root = PUBLICATION.split("/public/", maxsplit=1)[0]
    values = {"bootwright_artifact_server_request": {"contentRoot": content_root},
              "bootwright_os_tree_request": {"publication": {"path": PUBLICATION}}}
    proved = [Templar(loader=LOADER, variables=dict(values, **source)).template(source[name])
              for source, name in ((server, "infra_artifact_server_nginx_area"), (tree, "managedos_package_tree_area"))]
    assert proved[0] == proved[1] == ["services", "artifact-server"]


def hosted(host):
    return sorted(str(path.relative_to(host.root)) for path in (host.root / "var/lib/bootwright-hosted").rglob("*"))


# Only the artifact server creates its directories, the private ones 0700 and
# its content root 0755; a tree applied before it must not make them with the
# default mode, which the server's own next apply would refuse (B1294). The
# refusal is named, so the runner reports its remedy.
def unapplied(host):
    return pytest.raises(Refused, match="Refuse an artifact server that is not applied before any effect")


@pytest.mark.parametrize("present", [None, 1, 0], ids=["no hosted root", "context only", "services only"])
def test_apply_refuses_a_missing_artifact_server_area_before_any_effect(tmp_path, monkeypatch, present):
    host = Host(tmp_path, monkeypatch, area=False)
    if present is not None:
        host.area.parents[present].mkdir(parents=True)
    before = hosted(host)
    with unapplied(host):
        host.run("apply.yml")
    assert hosted(host) == before
    assert host.refusals() == ["artifact-server-not-applied"]
    assert host.extracts == 0 and not host.completed()


def break_server(host, how):
    if how == "missing":
        host.server.rmdir()
    elif how == "file":
        host.server.rmdir()
        host.server.write_text("not a directory")
    elif how == "link":
        host.server.rmdir()
        host.server.symlink_to(host.root, target_is_directory=True)
    elif how == "group-writable":
        host.server.chmod(0o775)
    elif how == "other-writable":
        host.server.chmod(0o757)
    else:
        host.foreign = host.server


SERVER_FAULTS = ["missing", "file", "link", "group-writable", "other-writable", "not root"]


@pytest.mark.parametrize("how", SERVER_FAULTS)
def test_apply_refuses_a_missing_or_unsafe_content_root_before_any_effect(tmp_path, monkeypatch, how):
    host = Host(tmp_path, monkeypatch)
    break_server(host, how)
    before = hosted(host)
    with unapplied(host):
        host.run("apply.yml")
    assert hosted(host) == before
    assert host.refusals() == ["artifact-server-not-applied"]
    assert host.extracts == 0 and not host.tree.parent.exists() and not host.completed()


@pytest.mark.parametrize("operation", ["destroy.yml", "observe.yml"])
@pytest.mark.parametrize("where", ["no area", "no content root"])
def test_removal_and_observation_of_a_host_without_the_server_create_nothing_and_do_not_refuse(tmp_path, monkeypatch, operation, where):
    host = Host(tmp_path, monkeypatch, area=where == "no content root", server=False)
    before = hosted(host)
    host.run(operation)
    host.run(operation)
    assert hosted(host) == before and not host.refusals()
    for record in host.completed():
        assert record["outcome"] == "unchanged"
        assert record["evidence"]["content"] is False and record["evidence"]["tree"] is False
        if operation == "destroy.yml":
            assert record["evidence"]["absent"] and record["evidence"]["postcondition"]
    assert len(host.completed()) == 2


_ROOT = "/var/lib/bootwright-hosted/lab/services/artifact-server/artifacts"
_MALFORMED = {
    "an extra component before the tree": PUBLICATION.replace("/tree", "/extra/tree"),
    "an extra component after the tree": PUBLICATION + "/extra",
    "an extra component in the content root": _ROOT + "/nested/public/os/rhel-9-8/tree",
    "a trailing slash": PUBLICATION + "/",
    "another service kind": PUBLICATION.replace("/artifact-server/", "/dns/"),
    "another area": PUBLICATION.replace("/services/", "/machines/"),
    "another hosted root": PUBLICATION.replace("/bootwright-hosted/", "/bootwright/"),
    "another context": PUBLICATION.replace("/lab/", "/other/"),
    "another profile": PUBLICATION.replace("/rhel-9-8/", "/rhel-8-10/"),
    "another served directory": PUBLICATION.replace("/public/", "/private/"),
    "another consumer": PUBLICATION.replace("/os/", "/iso/"),
    "another leaf": PUBLICATION.replace("/tree", "/install.iso"),
    "an empty server name": PUBLICATION.replace("/artifacts/", "//"),
    "an empty component": PUBLICATION.replace("/public/os/", "/public//"),
    "a dot server name": PUBLICATION.replace("/artifacts/", "/./"),
    "a dot-dot server name": PUBLICATION.replace("/artifacts/", "/../"),
    "a dot-dot in place of the served directory": PUBLICATION.replace("/public/", "/../"),
    "a dot in place of the consumer": PUBLICATION.replace("/os/", "/./"),
    "a relative path": PUBLICATION.lstrip("/"),
}


@pytest.mark.parametrize("operation", ["apply.yml", "destroy.yml", "observe.yml"])
@pytest.mark.parametrize("name", sorted(_MALFORMED))
def test_every_entry_point_refuses_a_publication_path_not_in_the_servers_normal_form(tmp_path, monkeypatch, operation, name):
    host = Host(tmp_path, monkeypatch)
    host.values["bootwright_os_tree_request"]["publication"]["path"] = _MALFORMED[name]
    before = hosted(host)
    with pytest.raises(Refused, match="Validate the frozen package tree request"):
        host.run(operation)
    assert hosted(host) == before
    assert not host.evidence and host.extracts == 0 and not host.proofs


def test_a_server_of_another_name_publishes_in_normal_form(tmp_path, monkeypatch):
    host = Host(tmp_path, monkeypatch)
    publication = "/var/lib/bootwright-hosted/lab/services/artifact-server/lab-artifacts.v2/public/os/rhel-9-8/tree"
    host.values["bootwright_os_tree_request"]["publication"]["path"] = publication
    host.tree = host.local(publication)
    host.stage = pathlib.Path(str(host.tree) + ".staging")
    host.tree.parents[3].mkdir()
    host.run("apply.yml")
    assert host.proofs == [["services", "artifact-server"]]
    assert host.completed()[-1]["evidence"]["postcondition"]


def test_a_context_named_with_digits_and_dashes_publishes_in_normal_form(tmp_path, monkeypatch):
    host = Host(tmp_path, monkeypatch, context="lab-6-nodes-ceph-network-matrix")
    host.run("apply.yml")
    assert host.proofs == [["services", "artifact-server"]]
    assert host.completed()[-1]["evidence"]["postcondition"]
    host.run("destroy.yml")
    assert not host.tree.exists()
