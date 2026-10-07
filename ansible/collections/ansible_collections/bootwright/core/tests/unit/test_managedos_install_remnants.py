"""An installation takes back what an attempt stopped part way leaves.

An apply extracts the package tree beside its published location, at
<tree>.staging, renames it into place whole, and builds the installer image in
a work area outside the served root. An apply killed between those steps leaves
the staging copy beneath the served root and the work area behind, and a
removal stopped while it deleted the tree leaves the tree's directory without
its .treeinfo (found in X29). Nor does an installation leave an empty directory
behind: the profile's directory the apply created for the tree, or the work
area an observation created for its reads (found in X31). These tests run the
role's own task files over a directory standing in for the artifact server's
host, performing each file task and each native command the way the tool does:

- xorriso -osirrox on -extract / <dir> merges the image into a directory that
  already exists rather than replacing it (xorriso(1), -extract, xorriso 1.5.8);
- mv --no-target-directory renames with rename(2), which refuses a target
  directory that is not empty: "Directory not empty", the failure X29 recorded;
- mkksiso refuses an output image that already exists ("%s already exists",
  main in pylorax/cmdline/mkksiso.py, lorax 43.12);
- the file module's state=absent deletes a directory through shutil.rmtree in
  the order the directory lists its entries (ansible/modules/file.py,
  ensure_absent, ansible-core 2.21), so a removal killed part way leaves the
  entries listed after the kill;
- rmdir --ignore-fail-on-non-empty removes a directory only while it is empty
  and ignores the failure to remove one that is not (the rmdir invocation in
  the GNU coreutils manual,
  https://www.gnu.org/software/coreutils/manual/html_node/rmdir-invocation.html,
  coreutils 9.7), and fails on one that does not exist;
- the stat module reads a path without following a symbolic link and answers
  only exists false for one that is not there (ansible/modules/stat.py, main,
  ansible-core 2.21);
- a block runs its always tasks after its own whether or not one failed, and
  its condition holds for every task inside it.

- the copy module writes the tree's identity into the staged tree; the
  rendered Kickstart it also writes is a write this does not depend on.

An included task file is a read these tests stand in for. Every other task,
the looped proofs of the tooling and the media an apply uses among them, is a
proof or a read this does not depend on, and is passed over: the media read
answers each entry unchanged. Rendering the
task files needs Ansible's controller (DataLoader and Templar), which
ansible-test does not offer to unit tests under tests/unit/plugins.
"""

from __future__ import annotations

import errno
import os
import pathlib
import shutil
from types import SimpleNamespace

import pytest
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar

from ansible_collections.bootwright.core.plugins.action import managedos_install_protocol as protocol
from ansible_collections.bootwright.core.plugins.modules.managedos_install_inspect import observe

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
MODELLED = (INSPECT, PROTOCOL, FILE, COMMAND, INCLUDE, STAT, COPY)
BOOT = {"name": "rhel-9.8-x86_64-boot.iso", "sha256": "b" * 64, "size": 1105199104}
DVD = {"name": "rhel-9.8-x86_64-dvd.iso", "sha256": "d" * 64, "size": 13107200000}
BUILD = "Build this machine's own installer image"
# What an apply publishes: the DVD's marker and one repository the extraction
# writes, and the identity of the DVD the apply records beside them.
EXTRACTED = {".treeinfo", "BaseOS", ".bootwright-tree-identity"}


class Killed(Exception):
    """The attempt was killed while a task deleted a directory."""


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

    def __init__(self, root, monkeypatch):
        self.root = root
        self.tree = root / "public" / "os" / "rhel-9-8" / "tree"
        self.staging = self.tree.parent / "tree.staging"
        self.image = root / "public" / "os" / "rhel-01" / "install.iso"
        self.parent = root / "var" / "lib" / "bootwright-install"
        self.work = self.parent / "work"
        self.variables = dict(LOADER.load_from_file(str(ROLE / "defaults" / "main.yml"), trusted_as_template=True))
        self.variables.update({
            "bootwright_os_install_digest": DIGEST,
            "bootwright_os_install_material": {},
            "bootwright_os_install_request": {
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
        # A directory whose next deletion is killed part way, and the entries
        # it lists after the kill, which that deletion never reaches.
        self.kills = {}
        # What each included task file does in place of its reads.
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
        if path in self.kills:
            unreached = self.kills.pop(path)
            for entry in path.iterdir():
                if entry.name in unreached:
                    continue
                if entry.is_dir():
                    shutil.rmtree(entry)
                else:
                    entry.unlink()
            raise Killed(path)
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

    def command(self, arguments):
        argv = [str(argument) for argument in arguments["argv"]]
        assert "removes" not in arguments and "creates" not in arguments, arguments
        if argv[0] == "/usr/bin/rmdir":
            assert argv[1:-1] == ["--ignore-fail-on-non-empty"], argv
            try:
                os.rmdir(self.inside(argv[-1]))
            except OSError as error:
                if error.errno != errno.ENOTEMPTY:
                    raise
        elif argv[0] == "/usr/bin/xorriso":
            target = self.inside(argv[-1])
            (target / "BaseOS").mkdir(parents=True, exist_ok=True)
            (target / ".treeinfo").write_text("[general]\n")
        elif argv[0] == "/usr/bin/mv":
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


def stopped_removal(host):
    (host.tree / "AppStream" / "Packages").mkdir(parents=True)
    (host.tree / "AppStream" / "Packages" / "left.rpm").write_text("left\n")


def killed_extraction(host):
    (host.staging / "Packages").mkdir(parents=True)
    (host.staging / "Packages" / "partial.rpm").write_text("partial\n")


def killed_build(host):
    host.work.mkdir(parents=True)
    (host.work / "install.iso").write_bytes(b"stale")


# An apply killed after it created the profile's directory and before it
# extracted, or a removal killed after the tree went and before the directory.
def empty_profile_directory(host):
    host.tree.parent.mkdir(parents=True)


@pytest.mark.parametrize("left", [stopped_removal, killed_extraction, killed_build],
                         ids=["tree without its marker", "staging tree", "image in the work area"])
def test_an_apply_clears_its_own_remnant_before_it_publishes(tmp_path, monkeypatch, left):
    host = Host(tmp_path, monkeypatch)
    left(host)
    host.run("apply.yml", through=BUILD)
    assert {entry.name for entry in host.tree.iterdir()} == EXTRACTED
    assert not os.path.lexists(host.staging)
    assert (host.work / "install.iso").read_bytes() == b"built"


# The clearing is decided by the marker the rename publishes last and the
# identity it carries, so a tree already published whole from the frozen DVD
# is left exactly as it is.
def test_an_apply_over_a_published_tree_leaves_it_alone(tmp_path, monkeypatch):
    host = Host(tmp_path, monkeypatch)
    (host.tree / "AppStream").mkdir(parents=True)
    (host.tree / ".treeinfo").write_text("[general]\n")
    (host.tree / ".bootwright-tree-identity").write_text(DVD["sha256"] + "\n")
    host.run("apply.yml", through=BUILD)
    assert {entry.name for entry in host.tree.iterdir()} == {".treeinfo", "AppStream", ".bootwright-tree-identity"}


# The completion evidence is read after the work area is removed, so it never
# reports one the completed apply does not leave.
def test_an_apply_completion_reads_after_its_work_area_is_gone():
    applied = tasks("apply.yml")
    removals = [index for index, task in enumerate(applied)
                if (task.get(FILE) or {}).get("state") == "absent"
                and task[FILE].get("path") == "{{ managedos_install_anaconda_work }}"]
    readings = [index for index, task in enumerate(applied) if INSPECT in task]
    assert len(removals) == 2 and len(readings) == 2
    assert readings[0] < removals[0]
    assert removals[1] < readings[1]


# Each remnant alone is what the removal changes, so each counts in its outcome.
@pytest.mark.parametrize("left", [(killed_extraction,), (killed_build,), (empty_profile_directory,),
                                  (killed_extraction, killed_build)],
                         ids=["staging tree", "work area", "empty profile directory", "both"])
def test_a_removal_takes_back_and_reports_what_a_killed_apply_left(tmp_path, monkeypatch, left):
    host = Host(tmp_path, monkeypatch)
    for remnant in left:
        remnant(host)
    host.run("destroy.yml")
    before = host.variables["managedos_install_anaconda_before"]["observation"]
    assert before["treeStaging"] is (killed_extraction in left)
    assert before["work"] is (killed_build in left)
    assert not os.path.lexists(host.staging)
    assert not os.path.lexists(host.tree.parent)
    assert not os.path.lexists(host.work)
    completed = host.completion()
    assert completed["outcome"] == "changed"
    assert completed["evidence"]["absent"] is True
    assert completed["evidence"]["postcondition"] is True


def published_tree(host):
    for repository in ("AppStream", "BaseOS"):
        (host.tree / repository / "Packages").mkdir(parents=True)
    (host.tree / ".treeinfo").write_text("[general]\n")
    (host.tree / ".bootwright-tree-identity").write_text(DVD["sha256"] + "\n")


# A removal killed while it deleted the tree leaves the entries the directory
# listed after the kill. The marker is withdrawn before the tree, so no listing
# order leaves it over a partial tree, and the next apply reads the tree as
# content left, clears it and publishes it whole.
@pytest.mark.parametrize("unreached", [{".treeinfo"}, {"AppStream", "BaseOS"}, {".treeinfo", "AppStream", "BaseOS"}],
                         ids=["marker listed last", "marker listed first", "killed before the first entry"])
def test_an_apply_republishes_the_tree_a_killed_removal_left(tmp_path, monkeypatch, unreached):
    host = Host(tmp_path, monkeypatch)
    published_tree(host)
    host.kills[host.tree] = unreached
    with pytest.raises(Killed):
        host.run("destroy.yml")
    assert ".treeinfo" not in names(host.tree)
    host.run("apply.yml", through=BUILD)
    before = host.variables["managedos_install_anaconda_before"]["observation"]
    assert (before["tree"], before["treeContent"]) == (False, True)
    assert names(host.tree) == EXTRACTED


def test_a_removal_with_nothing_left_changes_nothing(tmp_path, monkeypatch):
    host = Host(tmp_path, monkeypatch)
    host.run("destroy.yml")
    completed = host.completion()
    assert completed["outcome"] == "unchanged"
    assert completed["evidence"]["postcondition"] is True


# The apply created the profile's directory for the tree and the image's for
# the image, so a removal leaves neither behind.
def test_a_removal_leaves_no_directory_the_apply_created(tmp_path, monkeypatch):
    host = Host(tmp_path, monkeypatch)
    host.run("apply.yml", through=BUILD)
    assert names(host.tree.parent) == {"tree"}
    host.run("destroy.yml")
    assert not os.path.lexists(host.tree.parent)
    assert not os.path.lexists(host.image.parent)
    assert not os.path.lexists(host.work)
    # The root-only parent of the work area is shared, so a removal leaves it.
    assert host.parent.is_dir()
    assert host.completion()["outcome"] == "changed"


# Only the tree and its staging copy are published in the profile's directory,
# so anything else found there is not this removal's to take. It stays, and the
# directory kept for it counts as no change.
@pytest.mark.parametrize("left, outcome", [(published_tree, "changed"), (empty_profile_directory, "unchanged")],
                         ids=["beside the tree", "with nothing of its own"])
def test_a_removal_leaves_what_else_it_finds_in_the_profile_directory(tmp_path, monkeypatch, left, outcome):
    host = Host(tmp_path, monkeypatch)
    left(host)
    (host.tree.parent / "install.iso").write_bytes(b"another")
    host.run("destroy.yml")
    assert names(host.tree.parent) == {"install.iso"}
    completed = host.completion()
    assert completed["outcome"] == outcome
    assert completed["evidence"]["postcondition"] is True


# The observation creates the work area its reads may need, so one that found
# none leaves none, whether its reads answered or failed.
def test_an_observation_leaves_no_work_area_it_did_not_find(tmp_path, monkeypatch):
    host = Host(tmp_path, monkeypatch)
    host.run("observe.yml")
    assert not os.path.lexists(host.work)
    assert host.completion()["evidence"]["work"] is False


def test_an_observation_whose_read_fails_leaves_no_work_area_it_did_not_find(tmp_path, monkeypatch):
    host = Host(tmp_path, monkeypatch)

    def pinned_then_failed():
        (host.work / "known_hosts").write_text("198.51.100.11 ssh-ed25519 AAAAHOST\n")
        raise ReadFailed()

    host.reads["identity.yml"] = pinned_then_failed
    with pytest.raises(ReadFailed):
        host.run("observe.yml")
    assert not os.path.lexists(host.work)


# A work area it found is what an interrupted attempt left, which the evidence
# reports and a removal takes back, so the observation leaves it as it was.
def test_an_observation_leaves_the_work_area_it_found_as_it_was(tmp_path, monkeypatch):
    host = Host(tmp_path, monkeypatch)
    killed_build(host)
    host.run("observe.yml")
    assert names(host.work) == {"install.iso"}
    assert host.completion()["evidence"]["work"] is True
