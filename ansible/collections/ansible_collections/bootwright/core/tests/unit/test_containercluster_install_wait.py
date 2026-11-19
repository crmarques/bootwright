"""Every message an installer wait fails with, byte for byte.

Each scenario is a scripted installer, (rc, stderr, seconds) per run, what the
restore before each attempt does, and what the read of the registered hosts
after a stall answers. The exact message each scenario produces is pinned,
with the outcome it is classified as.
"""

from __future__ import annotations

from types import SimpleNamespace

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import containercluster_install_wait as waiting
from ansible_collections.bootwright.core.plugins.modules import containercluster_install_inspect as inspection
from ansible_collections.bootwright.core.tests.unit import test_containercluster_budgets as budgets

TIMED_OUT = budgets.TIMED_OUT
STALLED = budgets.STALLED
HOST_ERROR = budgets.HOST_ERROR
UNRESUMABLE = "level=fatal msg=\"failed to fetch the cluster\""
INIT = "level=error msg=\"failed to initialize the cluster: timed out waiting for the condition\""
ONE = {"read": True, "unregistered": ["master-1"]}
TWO_UNREADY = {"read": True, "unregistered": ["master-1", "master-2"], "unready": [
    {"name": "master-0", "status": "insufficient", "message": "Host is\tnot ready\x1b[0m",
     "validations": ["ntp-synced", "has-inventory"]}]}
UNREADY = {"read": True, "unregistered": [], "unready": [
    {"name": "master-0", "status": "error", "message": "Host failed", "validations": []},
    {"name": "master-2", "status": "disconnected", "message": "", "validations": ["connected"]}]}
FAILED_READ = {"read": False, "unregistered": []}

SCENARIOS = {
    "reached": dict(script=[(0, "", 600)]),
    "host error": dict(script=[(1, HOST_ERROR, 600)]),
    "host error stopped": dict(script=[(1, STALLED, 60), (1, HOST_ERROR, 10**6)]),
    "unclassified": dict(script=[(1, UNRESUMABLE, 600)]),
    "timeout": dict(script=[(1, TIMED_OUT, 4690)]),
    "cluster init resumed then timeout": dict(script=[(1, INIT, 60), (1, TIMED_OUT, 4690)]),
    "budget spent": dict(script=[(0, "", 10**6)]),
    "budget spent by a kill": dict(script=[(0, "", 10**6)], stopped=-9),
    "stall": dict(script=[(1, STALLED, 1)] * 41),
    "stall then stopped": dict(script=[(1, STALLED, 60), (0, "", 10**6)]),
    "stall then timeout": dict(script=[(1, STALLED, 60), (1, TIMED_OUT, 10**6)], stopped=137),
    "stall read one": dict(script=[(1, STALLED, 60), (0, "", 10**6)], registration=ONE),
    "stall read one no stop": dict(script=[(1, STALLED, 1)] * 41, registration=ONE),
    "timeout read two unready": dict(script=[(1, TIMED_OUT, 4690)], registration=TWO_UNREADY),
    "spent read unready": dict(script=[(0, "", 10**6)], registration=UNREADY),
    "stall failed read": dict(script=[(1, STALLED, 60), (0, "", 10**6)], registration=FAILED_READ),
    "install stall": dict(script=[(1, STALLED, 60), (0, "", 10**6)], milestone="install-complete", registration=ONE),
    "restored then unclassified": dict(script=[(1, UNRESUMABLE, 60)], restores=["restored"]),
    "restored then reached": dict(script=[(0, "", 60)], restores=["restored"]),
    "restore refused": dict(script=[(0, "", 60)], restores=["refused"]),
    "restore refused install": dict(script=[(0, "", 60)], restores=["refused"], milestone="install-complete"),
}


EXPECTED = {
    'reached':
        '',
    'host error':
        'the assisted service moved a declared host into error and stopped installing this cluster, so '
        'waiting again would watch a state that cannot change. A cluster diverted into recovery is never '
        'pivoted out of bootstrap, so an install left short of a declared node does not finish on its '
        'own. Repair the host, destroy this context and perform a fresh apply.\nThe installer did not '
        'reach bootstrap-complete for sno.',
    'host error stopped':
        'the assisted service moved a declared host into error and stopped installing this cluster, so '
        'waiting again would watch a state that cannot change. A cluster diverted into recovery is never '
        'pivoted out of bootstrap, so an install left short of a declared node does not finish on its '
        'own. Repair the host, destroy this context and perform a fresh apply.\nThe installer did not '
        'reach bootstrap-complete for sno.',
    'unclassified':
        'this give-up is not one the installer can be resumed after, so the wait ran once and stopped. '
        'Read the installer output before repeating the apply.\nThe installer did not reach '
        'bootstrap-complete for sno.',
    'timeout':
        'the installer stopped watching on its own compiled deadline, and this wait had no budget or '
        'attempt left to re-invoke it. The cluster keeps converging across that give-up, so the operator '
        'conditions reported here are its state at the last give-up rather than proof of a failed '
        'install; repeat the apply to resume watching where this attempt stopped.\nThe installer did not '
        'reach bootstrap-complete for sno.',
    'cluster init resumed then timeout':
        "this wait's budget of 4703 seconds was spent while the installer was still watching, so timeout "
        'stopped it: the installer did not give up on its own. The cluster keeps converging after the '
        'wait is stopped, so this is no proof of a failed install; repeat the apply to resume watching '
        'under a fresh budget.\nThe installer did not reach bootstrap-complete for sno.',
    'budget spent':
        "this wait's budget of 4703 seconds was spent while the installer was still watching, so timeout "
        'stopped it: the installer did not give up on its own. The cluster keeps converging after the '
        'wait is stopped, so this is no proof of a failed install; repeat the apply to resume watching '
        'under a fresh budget.\nThe installer did not reach bootstrap-complete for sno.',
    'budget spent by a kill':
        "this wait's budget of 4703 seconds was spent while the installer was still watching, so timeout "
        'stopped it: the installer did not give up on its own. The cluster keeps converging after the '
        'wait is stopped, so this is no proof of a failed install; repeat the apply to resume watching '
        'under a fresh budget.\nThe installer did not reach bootstrap-complete for sno.',
    'stall':
        'the cluster reached ready but the rendezvous node never started installing. It starts only once '
        'every declared node reports known, so a node that never booted the image, whose declared '
        'hardware address is not the one it registers with, or whose install disk was rejected holds the '
        'cluster there.\nThe installer did not reach bootstrap-complete for sno.',
    'stall then stopped':
        'the cluster reached ready but the rendezvous node never started installing. It starts only once '
        'every declared node reports known, so a node that never booted the image, whose declared '
        'hardware address is not the one it registers with, or whose install disk was rejected holds the '
        "cluster there. The installer was re-invoked after that give-up, and this wait's budget of 4703 "
        'seconds stopped that attempt.\nThe installer did not reach bootstrap-complete for sno.',
    'stall then timeout':
        'the cluster reached ready but the rendezvous node never started installing. It starts only once '
        'every declared node reports known, so a node that never booted the image, whose declared '
        'hardware address is not the one it registers with, or whose install disk was rejected holds the '
        "cluster there. The installer was re-invoked after that give-up, and this wait's budget of 4703 "
        'seconds stopped that attempt.\nThe installer did not reach bootstrap-complete for sno.',
    'stall read one':
        'node master-1 never registered with the assisted service on the rendezvous host, so the cluster '
        'reached ready but the rendezvous node never started installing: it starts installing only once '
        'every declared node reports known. A node that never booted the image, or whose declared '
        'hardware address is not the one it registers with, keeps the cluster from installing. the '
        'cluster reached ready but the rendezvous node never started installing. It starts only once '
        'every declared node reports known, so a node that never booted the image, whose declared '
        'hardware address is not the one it registers with, or whose install disk was rejected holds the '
        "cluster there. The installer was re-invoked after that give-up, and this wait's budget of 4703 "
        'seconds stopped that attempt.\nThe installer did not reach bootstrap-complete for sno.',
    'stall read one no stop':
        'node master-1 never registered with the assisted service on the rendezvous host, so the cluster '
        'reached ready but the rendezvous node never started installing: it starts installing only once '
        'every declared node reports known. A node that never booted the image, or whose declared '
        'hardware address is not the one it registers with, keeps the cluster from installing. the '
        'cluster reached ready but the rendezvous node never started installing. It starts only once '
        'every declared node reports known, so a node that never booted the image, whose declared '
        'hardware address is not the one it registers with, or whose install disk was rejected holds the '
        'cluster there.\nThe installer did not reach bootstrap-complete for sno.',
    'timeout read two unready':
        'nodes master-1, master-2 never registered with the assisted service on the rendezvous host. The '
        'rendezvous node starts installing only once every declared node reports known. A node that never '
        'booted the image, or whose declared hardware address is not the one it registers with, keeps the '
        'cluster from installing. node master-0 is insufficient: Host isnot ready[0m; failing '
        'validations: ntp-synced, has-inventory. the installer stopped watching on its own compiled '
        'deadline, and this wait had no budget or attempt left to re-invoke it. The cluster keeps '
        'converging across that give-up, so the operator conditions reported here are its state at the '
        'last give-up rather than proof of a failed install; repeat the apply to resume watching where '
        'this attempt stopped.\nThe installer did not reach bootstrap-complete for sno.',
    'spent read unready':
        'node master-0 is error: Host failed; failing validations: . node master-2 is disconnected: ; '
        "failing validations: connected. this wait's budget of 4703 seconds was spent while the installer "
        'was still watching, so timeout stopped it: the installer did not give up on its own. The cluster '
        'keeps converging after the wait is stopped, so this is no proof of a failed install; repeat the '
        'apply to resume watching under a fresh budget.\nThe installer did not reach bootstrap-complete '
        'for sno.',
    'stall failed read':
        'the cluster reached ready but the rendezvous node never started installing. It starts only once '
        'every declared node reports known, so a node that never booted the image, whose declared '
        'hardware address is not the one it registers with, or whose install disk was rejected holds the '
        "cluster there. The installer was re-invoked after that give-up, and this wait's budget of 4703 "
        'seconds stopped that attempt.\nThe installer did not reach bootstrap-complete for sno.',
    'install stall':
        'the cluster reached ready but the rendezvous node never started installing. It starts only once '
        'every declared node reports known, so a node that never booted the image, whose declared '
        'hardware address is not the one it registers with, or whose install disk was rejected holds the '
        "cluster there. The installer was re-invoked after that give-up, and this wait's budget of 4703 "
        'seconds stopped that attempt.\nThe installer did not reach install-complete for sno.',
    'restored then unclassified':
        'this give-up is not one the installer can be resumed after, so the wait ran once and stopped. '
        'Read the installer output before repeating the apply.\nThe installer did not reach '
        "bootstrap-complete for sno. This apply restored the installer's kubeconfig from the kept copy "
        'before waiting.',
    'restored then reached':
        '',
    'restore refused':
        "the installer's administrator kubeconfig in the work area of cluster sno no longer parses, and "
        'the copy this installation kept names no identity or another one than this apply recorded before '
        'its first effect, so it was not restored and the installer did not wait for bootstrap-complete. '
        "Destroy the cluster's installation first: bootwright destroy --context lab ejects its media and "
        'discards the work area, and the apply after it builds the image afresh',
    'restore refused install':
        "the installer's administrator kubeconfig in the work area of cluster sno no longer parses, and "
        'the copy this installation kept names no identity or another one than this apply recorded before '
        'its first effect, so it was not restored and the installer did not wait for install-complete. '
        "Destroy the cluster's installation first: bootwright destroy --context lab ejects its media and "
        'discards the work area, and the apply after it builds the image afresh',
}


class Scripted(budgets.Wait):
    """The inspection's wait with the restore and the registered-host read answering as a scenario says."""

    def __init__(self, scenario):
        super().__init__(scenario["script"], stopped=scenario.get("stopped", 124), nodes=budgets.NODES)
        self.answers = list(scenario.get("restores") or [])
        self.registration = scenario.get("registration", FAILED_READ)

    def read(self):
        self.reads.append(self.registration)
        return self.registration

    def restore(self):
        outcome = self.answers.pop(0) if self.answers else budgets.UNTOUCHED
        self.restores.append(outcome)
        return outcome


def outcome(scenario):
    wait = Scripted(scenario)
    return wait.run(scenario.get("milestone", "bootstrap-complete")), wait


@pytest.mark.parametrize("name", list(SCENARIOS))
def test_each_wait_fails_with_its_pinned_message(name):
    message, unused = outcome(SCENARIOS[name])
    assert (message or "") == EXPECTED[name], repr(message)


OUTCOMES = {
    "reached": "reached", "host error": "host-error", "host error stopped": "host-error",
    "unclassified": "unclassified", "timeout": "timeout", "cluster init resumed then timeout": "budget-spent",
    "budget spent": "budget-spent", "budget spent by a kill": "budget-spent", "stall": "stalled",
    "stall then stopped": "stalled", "stall then timeout": "stalled", "stall read one": "stalled",
    "stall read one no stop": "stalled", "timeout read two unready": "timeout", "spent read unready": "budget-spent",
    "stall failed read": "stalled", "install stall": "stalled", "restored then unclassified": "unclassified",
    "restored then reached": "reached", "restore refused": "restore-refused", "restore refused install": "restore-refused",
}


# Each outcome is one of the typed outcomes; only a failed bootstrap stall,
# timeout or spent budget reads the registered hosts, once; a refused restore
# runs no installer.
@pytest.mark.parametrize("name", list(SCENARIOS))
def test_each_wait_is_classified_as_one_typed_outcome(name):
    scenario = SCENARIOS[name]
    unused, wait = outcome(scenario)
    assert wait.result["outcome"] == OUTCOMES[name] and OUTCOMES[name] in waiting.OUTCOMES
    reads = scenario.get("milestone", "bootstrap-complete") == "bootstrap-complete" and OUTCOMES[name] in (
        "stalled", "budget-spent", "timeout")
    assert len(wait.reads) == (1 if reads else 0)
    assert ("registration" in wait.result) is reads
    assert (wait.runs == []) is (OUTCOMES[name] == "restore-refused")
    assert wait.result["restored"] is ("restored" in (scenario.get("restores") or []))


# The task runs the inspection, which runs the installer through the bounded
# runner and leaves the fail to the role.
def test_the_inspection_waits_through_the_bounded_runner(run_module, monkeypatch, tmp_path):
    runs = []

    def run(argv, data, timeout, limit, environment):
        runs.append(argv)
        return (1, b"", UNRESUMABLE.encode()) if "install-complete" in argv else (0, b"", b"")

    monkeypatch.setattr(waiting.bounded_process, "run", run)
    request = dict(budgets.INSTALL_REQUEST, workRoot=str(tmp_path), nodes=[],
                   image={"path": str(tmp_path / "served"), "url": "https://192.0.2.1:8443"})
    arguments = {"request": request, "wait": "install-complete", "budget": 600, "installer": "/opt/installer",
                 "identity": "a" * 64}
    result = run_module(inspection, arguments)
    assert result["outcome"] == "unclassified" and result["rc"] == 1 and result["changed"] is False
    assert result["message"].startswith("this give-up is not one the installer can be resumed after")
    assert [argv[3:7] for argv in runs] == [["/opt/installer", "agent", "wait-for", "install-complete"]]
    assert runs[0][0] == "/usr/bin/timeout" and runs[0][8] == str(tmp_path)
    assert run_module(inspection, dict(arguments, restored=True))["message"].endswith(
        "This apply restored the installer's kubeconfig from the kept copy before waiting.")
    assert run_module(inspection, dict(arguments, wait="bootstrap-complete"))["outcome"] == "reached"
    refused = run_module(inspection, dict(arguments, registered=True))
    assert refused["failed"] is True and "exclusive" in refused["msg"]
    monkeypatch.setattr(waiting.pwd, "getpwuid", lambda uid: SimpleNamespace(pw_dir=""))
    homeless = run_module(inspection, arguments)
    assert homeless["failed"] is True and homeless["msg"] == "the account running the installer wait has no absolute home directory"
    assert len(runs) == 3
