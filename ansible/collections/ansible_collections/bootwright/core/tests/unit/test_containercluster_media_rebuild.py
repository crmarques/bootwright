"""A work area an installation booted from is never discarded by a rebuild.

A cluster's media and install blocks share one installer work area (`WorkRoot`
in internal/containercluster/agentinstall/catalog.go, set on both requests in
selection.go). `openshift-install agent create image` leaves its asset state
and the administrator kubeconfig there
(.agents/knowledge/openshift-agent-work-area.md), and a media build starts by
discarding the area. These cases pin the rule's three parts: the installation
marks the area before any node is handed the image, the inspection reports the
mark, and the media apply refuses to build over it. They also pin the replay
that skips the build: the image already published still gets its served mode
and label and is fetched through the listener, from the directory the
inspection found it in.

Rendering the role's task files needs Ansible's controller (DataLoader and
Templar), which ansible-test does not offer to unit tests under
tests/unit/plugins/modules, so these checks live here while the inspection's
own tests stay beside its module.
"""

from __future__ import annotations

import itertools
import json
import os
import pathlib
import stat

import pytest
from ansible.module_utils.testing import patch_module_args
from ansible.modules import file as file_module
from ansible.modules import find
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar

from ansible_collections.bootwright.core.plugins.action import containercluster_media_protocol as protocol
from ansible_collections.bootwright.core.plugins.modules import containercluster_install_inspect
from ansible_collections.bootwright.core.plugins.modules import containercluster_media_inspect as inspect

ROLES = pathlib.Path(__file__).resolve().parents[2] / "roles"
MEDIA = ROLES / "containercluster_media_agent"
INSTALL = ROLES / "containercluster_install_agent"
LOADER = DataLoader()

DIGEST = "a" * 64
RELEASE = "4.21.15"
URL = "https://192.0.2.1:8443/private/clusters/sno"
# What `openssl rand -hex 32` mints: 64 hexadecimal digits.
TOKEN = "0123456789abcdef" * 4

BUILD = "not (containercluster_media_agent_published | bool)"
REPLAY = "containercluster_media_agent_published | bool"
# The probe's guard is conditioned on what the probe answered, never on a build.
PROBED = "containercluster_media_agent_probe.status | default(-1) | int not in [200, 206]"
INCLUDES = ("ansible.builtin.include_tasks", "ansible.builtin.import_tasks")


def request(tmp_path):
    """The frozen media request's fields the inspection and these tasks read."""
    return {
        "identity": {"block": "cluster-media-sno", "cluster": "sno", "context": "lab"},
        "image": {"path": str(tmp_path / "private"), "url": URL},
        "release": {"version": RELEASE},
        "workRoot": str(tmp_path / "work"),
    }


def publish(root, name=TOKEN):
    directory = pathlib.Path(root) / name
    directory.mkdir(parents=True)
    (directory / inspect.IMAGE).write_bytes(b"")
    return directory / inspect.IMAGE


def record(work, inputs=DIGEST, installer=RELEASE):
    work = pathlib.Path(work)
    work.mkdir(parents=True, exist_ok=True)
    (work / inspect.RECEIPT).write_text(json.dumps({"inputs": inputs, "installer": installer}))


def built(tmp_path):
    """A work area and image exactly as a completed build leaves them."""
    frozen = request(tmp_path)
    record(frozen["workRoot"])
    publish(frozen["image"]["path"])
    return frozen


def test_the_mark_reaches_no_evidence(tmp_path):
    frozen = built(tmp_path)
    (pathlib.Path(frozen["workRoot"]) / inspect.MARKER).write_bytes(b"")
    evidence = protocol.presence({"observation": inspect.observe(frozen)}, DIGEST)
    assert sorted(evidence) == ["absent", "image", "inputs", "installer", "postcondition", "request", "work"]


def run(module, arguments, capsys):
    """One module run in process, as a task would make it, and what it returned."""
    capsys.readouterr()
    with patch_module_args(arguments), pytest.raises(SystemExit):
        module.main()
    return json.loads(capsys.readouterr().out)


def tasks(role, name):
    """A task file as a play loads it: its templates trusted."""
    loaded = LOADER.load_from_file(str(role / "tasks" / name), trusted_as_template=True)
    return [task for task in loaded if isinstance(task, dict)]


def one(found, description):
    assert len(found) == 1, "%d tasks %s" % (len(found), description)
    return found[0]


def flattened(role, entries):
    for task in entries:
        yield task
        for section in ("block", "rescue", "always"):
            yield from flattened(role, [child for child in task.get(section) or [] if isinstance(child, dict)])
        for action in INCLUDES:
            target = task.get(action)
            if isinstance(target, dict):
                target = target.get("file")
            if isinstance(target, str):
                yield from flattened(role, tasks(role, target))


def expanded(role, name):
    """Every task one file runs within its role, nested and included ones in the order they run."""
    return flattened(role, tasks(role, name))


def marks(task):
    copy = task.get("ansible.builtin.copy")
    return isinstance(copy, dict) and str(copy.get("dest", "")).endswith("/" + inspect.MARKER)


def inserts(task):
    arguments = task.get("bootwright.core.redfish_boot")
    return isinstance(arguments, dict) and arguments.get("operation") == "insert"


def test_the_installation_marks_the_work_area_before_any_node_is_handed_the_image():
    run_order = list(expanded(INSTALL, "apply.yml"))
    mark = one([index for index, task in enumerate(run_order) if marks(task)], "mark the work area")
    handed = [index for index, task in enumerate(run_order) if inserts(task)]
    assert handed, "the walk no longer sees the installation insert any image"
    assert mark < min(handed)
    # The mark is written on every path that may reach an insert: at the top
    # level of apply.yml, under no condition and never excused from failing.
    task = run_order[mark]
    assert task in tasks(INSTALL, "apply.yml")
    assert not {"when", "failed_when", "ignore_errors"} & set(task)


def test_the_mark_the_installation_writes_is_the_one_the_inspection_reads(tmp_path):
    frozen = request(tmp_path)
    pathlib.Path(frozen["workRoot"]).mkdir()
    task = one([task for task in tasks(INSTALL, "apply.yml") if marks(task)], "mark the work area")
    copy = task["ansible.builtin.copy"]
    scope = dict(LOADER.load_from_file(str(INSTALL / "defaults" / "main.yml"), trusted_as_template=True))
    scope["bootwright_cluster_install_request"] = {"workRoot": frozen["workRoot"]}
    mark = pathlib.Path(Templar(loader=LOADER, variables=scope).template(copy["dest"]))
    assert mark == pathlib.Path(frozen["workRoot"]) / inspect.MARKER
    assert (copy["owner"], copy["group"], copy["mode"]) == ("root", "root", "0600")
    assert not inspect.observe(frozen)["booted"]
    mark.write_bytes(copy["content"].encode())
    assert inspect.observe(frozen)["booted"]


def media_scope(tmp_path, observation, **variables):
    """The media role's defaults with one observation of what is published."""
    scope = dict(LOADER.load_from_file(str(MEDIA / "defaults" / "main.yml"), trusted_as_template=True))
    scope.update(bootwright_cluster_media_request=request(tmp_path), bootwright_cluster_media_digest=DIGEST,
                 containercluster_media_agent_before={"observation": observation})
    scope.update(variables)
    return scope


def refusal():
    return one([task for task in tasks(MEDIA, "apply.yml") if "ansible.builtin.fail" in task],
               "refuse a rebuild")


def refuses(tmp_path, observation):
    """Whether the apply refuses, its conditions evaluated in order as a play does."""
    task = refusal()
    rendering = Templar(loader=LOADER, variables=media_scope(tmp_path, observation, **task.get("vars", {})))
    return all(rendering.evaluate_conditional(condition) for condition in task["when"])


PUBLISHED = {"booted": False, "image": True, "inputs": DIGEST, "installer": RELEASE, "work": True}
CHANGES = {
    "no image": ({"image": False}, "no image built there is published any more"),
    "no receipt": ({"inputs": "", "installer": ""}, "no receipt there records what its image was built from"),
    "other inputs": ({"inputs": "b" * 64}, "the inputs frozen now are not the ones its image was built from"),
    "other installer": ({"installer": "4.21.14"},
                        "its image was built by installer 4.21.14 and this cluster declares 4.21.15"),
    "no image and other inputs": ({"image": False, "inputs": "b" * 64},
                                  "no image built there is published any more, and the inputs frozen now are "
                                  "not the ones its image was built from"),
}


@pytest.mark.parametrize("change, cause", CHANGES.values(), ids=CHANGES.keys())
def test_a_marked_area_that_would_be_rebuilt_refuses_naming_why_and_the_remedy(tmp_path, change, cause):
    observation = dict(PUBLISHED, booted=True, **change)
    assert refuses(tmp_path, observation) is True
    task = refusal()
    message = Templar(loader=LOADER, variables=media_scope(tmp_path, observation, **task["vars"])).template(
        task["ansible.builtin.fail"]["msg"])
    assert message == (
        "the installation of cluster sno marked the work area its boot image is built in before it booted any node "
        "from that image, so the area holds the state those nodes install from and the cluster's only "
        "administrator access, and building the image again would start by discarding it; it would be built "
        "again because %s. Nothing was changed. Destroy the cluster's installation first: bootwright destroy "
        "--context lab ejects its media before the work area is discarded, and the apply after it builds the "
        "image afresh" % cause)


def test_a_marked_area_whose_image_is_this_requests_is_replayed(tmp_path):
    assert refuses(tmp_path, dict(PUBLISHED, booted=True)) is False


@pytest.mark.parametrize("change", [change for change, _cause in CHANGES.values()] + [{}],
                         ids=list(CHANGES) + ["published"])
def test_an_unmarked_area_is_rebuilt_or_replayed_as_before(tmp_path, change):
    assert refuses(tmp_path, dict(PUBLISHED, **change)) is False


def test_the_refusal_comes_before_anything_a_build_discards():
    apply = tasks(MEDIA, "apply.yml")
    observed = one([index for index, task in enumerate(apply)
                    if task.get("register") == "containercluster_media_agent_before"], "observe before")
    refused = apply.index(refusal())
    included = one([index for index, task in enumerate(apply)
                    if task.get("ansible.builtin.include_tasks") == "build.yml"], "include the build")
    assert observed < refused < included
    assert "when" not in apply[included]
    # The refusal is decided on the same fact that lets the build discard the
    # area, and that discard happens nowhere else.
    assert refusal()["when"][0] == BUILD
    assert not {"failed_when", "ignore_errors"} & set(refusal())
    build = tasks(MEDIA, "build.yml")
    assert build[0]["when"] == BUILD
    discards = [task for task in expanded(MEDIA, "apply.yml")
                if (task.get("ansible.builtin.file") or {}).get("state") == "absent"
                and task["ansible.builtin.file"]["path"] == "{{ containercluster_media_agent_work }}"]
    assert discards == [build[0]["block"][0]]


def argv(task):
    command = task.get("ansible.builtin.command")
    return [str(value) for value in (command or {}).get("argv") or []] if isinstance(command, dict) else []


def serves(task):
    """The four steps that make the published image readable and prove it."""
    path = str((task.get("ansible.builtin.file") or {}).get("path", ""))
    return (path.endswith("/agent.iso") or argv(task)[:1] == ["/usr/bin/chcon"]
            or "ansible.builtin.uri" in task or "ansible.builtin.fail" in task)


def test_a_replay_builds_nothing_and_still_serves_and_proves_the_image():
    build = tasks(MEDIA, "build.yml")
    served = [task for task in build if serves(task)]
    assert len(served) == 4
    for task in served:
        assert task.get("when") in (None, PROBED), task["name"]
    replay = one([task for task in build if task.get("when") == REPLAY], "run only on a replay")
    assert {action for task in replay["block"] for action in task if action.startswith("ansible.")} == {
        "ansible.builtin.find", "ansible.builtin.assert", "ansible.builtin.set_fact"}
    for task in build:
        if task not in served and task is not replay:
            assert task.get("when") == BUILD, task["name"]


def replay_tokens(tmp_path, capsys):
    """The directories a replay may prove: its own find run, then its own choice.

    find lists its matches in the order the filesystem enumerates them (os.walk
    in ansible/modules/find.py), which differs between filesystems and volumes,
    so the choice is taken over every order it could list them in. None stands
    for a replay that refuses.
    """
    replay = one([task for task in tasks(MEDIA, "build.yml") if task.get("when") == REPLAY], "run only on a replay")
    finding, guard, taking = replay["block"]
    scope = media_scope(tmp_path, dict(PUBLISHED), **replay["vars"])
    arguments = Templar(loader=LOADER, variables=scope).template(finding["ansible.builtin.find"])
    listed = run(find, arguments, capsys)
    token = taking["ansible.builtin.set_fact"]["containercluster_media_agent_token"]
    chosen = set()
    for files in itertools.permutations(listed["files"]):
        scope[finding["register"]] = dict(listed, files=list(files))
        rendering = Templar(loader=LOADER, variables=scope)
        if all(rendering.evaluate_conditional(condition) for condition in guard["ansible.builtin.assert"]["that"]):
            chosen.add(rendering.template(token)["stdout"])
        else:
            chosen.add(None)
    return chosen


def serving_mode():
    return one([task for task in tasks(MEDIA, "build.yml")
                if str((task.get("ansible.builtin.file") or {}).get("path", "")).endswith("/agent.iso")],
               "set the served mode")


LAYOUTS = {
    "one image": [TOKEN],
    "an empty directory first, a later image": ["00", TOKEN, "f" * 64],
    "a hidden directory first": [".0", TOKEN],
    "an image directly beneath the subtree": ["", TOKEN],
}


@pytest.mark.parametrize("layout", LAYOUTS.values(), ids=LAYOUTS.keys())
def test_a_replay_proves_the_image_the_inspection_found_at_the_address_a_node_boots(tmp_path, capsys, layout):
    private = pathlib.Path(request(tmp_path)["image"]["path"])
    for name in layout:
        if name == "":
            private.mkdir(parents=True, exist_ok=True)
            (private / inspect.IMAGE).write_bytes(b"")
        elif name == "00":
            (private / name).mkdir(parents=True)
        else:
            publish(private, name)
    found = inspect.published(str(private))
    assert found
    token = os.path.basename(os.path.dirname(found))
    assert replay_tokens(tmp_path, capsys) == {token}
    scope = media_scope(tmp_path, dict(PUBLISHED), containercluster_media_agent_token={"stdout": token})
    rendering = Templar(loader=LOADER, variables=scope)
    probe = one([task for task in tasks(MEDIA, "build.yml") if "ansible.builtin.uri" in task],
                "fetch through the listener")
    assert rendering.template(serving_mode()["ansible.builtin.file"]["path"]) == found
    assert rendering.template(probe["ansible.builtin.uri"]["url"]) == containercluster_install_inspect.published(
        str(private), URL)


def test_a_replay_that_finds_no_image_in_a_directory_refuses(tmp_path, capsys):
    private = pathlib.Path(request(tmp_path)["image"]["path"])
    (private / "00").mkdir(parents=True)
    (private / inspect.IMAGE).write_bytes(b"")
    assert replay_tokens(tmp_path, capsys) == {None}


def served(tmp_path, capsys, image):
    """What a play registers from the served-mode step, run by the real file module over one published image."""
    mode = serving_mode()
    scope = media_scope(tmp_path, dict(PUBLISHED), containercluster_media_agent_token={"stdout": image.parent.name})
    arguments = Templar(loader=LOADER, variables=scope).template(mode["ansible.builtin.file"])
    assert pathlib.Path(arguments["path"]) == image
    # A root apply published the image root's already, so ownership changes
    # nothing and is left out: the step then runs unprivileged.
    assert (arguments.pop("owner"), arguments.pop("group")) == ("root", "root")
    result = run(file_module, arguments, capsys)
    # The task executor judges changed_when, if any, with the result registered.
    scope[mode["register"]] = result
    judged = mode.get("changed_when", [])
    for condition in judged if isinstance(judged, list) else [judged]:
        result["changed"] = Templar(loader=LOADER, variables=scope).evaluate_conditional(condition)
    return {mode["register"]: result}


def outcome(tmp_path, observation, registered):
    """The outcome the completion publishes, given what the served-mode step registered."""
    action = "bootwright.core.containercluster_media_protocol"
    completed = one([task for task in tasks(MEDIA, "apply.yml") if (task.get(action) or {}).get("phase") == "completed"],
                    "publish completion")
    scope = media_scope(tmp_path, observation, **registered)
    return Templar(loader=LOADER, variables=scope).template(completed[action]["outcome"])


@pytest.mark.parametrize("left, reported", [(0o600, "changed"), (0o640, "unchanged")],
                         ids=["left 0600 by an earlier executable", "already served"])
def test_a_replay_reports_a_change_exactly_when_it_repaired_the_served_mode(tmp_path, capsys, left, reported):
    image = publish(request(tmp_path)["image"]["path"])
    image.chmod(left)
    registered = served(tmp_path, capsys, image)
    assert stat.S_IMODE(image.stat().st_mode) == 0o640
    assert outcome(tmp_path, dict(PUBLISHED), registered) == reported
    # A build reports a change whatever the mode step found.
    assert outcome(tmp_path, dict(PUBLISHED, inputs="b" * 64), registered) == "changed"
