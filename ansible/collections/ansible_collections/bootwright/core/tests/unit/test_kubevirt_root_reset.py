"""A staged destroy blanks a kept KubeVirt machine's root disk and nothing else.

The reset touches only this context's own VirtualMachine, by the UID its apply
recorded, on the cluster its provider proof pinned, stopped and unchanged from
its frozen request, and only the root DataVolume its template names and it
controls. It deletes that DataVolume by its UID, lets the VirtualMachine's
template create it again blank at its frozen size and class over a claim of its
own, and marks the VirtualMachine through a UID-tested patch only once the new
DataVolume reads blank and the VirtualMachine is still stopped, naming the
staged destroy, the step and the new DataVolume. So the VirtualMachine's UID,
MACs, vTPM state and claim stay, a reading tells a completed reset from one
that never ran or was cut short, a repeated reset changes nothing, and a reset
cut short never leaves a mark over a disk it did not blank. Losing any of this
is invisible to lint and to a syntax check.
"""

from __future__ import annotations

import copy
import pathlib

import pytest
import yaml

from ansible_collections.bootwright.core.plugins.action import substrate_kubevirt_api as action
from ansible_collections.bootwright.core.plugins.action.substrate_kubevirt_protocol import completion
from ansible_collections.bootwright.core.plugins.module_utils import kubevirt_api as kapi
from ansible_collections.bootwright.core.plugins.module_utils import kubevirt_machine as arm
from ansible_collections.bootwright.core.plugins.module_utils.kubevirt_root_reset import MARK_KEY, completed, mark_of, read, reset
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils.test_kubevirt_machine import (
    CLAIMS, VM, VM_PATH, FakeAPI, client, context, host, request)

COLLECTION = pathlib.Path(__file__).resolve().parents[2]
ROLE = COLLECTION / "roles" / "substrate_kubevirt_machine"
REMOVAL = "op-0123456789abcdef0123456789abcdef"
OTHER = "op-ffffffffffffffffffffffffffffffff"
STEP = "cluster-install-edge"
MARK = REMOVAL + "/" + STEP
DIGEST = "a" * 64
DV = "/apis/cdi.kubevirt.io/v1beta1/namespaces/guests/datavolumes/"
ROOT = DV + VM + "-root"
STATE = CLAIMS + "persistent-state-for-" + VM


def unescaped(part):
    return part.replace("~1", "/").replace("~0", "~")


class Cluster(FakeAPI):
    """The fake host with KubeVirt's VirtualMachine controller, which creates a
    template's DataVolume again once it is gone, and JSON patches over maps."""

    def __init__(self):
        super().__init__()
        self.recreate, self.conflict, self.adopt, self.start_after_recreate = True, False, False, False
        self.rebind, self.deletes = "", []

    def claim(self, dv):
        """A claim the provisioner binds to a new PersistentVolume, or to the one rebind names, as a Retain class's
        Available volume would be bound again."""
        super().claim(dv)
        self.counter += 1
        self.objects[CLAIMS + dv["metadata"]["name"]]["spec"] = {"volumeName": self.rebind or "pv-%d" % self.counter}

    def settle(self):
        super().settle()
        if not self.recreate:
            return
        for path, vm in list(self.objects.items()):
            if "/virtualmachines/" not in path:
                continue
            for template in vm["spec"].get("dataVolumeTemplates", []):
                name = template["metadata"]["name"]
                if DV + name in self.objects:
                    continue
                volume = copy.deepcopy(template)
                volume["metadata"].update(self.meta(name)["metadata"])
                volume["metadata"]["ownerReferences"] = [{"controller": True, "uid": vm["metadata"]["uid"]}]
                volume["status"] = {"phase": "Succeeded"}
                self.put(DV + name, volume)
                held = self.objects.get(CLAIMS + name)
                if self.adopt and held is not None:
                    held["metadata"]["ownerReferences"] = [{"controller": True, "kind": "DataVolume", "name": name, "uid": volume["metadata"]["uid"]}]
                else:
                    self.claim(volume)
                if self.start_after_recreate:
                    self.put("/apis/kubevirt.io/v1/namespaces/guests/virtualmachineinstances/" + VM, {"status": {"phase": "Running"}})

    def delete(self, path, body):
        """A delete of a DataVolume is honoured only under a UID precondition, as the reset must send it."""
        self.deletes.append((path, (body or {}).get("preconditions", {}).get("uid"), (body or {}).get("propagationPolicy")))
        if path.startswith(DV) and not (body or {}).get("preconditions", {}).get("uid"):
            return 422, b'{"reason":"Invalid"}'
        if self.conflict and path.startswith(DV):
            return 409, b'{"reason":"Conflict"}'
        return super().delete(path, body)

    def patch(self, path, operations):
        held = copy.deepcopy(self.objects[path])
        for operation in operations:
            parts = [unescaped(part) for part in operation["path"].strip("/").split("/")]
            parent = held
            for part in parts[:-1]:
                parent = parent[int(part)] if isinstance(parent, list) else parent[part]
            leaf = parts[-1]
            value = parent[int(leaf)] if isinstance(parent, list) else parent.get(leaf)
            if operation["op"] == "test" and value != operation["value"]:
                return 422, b"{}"
            if operation["op"] == "add" and isinstance(parent, dict):
                parent[leaf] = operation["value"]
            elif operation["op"] == "remove" and isinstance(parent, dict):
                del parent[leaf]
        self.objects[path] = held
        return 200, b"{}"

    def writes(self):
        return [(method, path) for method, path in self.requests if method in ("DELETE", "PATCH", "POST", "PUT")]


@pytest.fixture(name="kept")
def fixture_kept():
    """A VirtualMachine the apply realized, its installed root, its claim and its vTPM state, and the context its block recorded."""
    api = Cluster()
    ignored_changed, applied = arm.apply(host(api), request(tpm={"persistent": True}), context())
    api.objects[ROOT]["metadata"]["annotations"]["installed"] = "system"
    api.put(STATE, {"metadata": {"name": "persistent-state-for-" + VM, "uid": "tpm-1", "ownerReferences": [{"uid": api.objects[VM_PATH]["metadata"]["uid"]}]}})
    api.requests.clear()
    return api, context(applied["receipts"])


def uid(api, path):
    return api.objects[path]["metadata"]["uid"]


def test_a_reset_deletes_only_the_root_data_volume_and_marks_the_vm_once_its_template_created_it_blank(kept):
    api, recorded = kept
    vm_uid, installed, claim = uid(api, VM_PATH), uid(api, ROOT), uid(api, CLAIMS + VM + "-root")
    spec = copy.deepcopy(api.objects[VM_PATH]["spec"])
    changed, observation = reset(host(api), request(tpm={"persistent": True}), recorded, REMOVAL, STEP)
    fresh = uid(api, ROOT)
    assert changed and observation["postcondition"] and observation["rootReset"] == MARK and observation["virtualMachine"] == vm_uid
    assert fresh != installed and uid(api, CLAIMS + VM + "-root") != claim and "installed" not in api.objects[ROOT]["metadata"]["annotations"]
    assert {key: value for key, value in observation["root"].items() if key != "volume"} == {
        "blank": True, "name": VM + "-root", "phase": "Succeeded", "present": True, "storage": "120Gi", "storageClass": "guest-block", "uid": fresh}
    assert uid(api, VM_PATH) == vm_uid and api.objects[VM_PATH]["spec"] == spec and STATE in api.objects
    assert api.objects[VM_PATH]["metadata"]["annotations"][MARK_KEY] == MARK + "/" + fresh
    assert api.writes() == [("DELETE", ROOT), ("PATCH", VM_PATH)] and api.deletes == [(ROOT, installed, "Foreground")]
    assert observation["root"]["volume"] == api.objects[CLAIMS + VM + "-root"]["spec"]["volumeName"]


def test_a_repeated_reset_under_its_own_mark_changes_nothing(kept):
    api, recorded = kept
    reset(host(api), request(tpm={"persistent": True}), recorded, REMOVAL, STEP)
    api.requests.clear()
    changed, observation = reset(host(api), request(tpm={"persistent": True}), recorded, REMOVAL, STEP)
    assert not changed and observation["postcondition"] and api.writes() == []


def test_another_removals_mark_is_dropped_before_the_disk_is_blanked_again(kept):
    api, recorded = kept
    reset(host(api), request(tpm={"persistent": True}), recorded, OTHER, STEP)
    api.recreate = False
    with pytest.raises(kapi.Refused, match="did not create its root DataVolume again"):
        reset(host(api), request(tpm={"persistent": True}), recorded, REMOVAL, STEP)
    assert MARK_KEY not in api.objects[VM_PATH]["metadata"]["annotations"] and ROOT not in api.objects
    assert not read(host(api), request(tpm={"persistent": True}), recorded, OTHER + "/" + STEP)["postcondition"]


def running(api, recorded):
    api.put("/apis/kubevirt.io/v1/namespaces/guests/virtualmachineinstances/" + VM, {"status": {"phase": "Running"}})
    return recorded


def foreign(api, recorded):
    receipts = [dict(r, uid="another") if r["kind"] == "VirtualMachine" else r for r in recorded["pinned"]["receipts"]]
    return dict(recorded, pinned=dict(recorded["pinned"], receipts=receipts))


def unrecorded(api, recorded):
    return dict(recorded, pinned=dict(recorded["pinned"], receipts=[]))


def other_cluster(api, recorded):
    return dict(recorded, pinned=dict(recorded["pinned"], kubeSystemUID="cluster-0"))


def uncontrolled(api, recorded):
    api.objects[ROOT]["metadata"]["ownerReferences"] = [{"controller": True, "uid": "someone"}]
    return recorded


def drifted(api, recorded):
    api.objects[VM_PATH]["spec"]["dataVolumeTemplates"][0]["spec"]["storage"]["resources"]["requests"]["storage"] = "200Gi"
    return recorded


def relabelled(api, recorded):
    api.objects[VM_PATH]["metadata"]["labels"][arm.MACHINE] = "other"
    return recorded


def handed_to_the_vm(api, recorded):
    api.recreate = False
    del api.objects[ROOT]
    api.objects[CLAIMS + VM + "-root"]["metadata"]["ownerReferences"] = [{
        "apiVersion": "kubevirt.io/v1", "kind": "VirtualMachine", "name": VM, "uid": uid(api, VM_PATH), "controller": True,
        "blockOwnerDeletion": True}]
    return recorded


def orphaned(api, recorded):
    api.recreate = False
    del api.objects[ROOT]
    api.objects[CLAIMS + VM + "-root"]["metadata"]["ownerReferences"] = []
    return recorded


@pytest.mark.parametrize("arrange, reason", [
    (running, "is running"),
    (foreign, "is not the one this context's Machine block recorded"),
    (unrecorded, "has no UID its Machine block recorded"),
    (other_cluster, "kube-system UID differs"),
    (uncontrolled, "is not controlled by its VirtualMachine"),
    (drifted, "differs from its frozen request"),
    (relabelled, "is not the one this context's Machine block recorded"),
    (orphaned, "outlives its DataVolume"),
    (handed_to_the_vm, "is held by its VirtualMachine since CDI garbage-collected its DataVolume"),
], ids=["running", "foreign", "unrecorded", "other-cluster", "uncontrolled", "drifted", "relabelled", "garbage-collected", "handed-to-the-vm"])
def test_a_reset_refuses_before_any_effect_on_a_target_that_is_not_the_machines_own_stopped_root(kept, arrange, reason):
    api, recorded = kept
    api.objects[VM_PATH]["metadata"]["annotations"][MARK_KEY] = OTHER + "/" + STEP + "/" + uid(api, ROOT)
    recorded = arrange(api, recorded)
    before = copy.deepcopy(api.objects)
    with pytest.raises(kapi.Refused, match=reason):
        reset(host(api), request(tpm={"persistent": True}), recorded, REMOVAL, STEP)
    assert api.writes() == [] and api.objects == before


def test_a_data_volume_replaced_under_the_reset_refuses_its_deletion_unmarked(kept):
    api, recorded = kept
    api.conflict = True
    with pytest.raises(kapi.Refused, match="changed while its reset deleted it"):
        reset(host(api), request(tpm={"persistent": True}), recorded, REMOVAL, STEP)
    assert MARK_KEY not in api.objects[VM_PATH]["metadata"]["annotations"] and "installed" in api.objects[ROOT]["metadata"]["annotations"]


def test_a_recreated_data_volume_that_adopted_the_installed_claim_never_reads_blank(kept):
    api, recorded = kept
    api.adopt = True
    api.collect = lambda owner: None
    with pytest.raises(kapi.Refused, match="did not create its root DataVolume again"):
        reset(host(api), request(tpm={"persistent": True}), recorded, REMOVAL, STEP)
    assert MARK_KEY not in api.objects[VM_PATH]["metadata"]["annotations"]
    assert not read(host(api), request(tpm={"persistent": True}), recorded, MARK)["postcondition"]


def test_a_claim_bound_again_to_the_installed_volume_never_reads_blank(kept):
    api, recorded = kept
    api.rebind = api.objects[CLAIMS + VM + "-root"]["spec"]["volumeName"]
    with pytest.raises(kapi.Refused, match="did not create its root DataVolume again"):
        reset(host(api), request(tpm={"persistent": True}), recorded, REMOVAL, STEP)
    assert MARK_KEY not in api.objects[VM_PATH]["metadata"]["annotations"]


def test_a_claim_bound_to_no_volume_never_reads_blank(kept):
    api, recorded = kept
    reset(host(api), request(tpm={"persistent": True}), recorded, REMOVAL, STEP)
    api.objects[CLAIMS + VM + "-root"]["spec"] = {}
    observation = read(host(api), request(tpm={"persistent": True}), recorded, MARK)
    assert not observation["postcondition"] and observation["root"]["volume"] == ""
    assert not published(observation, False)[0]["postcondition"]


def test_a_vm_started_during_the_reset_leaves_the_blank_disk_unmarked(kept):
    api, recorded = kept
    api.start_after_recreate = True
    with pytest.raises(kapi.Refused, match="did not stay stopped"):
        reset(host(api), request(tpm={"persistent": True}), recorded, REMOVAL, STEP)
    assert MARK_KEY not in api.objects[VM_PATH]["metadata"]["annotations"]


def mark_removed(api):
    api.objects[VM_PATH]["metadata"]["annotations"].pop(MARK_KEY, None)


def deleted_not_recreated(api):
    del api.objects[ROOT]
    del api.objects[CLAIMS + VM + "-root"]
    api.recreate = False


def recreated_unmarked(api):
    deleted_not_recreated(api)
    api.recreate = True
    api.settle()


def marked_over_the_installed_volume(api):
    api.objects[VM_PATH]["metadata"]["annotations"][MARK_KEY] = MARK + "/" + uid(api, ROOT)


@pytest.mark.parametrize("cut", [mark_removed, deleted_not_recreated, recreated_unmarked, marked_over_the_installed_volume],
                         ids=["mark-removed", "deleted-not-recreated", "recreated-unmarked", "marked-over-the-installed-volume"])
def test_a_reset_cut_short_reads_not_completed_and_is_redone(kept, cut):
    api, recorded = kept
    cut(api)
    observation = read(host(api), request(tpm={"persistent": True}), recorded, MARK)
    evidence, refusal = published(observation, True)
    assert observation["answered"] and not observation["postcondition"] and refusal is None and not evidence["postcondition"]
    api.recreate = True
    changed, observation = reset(host(api), request(tpm={"persistent": True}), recorded, REMOVAL, STEP)
    assert changed and observation["postcondition"] and observation["rootReset"] == MARK
    assert api.objects[VM_PATH]["metadata"]["annotations"][MARK_KEY] == MARK + "/" + uid(api, ROOT)


def test_a_host_that_does_not_answer_reads_unanswered_never_blank(kept):
    api, recorded = kept
    reset(host(api), request(tpm={"persistent": True}), recorded, REMOVAL, STEP)
    api.unanswered.add("*")
    observation = read(host(api), request(tpm={"persistent": True}), recorded, MARK)
    assert not observation["answered"] and not observation["postcondition"]


def test_a_mark_that_is_not_one_reads_as_none(kept):
    api, recorded = kept
    api.objects[VM_PATH]["metadata"]["annotations"][MARK_KEY] = "not a mark"
    assert read(host(api), request(tpm={"persistent": True}), recorded, MARK)["rootReset"] == ""
    with pytest.raises(ValueError):
        mark_of("not-an-operation", STEP)


def published(observation, observed, mark=MARK):
    return completion({"subject": "root-reset", "root_reset": mark, "observed": observed, "digest": DIGEST, "observation": observation})


def test_the_protocol_proves_a_reset_only_under_its_mark_over_a_blank_root(kept):
    api, recorded = kept
    ignored_changed, observation = reset(host(api), request(tpm={"persistent": True}), recorded, REMOVAL, STEP)
    evidence, refusal = published(observation, False)
    assert refusal is None and evidence["postcondition"] and evidence["rootReset"] == MARK and evidence["request"] == DIGEST
    assert set(evidence) == {"answered", "instance", "kubeSystemUID", "owned", "postcondition", "request", "root", "rootReset", "virtualMachine"}
    ignored_evidence, refusal = published(dict(observation, root=dict(observation["root"], volume="")), False)
    assert "rootReset" in refusal
    ignored_evidence, refusal = published(observation, False, mark=OTHER + "/" + STEP)
    assert "rootReset" in refusal
    ignored_evidence, refusal = published(dict(observation, root=dict(observation["root"], blank=False)), False)
    assert "rootReset" in refusal
    evidence, refusal = published(dict(observation, rootReset="", postcondition=False), True)
    assert refusal is None and not evidence["postcondition"]
    with pytest.raises(ValueError):
        published(observation, True, mark="not-a-mark")


def test_the_action_reads_without_writing_and_resets_only_outside_check_mode(kept):
    api, recorded = kept
    arguments = {"request": request(tpm={"persistent": True}), "pinned": recorded["pinned"], "removal": REMOVAL, "step": STEP, "seconds": 60}
    clock = {"now": 0.0}
    tick = {"clock": lambda: clock["now"], "sleep": lambda seconds: clock.update(now=clock["now"] + seconds)}
    changed, observation = action.root("root_read", client(api), arguments, **tick)
    assert not changed and not observation["postcondition"] and api.writes() == []
    changed, observation = action.root("root_reset", client(api), arguments, **tick)
    assert changed and observation["postcondition"] and completed(observation, arguments["request"], recorded, MARK)


def tasks_of(name):
    return yaml.safe_load((ROLE / "tasks" / name).read_text())


def test_the_reset_entry_point_writes_only_through_the_reset_and_publishes_its_mark():
    tasks = tasks_of("root_reset.yml")
    modules = [key for task in tasks for key in task if key.startswith(("bootwright.", "ansible."))]
    assert modules == ["ansible.builtin.assert", "bootwright.core.substrate_kubevirt_protocol", "bootwright.core.substrate_kubevirt_api",
                       "ansible.builtin.assert", "bootwright.core.substrate_kubevirt_protocol"]
    assert tasks[2]["bootwright.core.substrate_kubevirt_api"]["operation"] == "root_reset"
    published_task = tasks[4]["bootwright.core.substrate_kubevirt_protocol"]
    assert published_task["subject"] == "root-reset" and "observed" not in published_task
    specs = yaml.safe_load((ROLE / "meta" / "argument_specs.yml").read_text())["argument_specs"]
    assert specs["root_reset"]["options"] == specs["destroy"]["options"]
    playbook = yaml.safe_load((COLLECTION / "playbooks" / "substrate" / "kubevirt_machine_root_reset.yml").read_text())
    assert playbook[0]["tasks"][0]["ansible.builtin.import_role"]["tasks_from"] == "root_reset"


def test_the_observation_reads_the_root_disk_without_writing_it_and_ends_there():
    tasks = tasks_of("observe.yml")
    readings = [task["bootwright.core.substrate_kubevirt_api"] for task in tasks if "bootwright.core.substrate_kubevirt_api" in task]
    assert [reading["operation"] for reading in readings] == ["root_read", "observe"]
    ending = next(index for index, task in enumerate(tasks) if "ansible.builtin.meta" in task)
    machine = next(index for index, task in enumerate(tasks) if task.get("bootwright.core.substrate_kubevirt_api", {}).get("operation") == "observe")
    assert tasks[ending]["ansible.builtin.meta"] == "end_host" and ending < machine
    assert tasks[ending]["when"] == "bootwright_substrate_kubevirt_machine_material.rootReset is defined"
