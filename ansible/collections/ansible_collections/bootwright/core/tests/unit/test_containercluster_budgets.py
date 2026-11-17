"""Every cluster budget bounds its phase in wall-clock time.

The media request freezes the build budget and the install request the boot,
bootstrap and install budgets (internal/containercluster/agentinstall,
requests.go), and each run's deadline is derived from them. A phase that only
stops starting new attempts once its budget is spent overruns it by a whole
installer timeout, and a read that never gives up holds the run until its
deadline, so these checks pin how each role spends what it froze: the image
build and every installer wait run under timeout with what their budget has
left, recomputed for each attempt, a stopped one is diagnosed as the budget
spent, the boot phase gives every node only what is left of one budget, and
every cluster read gives up on a request after a bounded time.

GNU timeout exits 124 once it stopped the command, and a kill after
--kill-after also ends timeout's own process group, which a shell reports as
137 and Python's subprocess as -9 (the timeout invocation in the GNU coreutils
manual,
https://www.gnu.org/software/coreutils/manual/html_node/timeout-invocation.html,
and subprocess.Popen.returncode in the Python documentation).

How a wait goes on from one attempt to the next is checked by running the
inspection's wait (plugins/module_utils/containercluster_install_wait.py)
against a scripted installer and a simulated clock (Wait below), so the gate,
the pause, the remaining time and the record step are proved together. The
same run restores the installer's own
kubeconfig through the inspection's restore before each attempt (B220, D54),
over a real work area when a test gives it one. A wait diagnosed as a stall
then reads once which declared nodes registered with the assisted service on
the rendezvous host (B32, D49), through the inspection's own read against a
service the test serves on 127.0.0.1, and its failure leads with each node that
did not; any other wait reads nothing.

Rendering the task files needs Ansible's controller (DataLoader and Templar),
which ansible-test does not offer to unit tests under tests/unit/plugins.
"""

from __future__ import annotations

import base64
import datetime
import hashlib
import http.server
import json
import pathlib
import threading
import time
from types import SimpleNamespace
from unittest import mock

import pytest
from ansible.template import Templar

from ansible_collections.bootwright.core.plugins.module_utils import containercluster_cluster_read as cluster_read
from ansible_collections.bootwright.core.plugins.module_utils import containercluster_install_wait as waiting
from ansible_collections.bootwright.core.plugins.modules import containercluster_install_inspect as inspection
from ansible_collections.bootwright.core.plugins.modules.containercluster_install_inspect import (
    AGENT_CONFIG, AUTH_CONFIG, KEPT, RESTORED, STATE, UNTOUCHED, registration, restore)
from ansible_collections.bootwright.core.tests.unit import role_tasks
from ansible_collections.bootwright.core.tests.unit.role_tasks import LOADER

ROLES = pathlib.Path(__file__).resolve().parents[2] / "roles"
INSTALL = ROLES / "containercluster_install_agent"
MEDIA = ROLES / "containercluster_media_agent"

# Distinct values, so each phase is proved to read its own budget.
INSTALL_BUDGETS = {"bootSeconds": 311, "bootstrapSeconds": 4703, "fanOut": 4, "installSeconds": 5903, "roundMarginSeconds": 83}
MEDIA_BUDGETS = {"buildSeconds": 1709}
INSTALL_REQUEST = {"budgets": INSTALL_BUDGETS, "identity": {"cluster": "sno", "context": "lab"},
                   "workRoot": "/var/lib/bootwright-hosted/lab/clusters/sno"}
SPENT = (124, 137, -9)
CLIENT = "{{ containercluster_install_agent_client }}"
TIMED_OUT = "level=fatal msg=\"bootstrap process timed out\""
STALLED = "level=fatal msg=\"failed to progress after all hosts available\""
HOST_ERROR = "level=info msg=\"cluster has hosts in error\"\n" + TIMED_OUT
INSPECT = "bootwright.core.containercluster_install_inspect"


def pem(kind, body):
    return ("-----BEGIN %s-----\n%s\n-----END %s-----\n" % (kind, body, kind)).encode()


def encoded(data):
    return base64.b64encode(data).decode()


ADMIN = pem("CERTIFICATE", "YWRtaW4=")
OTHER = pem("CERTIFICATE", "b3RoZXI=")


def kubeconfig(client=ADMIN):
    """auth/kubeconfig as AgentAdminClient writes it with the image, in the
    layout kubeconfig() in the inspection's own tests builds from the installer
    (pkg/asset/kubeconfig/agent.go and kubeconfig.go, release-4.21), with
    placeholder certificate bodies the inspection hashes and never parses."""
    return ((
        "clusters:\n"
        "- cluster:\n"
        "    certificate-authority-data: %s\n"
        "    server: https://api.sno.lab.example:6443\n"
        "  name: sno\n"
        "contexts:\n"
        "- context:\n"
        "    cluster: sno\n"
        "    user: admin\n"
        "  name: admin\n"
        "current-context: admin\n"
        "users:\n"
        "- name: admin\n"
        "  user:\n"
        "    client-certificate-data: %s\n"
        "    client-key-data: %s\n"
    ) % (encoded(pem("CERTIFICATE", "bG9jYWxob3N0")), encoded(client),
         encoded(pem("RSA PRIVATE KEY", "a2V5")))).encode()


def anchor(client=ADMIN):
    return hashlib.sha256(b"bootwright/containercluster/identity/v1\0" + client).hexdigest()


def trusted(path):
    return [task for task in LOADER.load_from_file(str(path), trusted_as_template=True) if isinstance(task, dict)]


def found(role, name, predicate):
    matches = [task for task in role_tasks.flatten(trusted(role / "tasks" / name)) if predicate(task)]
    assert len(matches) == 1, "%s has %d tasks that match" % (name, len(matches))
    return matches[0]


def argv(task):
    """A command's arguments as loaded, so each templated one still renders."""
    command = task.get("ansible.builtin.command")
    return list((command or {}).get("argv") or []) if isinstance(command, dict) else []


def scope(role, **variables):
    """The role's defaults, a request freezing the test budgets, and variables."""
    values = dict(LOADER.load_from_file(str(role / "defaults" / "main.yml"), trusted_as_template=True))
    values["bootwright_cluster_install_request"] = dict(INSTALL_REQUEST)
    values["bootwright_cluster_install_material"] = {"openshiftinstall": "/installer", "oc": "/oc"}
    values["bootwright_cluster_media_request"] = {
        "budgets": MEDIA_BUDGETS, "identity": {"cluster": "sno"}, "workRoot": "/var/lib/bootwright-hosted/lab/clusters/sno"}
    values["bootwright_cluster_media_material"] = {"installer": "/installer"}
    values.update(variables)
    return Templar(loader=LOADER, variables=values)


def now():
    return int(time.time())


def test_every_budget_a_request_freezes_is_read_by_its_role():
    for role, block in ((INSTALL, "install"), (MEDIA, "media")):
        specs = LOADER.load_from_file(str(role / "meta" / "argument_specs.yml"))["argument_specs"]
        budgets = {entry: set(spec["options"]["bootwright_cluster_%s_request" % block]["options"]["budgets"]["options"])
                   for entry, spec in specs.items()}
        assert set(budgets) == {"apply", "observe", "destroy"}
        expected = set(INSTALL_BUDGETS if block == "install" else MEDIA_BUDGETS)
        assert all(declared == expected for declared in budgets.values()), budgets
        text = "".join(path.read_text() for path in sorted((role / "tasks").glob("*.yml")))
        if block == "install":
            text += (ROLES.parent / "playbooks/containercluster/install_apply.yml").read_text()
        for name in expected - {"roundMarginSeconds"}:
            assert "bootwright_cluster_%s_request.budgets.%s" % (block, name) in text, name
        # roundMarginSeconds bounds the invocation supervisor in Go, never a role task.


def test_each_wait_reads_its_own_phases_budget():
    for milestone, budget in (("bootstrap-complete", "bootstrapSeconds"), ("install-complete", "installSeconds")):
        wait = found(INSTALL, "waits.yml", lambda task, milestone=milestone: (task.get("vars") or {}).get(
            "containercluster_install_agent_milestone") == milestone)
        assert wait["ansible.builtin.include_tasks"] == "wait.yml"
        assert int(scope(INSTALL).template(wait["vars"]["containercluster_install_agent_budget"])) == INSTALL_BUDGETS[budget]


def wait_task():
    return found(INSTALL, "wait.yml", lambda task: (task.get(INSPECT) or {}).get("wait") is not None)


# The wait is one task and one failure: the inspection runs every attempt,
# takes the time each may run from the deadline the budget sets once, and
# classifies the result; wait.yml binds the frozen budget and reports.
def test_each_installer_wait_is_one_inspection_bound_to_its_phases_budget_and_one_failure():
    tasks = trusted(INSTALL / "tasks" / "wait.yml")
    assert [next(key for key in task if "." in key) for task in tasks] == [INSPECT, "ansible.builtin.fail"]
    task = wait_task()
    assert task[INSPECT]["wait"] == "{{ containercluster_install_agent_milestone }}"
    assert task[INSPECT]["budget"] == "{{ containercluster_install_agent_budget }}"
    assert task[INSPECT]["installer"] == "{{ containercluster_install_agent_installer }}"
    assert task["no_log"] is True and "failed_when" not in task
    for task in role_tasks.flatten(tasks):
        assert not {"until", "retries", "loop"} & set(task), task.get("name")
    report = tasks[1]
    assert report["ansible.builtin.fail"]["msg"] == "{{ containercluster_install_agent_wait.message }}"
    assert report["when"] == "containercluster_install_agent_wait.outcome != 'reached'"
    assert not (INSTALL / "tasks" / "wait_attempt.yml").exists()


def attempted(rc, stderr=""):
    """The outcome of a wait whose only attempt ended so, and whether another attempt followed it."""
    runs = []

    def attempt(seconds):
        runs.append(seconds)
        return (rc, stderr) if len(runs) == 1 else (0, "")

    clock = Clock()
    record = waiting.attempts(4703, attempt, lambda: UNTOUCHED, lambda: clock.seconds,
                              lambda seconds: setattr(clock, "seconds", clock.seconds + seconds))
    return record, len(runs) > 1


def failure(rc, stderr, stalled=False):
    """The message a wait whose last attempt ended so fails with, or None when it reached the milestone."""
    record = {"rc": rc, "stderr": stderr, "stalled": stalled, "restored": False, "refused": False}
    outcome = waiting.classify(record)
    if outcome == "reached":
        return None
    return waiting.message(outcome, record, INSTALL_REQUEST, "bootstrap-complete", 4703)


@pytest.mark.parametrize("rc", SPENT)
def test_an_attempt_the_budget_stopped_is_the_budget_spent_and_never_resumed(rc):
    # Its output carries a give-up the installer could be resumed after, so
    # only the exit status keeps the wait from going on.
    assert attempted(rc)[1] is False
    assert attempted(rc, TIMED_OUT)[1] is False
    message = failure(rc, TIMED_OUT)
    assert message.startswith("this wait's budget of 4703 seconds was spent while the installer was still watching")


def test_only_a_give_up_the_installer_can_resume_from_is_resumed():
    assert attempted(1, TIMED_OUT)[1] is True
    assert attempted(1, STALLED)[1] is True
    # Success ends the wait even when its output recorded an earlier give-up.
    assert attempted(0, TIMED_OUT)[1] is False
    # A host in error ends it although the same output carries a resumable give-up.
    assert attempted(1, HOST_ERROR)[1] is False
    assert attempted(1, "level=fatal msg=\"failed to fetch the cluster\"")[1] is False


def test_the_budget_spent_is_told_apart_from_the_installers_own_give_ups():
    assert attempted(1, TIMED_OUT)[1] is True
    assert failure(1, TIMED_OUT).startswith("the installer stopped watching on its own compiled deadline")
    # No attempt ran because nothing of the budget was left.
    assert failure(None, "").startswith("this wait's budget of 4703 seconds was spent")
    # A host in error stopped the installation whatever stopped the wait.
    for rc in (1,) + SPENT:
        assert failure(rc, "cluster has hosts in error").startswith("the assisted service moved a declared host into error")
    assert failure(0, "") is None
    record = attempted(0)[0]
    assert (record["rc"], record["stderr"], waiting.classify(record)) == (0, "", "reached")


# An attempt runs the installer's wait under timeout with what is left,
# through the bounded runner, whose own bound lies past the kill-after grace.
def test_an_attempt_runs_the_installer_under_timeout_with_what_is_left(monkeypatch):
    runs = []

    def run(argv, data, timeout, limit, environment):
        runs.append((argv, timeout, environment))
        return 124, b"", "level=fatal msg=\"bootstrap process timed out\"".encode()

    monkeypatch.setattr(waiting.bounded_process, "run", run)
    monkeypatch.setenv("HOME", "/ambient/home")
    monkeypatch.setenv("HTTPS_PROXY", "http://proxy.example.test:3128")
    monkeypatch.setattr(waiting.pwd, "getpwuid", lambda uid: SimpleNamespace(pw_dir="/var/lib/account"))
    assert waiting.installer("/installer", "bootstrap-complete", "/work")(17) == (124, TIMED_OUT)
    (argv, timeout, environment), = runs
    assert argv == ["/usr/bin/timeout", "--kill-after=30s", "17s", "/installer", "agent", "wait-for",
                    "bootstrap-complete", "--dir", "/work", "--log-level", "info"]
    assert 17 + 30 < timeout <= 17 + 60
    # The account's own home from the password database, and nothing ambient.
    assert environment == {"HOME": "/var/lib/account", "LANG": "C.UTF-8", "PATH": "/usr/bin:/bin"}

    def stalled(argv, data, timeout, limit, environment):
        raise waiting.bounded_process.Failure("timeout")

    monkeypatch.setattr(waiting.bounded_process, "run", stalled)
    assert waiting.installer("/installer", "install-complete", "/work")(17)[0] in SPENT


# An account with no absolute home refuses before the installer runs.
@pytest.mark.parametrize("entry", [SimpleNamespace(pw_dir=""), SimpleNamespace(pw_dir="relative/home"), None],
                         ids=["empty", "relative", "no entry"])
def test_an_account_without_an_absolute_home_runs_no_installer(monkeypatch, entry):
    def getpwuid(uid):
        if entry is None:
            raise KeyError(uid)
        return entry

    monkeypatch.setattr(waiting.pwd, "getpwuid", getpwuid)
    monkeypatch.setattr(waiting.bounded_process, "run", lambda *arguments, **keywords: pytest.fail("the installer ran"))
    with pytest.raises(waiting.NoHome):
        waiting.installer("/installer", "bootstrap-complete", "/work")


class Clock:
    """The integer seconds a simulated wait reads, and the now() the rendered templates read, so neither takes time.

    It answers as ansible-core's own now() does (_now in
    _internal/_templating/_jinja_plugins.py): a naive datetime, in UTC when utc
    is true and in the controller's local time otherwise, so a template that
    reads a naive value as local time is caught here as it would fail there.
    """

    def __init__(self, seconds=1_000_000):
        self.seconds = seconds

    def __call__(self, utc=False, fmt=None):
        if utc:
            moment = datetime.datetime.fromtimestamp(self.seconds, datetime.timezone.utc).replace(tzinfo=None)
        else:
            moment = datetime.datetime.fromtimestamp(self.seconds)
        return moment.strftime(fmt) if fmt else moment


class Wait:
    """Runs the inspection's wait against a scripted installer and a simulated clock.

    Each scripted installer run is (rc, stderr, seconds); one that would run
    past the time it was given is stopped there, exiting with `stopped`, 124
    at the deadline or, for a kill, one grace later.

    The restore before each attempt runs the inspection's own restore over
    `work` with the identity the apply recorded, and `seen` keeps the
    installer's kubeconfig as each run found it. Without a work area it
    answers as the inspection does over an installer file that parses. The
    read after a stall runs the inspection's own read over `work`, for a
    request declaring `nodes`, and `reads` keeps what each read reported;
    without a work area it answers as a read that failed.
    """

    def __init__(self, script, budget=4703, stopped=124, work=None, identity="", nodes=None):
        self.clock = Clock()
        self.script = list(script)
        self.budget = budget
        self.stopped = stopped
        self.work = work
        self.identity = identity
        self.request = dict(INSTALL_REQUEST, nodes=nodes or [])
        self.result = {}
        self.runs = []
        self.pauses = []
        self.restores = []
        self.reads = []
        self.seen = []
        self.started = None

    def run(self, milestone="bootstrap-complete"):
        """Wait for one milestone; the message it failed with, or None when it was reached."""
        self.started = self.clock.seconds
        self.result = waiting.wait(self.request, milestone, self.budget, self.installer, self.restore, self.read,
                                   self.result.get("restored", False), lambda: self.clock.seconds, self.pause)
        return self.result.get("message")

    def pause(self, seconds):
        self.pauses.append(seconds)
        self.clock.seconds += seconds

    def restore(self):
        outcome = UNTOUCHED if self.work is None else restore(dict(self.request, workRoot=str(self.work)), self.identity)
        self.restores.append(outcome)
        return outcome

    def read(self):
        if self.work is None:
            reported = {"read": False, "unregistered": []}
        else:
            reported = registration(dict(self.request, workRoot=str(self.work)))
        self.reads.append(reported)
        return reported

    def installer(self, given):
        assert self.script, "the installer was run more often than the %d times scripted" % len(self.runs)
        rc, stderr, seconds = self.script.pop(0)
        self.runs.append((self.clock.seconds - self.started, given))
        if self.work is not None:
            self.seen.append((self.work / "auth" / "kubeconfig").read_bytes())
        if seconds >= given:
            rc = self.stopped
            seconds = given if rc == 124 else given + waiting.GRACE
        self.clock.seconds += seconds
        return rc, stderr


def test_a_wait_that_reaches_its_milestone_runs_the_installer_once():
    wait = Wait([(0, "", 600)])
    assert wait.run() is None
    assert wait.runs == [(0, 4703)] and wait.pauses == []


@pytest.mark.parametrize("stderr, hint", [
    (HOST_ERROR, "the assisted service moved a declared host into error"),
    ("level=fatal msg=\"failed to fetch the cluster\"", "this give-up is not one the installer can be resumed after"),
], ids=["host-error", "not-resumable"])
def test_a_give_up_the_installer_cannot_resume_from_runs_it_once(stderr, hint):
    wait = Wait([(1, stderr, 600)])
    assert wait.run().startswith(hint)
    assert len(wait.runs) == 1 and wait.pauses == []


def test_each_attempt_is_given_what_the_budget_has_left_when_it_starts():
    wait = Wait([(1, TIMED_OUT, 600), (1, STALLED, 1200), (0, "", 100)])
    assert wait.run() is None
    # 30 seconds apart, and each given the budget less what the attempts and
    # pauses before it took.
    assert wait.pauses == [30, 30]
    assert wait.runs == [(0, 4703), (630, 4073), (1860, 2843)]


def test_a_give_up_near_the_deadline_pauses_only_for_what_is_left_and_runs_no_more():
    wait = Wait([(1, TIMED_OUT, 4690)])
    assert wait.run().startswith("the installer stopped watching on its own compiled deadline")
    assert wait.runs == [(0, 4703)] and wait.pauses == [13]
    assert wait.clock.seconds - wait.started == 4703


def test_repeated_give_ups_end_with_the_budget():
    wait = Wait([(1, TIMED_OUT, 20)] * 41, budget=100)
    assert wait.run().startswith("the installer stopped watching on its own compiled deadline")
    assert wait.runs == [(0, 100), (50, 50)] and wait.pauses == [30, 30]


def test_quick_give_ups_end_with_the_last_attempt():
    wait = Wait([(1, TIMED_OUT, 1)] * 41)
    assert wait.run().startswith("the installer stopped watching on its own compiled deadline")
    assert len(wait.runs) == 41 and wait.pauses == [30] * 40


@pytest.mark.parametrize("stopped", SPENT)
def test_a_wait_the_budget_stops_ends_within_it_and_one_grace(stopped):
    wait = Wait([(0, "", 10**6)], stopped=stopped)
    assert wait.run().startswith("this wait's budget of 4703 seconds was spent while the installer was still watching")
    assert wait.runs == [(0, 4703)]
    assert wait.clock.seconds - wait.started <= 4703 + 30


@pytest.mark.parametrize("stopped", SPENT)
def test_a_stall_stays_the_diagnosis_when_the_budget_stops_the_attempt_after_it(stopped):
    wait = Wait([(1, STALLED, 60), (0, "", 10**6)], stopped=stopped)
    message = wait.run()
    assert message.startswith("the cluster reached ready but the rendezvous node never started installing")
    assert "this wait's budget of 4703 seconds stopped that attempt" in message
    assert wait.runs == [(0, 4703), (90, 4613)]


def test_a_later_give_up_of_another_kind_replaces_the_stall():
    wait = Wait([(1, STALLED, 60), (1, TIMED_OUT, 60), (0, "", 10**6)])
    assert wait.run().startswith("this wait's budget of 4703 seconds was spent while the installer was still watching")


def test_a_host_in_error_is_the_diagnosis_even_after_a_stall():
    wait = Wait([(1, STALLED, 60), (1, HOST_ERROR, 60)])
    assert wait.run().startswith("the assisted service moved a declared host into error")
    assert len(wait.runs) == 2


def test_the_next_wait_starts_afresh():
    wait = Wait([(1, STALLED, 60), (0, "", 600), (1, TIMED_OUT, 100), (0, "", 100)])
    assert wait.run("bootstrap-complete") is None
    assert wait.run("install-complete") is None
    assert len(wait.runs) == 4 and wait.runs[2:] == [(0, 4703), (130, 4573)]
    # A stall the first wait recovered from is not the second one's diagnosis.
    wait = Wait([(1, STALLED, 60), (0, "", 600), (0, "", 10**6)])
    assert wait.run("bootstrap-complete") is None
    assert wait.run("install-complete").startswith("this wait's budget of 4703 seconds was spent")


def work_area(root, installer, kept):
    """A work area holding the installer's kubeconfig and the copy the installation kept."""
    (root / "auth").mkdir(parents=True)
    (root / "auth" / "kubeconfig").write_bytes(installer)
    (root / KEPT).write_bytes(kept)
    return root


# clientcmd.WriteToFile writes through os.WriteFile, which truncates before it
# writes, so a budget kill during the installer's rewrite leaves any prefix of
# the file, the empty one included (the client-go loader.go the installer
# vendors at release-4.21). The next wait puts the kept copy back first, and the
# installer then runs over the whole file and reaches its milestone.
@pytest.mark.parametrize("cut", [0, 1, 200, -1], ids=["empty", "one byte", "inside the authority", "no final newline"])
def test_a_wait_over_a_truncated_installer_kubeconfig_restores_it_and_continues(tmp_path, cut):
    whole = kubeconfig()
    work = work_area(tmp_path / "work", whole[:cut], whole)
    wait = Wait([(0, "", 600)], work=work, identity=anchor())
    assert wait.run() is None
    assert wait.restores == [RESTORED] and wait.seen == [whole]
    assert wait.result["restored"] is True
    assert (work / KEPT).read_bytes() == whole


# A kept copy that no longer names the identity the apply took from it before
# its first effect, whether it names another cluster's or none, is never handed
# to the installer: the wait refuses before the installer runs, naming the
# destroy that discards the work area, and changes nothing.
@pytest.mark.parametrize("kept", [kubeconfig(OTHER), b"", kubeconfig()[:-40]],
                         ids=["another cluster", "empty", "cut short"])
def test_a_wait_whose_kept_copy_names_another_cluster_refuses_before_the_installer_runs(tmp_path, kept):
    cut = kubeconfig()[:200]
    work = work_area(tmp_path / "work", cut, kept)
    wait = Wait([(0, "", 600)], work=work, identity=anchor())
    message = wait.run()
    assert message.startswith("the installer's administrator kubeconfig in the work area of cluster sno no longer "
                              "parses, and the copy this installation kept names no identity or another one")
    assert "did not wait for bootstrap-complete" in message and "bootwright destroy --context lab" in message
    assert wait.restores == ["refused"] and wait.runs == []
    assert (work / "auth" / "kubeconfig").read_bytes() == cut and (work / KEPT).read_bytes() == kept
    assert wait.result["restored"] is False


# A file that parses is the installer's to read, even one naming another
# identity, so the wait leaves it as it is and records no restore.
@pytest.mark.parametrize("installer", [kubeconfig(), kubeconfig(OTHER)], ids=["this build's", "another identity"])
def test_a_wait_over_an_installer_kubeconfig_that_parses_leaves_it_as_it_is(tmp_path, installer):
    work = work_area(tmp_path / "work", installer, kubeconfig())
    wait = Wait([(1, TIMED_OUT, 60), (0, "", 600)], work=work, identity=anchor())
    assert wait.run() is None
    assert wait.restores == [UNTOUCHED, UNTOUCHED] and wait.seen == [installer, installer]
    assert wait.result["restored"] is False


# B32, D49. The asset state holds the two members the read takes, in the shape
# openshift-install 4.21.10 wrote them (the inspection's own tests show the
# whole entries), and the service on 127.0.0.1 answers the cluster list with
# the members the assisted-service models the installer vendors name: hosts,
# and each host's inventory, the JSON document its agent reports. The token
# is a placeholder. The request and its header are spelled here, as the
# inspection's own tests spell them, rather than taken from the module.
TOKEN = "eyJhbGciOiJFUzI1NiIsInR5cCI6IkpXVCJ9.cGxhY2Vob2xkZXI.c2lnbmF0dXJl"
READ = ("/api/assisted-install/v2/clusters?with_hosts=true", TOKEN)
NODES = [{"name": "master-0", "address": "127.0.0.1"}, {"name": "master-1", "address": "192.0.2.11"},
         {"name": "master-2", "address": "192.0.2.12"}]


def asset_state(root, state=True):
    """A work area whose installer kubeconfig parses, holding the installer's
    asset state, which names 127.0.0.1 as the rendezvous address."""
    work_area(root, kubeconfig(), kubeconfig())
    if not state:
        return root
    (root / STATE).write_text(json.dumps({
        AUTH_CONFIG: {"WatcherAuthToken": TOKEN, "AuthType": "agent-installer-local"},
        AGENT_CONFIG: {"Config": {"kind": "AgentConfig", "rendezvousIP": "127.0.0.1"}}}))
    return root


def listed(*names):
    """The service's one cluster, with one known host reporting each name."""
    return json.dumps([{"id": "6a4ba3e8-0000-4000-8000-000000000000", "status": "ready", "hosts": [
        {"status": "known", "inventory": json.dumps({"hostname": name})} for name in names]}]).encode()


class Service(http.server.BaseHTTPRequestHandler):
    """The assisted service on the rendezvous host: it records each request and answers as told."""

    def do_GET(self):
        self.server.seen.append((self.path, self.headers.get("Watcher-Authorization")))
        status, body = self.server.answer
        self.send_response(status)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *args):
        return


@pytest.fixture(name="service")
def service_fixture(request, monkeypatch):
    server = http.server.HTTPServer(("127.0.0.1", 0), Service)
    server.seen, server.answer = [], (200, listed("master-0"))
    thread = threading.Thread(target=server.serve_forever, kwargs={"poll_interval": 0.05}, daemon=True)
    thread.start()
    monkeypatch.setattr(inspection, "SERVICE_PORT", server.server_address[1])

    def stop():
        server.shutdown()
        server.server_close()
        thread.join()

    request.addfinalizer(stop)
    return server


# The read carries the watcher token, so it runs inside the inspection's
# wait, under no_log, and it never fails the wait itself: a read that fails
# leaves the stall's own diagnosis (below).
def test_the_read_after_a_stall_runs_inside_the_wait_under_no_log():
    assert wait_task()["no_log"] is True
    for path in sorted((INSTALL / "tasks").glob("*.yml")):
        assert not [task for task in role_tasks.flatten(trusted(path)) if (task.get(INSPECT) or {}).get("registered")], path


# The installer's two give-ups that mark a stall name no host. The read names
# each declared node no registered host carries the name of, and the failure
# leads with it, also when the budget stopped the attempt after the stall.
@pytest.mark.serial
@pytest.mark.parametrize("registered, named", [
    (("master-0", "master-2"), "node master-1 never registered"),
    (("master-0",), "nodes master-1, master-2 never registered"),
], ids=["one node", "two nodes"])
def test_a_stall_names_each_declared_node_that_never_registered(tmp_path, service, registered, named):
    service.answer = (200, listed(*registered))
    wait = Wait([(1, STALLED, 60), (0, "", 10**6)], work=asset_state(tmp_path / "work"), nodes=NODES)
    message = wait.run()
    assert message.startswith(named + " with the assisted service on the rendezvous host, so the cluster "
                              "reached ready but the rendezvous node never started installing")
    assert "this wait's budget of 4703 seconds stopped that attempt" in message
    assert message.endswith("The installer did not reach bootstrap-complete for sno.")
    assert service.seen == [READ] and TOKEN not in message


@pytest.mark.serial
def test_a_wait_that_stalls_again_and_again_reads_once(tmp_path, service):
    service.answer = (200, listed("master-0", "master-1"))
    wait = Wait([(1, STALLED, 1)] * 41, work=asset_state(tmp_path / "work"), nodes=NODES)
    message = wait.run()
    assert message.startswith("node master-2 never registered with the assisted service")
    assert "stopped that attempt" not in message
    assert len(wait.runs) == 41 and len(wait.reads) == 1 and service.seen == [READ]


# A read that fails, or that finds every declared node registered by the time
# it runs, names no node and leaves the stall's own hint.
@pytest.mark.serial
@pytest.mark.parametrize("answer, state", [
    ((200, listed("master-0", "master-1", "master-2")), True),
    ((401, b'{"code":"401","reason":"unauthorized"}'), True),
    ((200, b"not json"), True),
    ((200, listed("master-0")), False),
], ids=["every node registered", "unauthorized", "not json", "no asset state"])
def test_a_read_that_names_no_node_keeps_the_stalls_own_hint(tmp_path, service, answer, state):
    service.answer = answer
    wait = Wait([(1, STALLED, 60), (1, STALLED, 60), (0, "", 10**6)], work=asset_state(tmp_path / "work", state),
                nodes=NODES)
    message = wait.run()
    assert message.startswith("the cluster reached ready but the rendezvous node never started installing")
    assert "this wait's budget of 4703 seconds stopped that attempt" in message
    assert len(wait.reads) == 1 and wait.reads[0]["read"] is (answer[0] == 200 and answer[1] != b"not json" and state)
    assert service.seen == ([READ] if state else [])


@pytest.mark.serial
def test_a_rendezvous_host_that_does_not_answer_keeps_the_stalls_own_hint(tmp_path, monkeypatch):
    closed = http.server.HTTPServer(("127.0.0.1", 0), Service)
    port = closed.server_address[1]
    closed.server_close()
    monkeypatch.setattr(inspection, "SERVICE_PORT", port)
    wait = Wait([(1, STALLED, 60)] + [(1, STALLED, 4690)], work=asset_state(tmp_path / "work"), nodes=NODES)
    assert wait.run().startswith("the cluster reached ready but the rendezvous node never started installing")
    assert wait.reads == [{"read": False, "unregistered": []}]


# Successful waits, host errors and unrelated give-ups read nothing.
@pytest.mark.parametrize("script", [
    [(0, "", 600)],
    [(1, STALLED, 60), (0, "", 600)],
    [(1, HOST_ERROR, 60)],
    [(1, STALLED, 60), (1, HOST_ERROR, 60)],
    [(1, "level=fatal msg=\"failed to fetch the cluster\"", 60)],
], ids=["reached", "reached after a stall", "host error", "host error after a stall", "not resumable"])
def test_success_host_errors_and_unrelated_failures_do_not_read_registered_hosts(tmp_path, service, script):
    wait = Wait(script, work=asset_state(tmp_path / "work"), nodes=NODES)
    message = wait.run()
    assert message is None or not message.startswith(("node", "the cluster reached ready"))
    assert wait.reads == [] and service.seen == []


# The budget stopped the attempt after the stall, so the wait keeps that stall,
# but the output of the stopped attempt shows a host in error: the wait is
# diagnosed as the host in error, and only the host-error clause of the read's
# condition keeps the token from being sent.
def test_a_host_in_error_after_a_stall_reads_nothing_when_the_budget_stopped_its_attempt(tmp_path, service):
    wait = Wait([(1, STALLED, 60), (1, HOST_ERROR, 10**6)], work=asset_state(tmp_path / "work"), nodes=NODES)
    message = wait.run()
    assert wait.result["rc"] == 124 and wait.result["outcome"] == "host-error"
    assert message.startswith("the assisted service moved a declared host into error and stopped installing")
    assert wait.reads == [] and service.seen == []


def build():
    return found(MEDIA, "build.yml", lambda task: argv(task)[4:7] == ["agent", "create", "image"])


def test_the_image_is_built_under_timeout_with_the_budget_its_request_froze():
    builds = [task for path in sorted((MEDIA / "tasks").glob("*.yml")) for task in role_tasks.flatten(trusted(path))
              if "create" in argv(task) and "image" in argv(task)]
    assert builds == [build()]
    command = argv(build())
    block = found(MEDIA, "build.yml", lambda task: build() in (task.get("block") or []))
    rendering = scope(MEDIA, containercluster_media_agent_started=1000,
                      containercluster_media_agent_build_now=1017, **block["vars"])
    remaining = next(task["ansible.builtin.set_fact"]["containercluster_media_agent_build_remaining"]
                     for task in block["block"] if "containercluster_media_agent_build_remaining" in task.get("ansible.builtin.set_fact", {}))
    value = int(rendering.template(remaining))
    assert value == MEDIA_BUDGETS["buildSeconds"] - 17
    rendering = scope(MEDIA, containercluster_media_agent_build_remaining=value, **block["vars"])
    assert command[0] == "/usr/bin/timeout"
    grace = rendering.template(command[1])
    assert grace.startswith("--kill-after=") and 0 < int(grace[len("--kill-after="):-1]) <= 60
    assert rendering.template(command[2]) == "%ds" % (MEDIA_BUDGETS["buildSeconds"] - 17)
    assert command[3] == "{{ containercluster_media_agent_installer }}"


@pytest.mark.parametrize("rc, stopped", [(0, False), (1, False), (2, False)] + [(rc, True) for rc in SPENT])
def test_a_build_the_budget_stopped_fails_as_the_budget_spent_and_any_other_as_the_installers(rc, stopped):
    block = found(MEDIA, "build.yml", lambda task: build() in (task.get("block") or []))
    tasks = block["block"]
    refusal = tasks[tasks.index(build()) + 1]
    rendering = scope(MEDIA, containercluster_media_agent_built={"rc": rc}, **block["vars"])
    failed = all(rendering.evaluate_conditional(condition) for condition in build()["failed_when"])
    assert failed is (rc != 0 and not stopped)
    assert rendering.evaluate_conditional(refusal["when"]) is stopped
    assert rendering.template(refusal["ansible.builtin.fail"]["msg"]).startswith(
        "the installer was still building the boot image of cluster sno when its build budget of 1709 seconds was spent")


def test_every_cluster_read_gives_up_on_a_request_after_a_bounded_time(monkeypatch):
    # The role runs no oc of its own: its cluster read is the inspection's,
    # whose every oc run passes --request-timeout and is itself bounded.
    for path in sorted((INSTALL / "tasks").glob("*.yml")):
        assert not [task for task in role_tasks.flatten(trusted(path)) if argv(task)[:1] == [CLIENT]], path.name
    read = found(INSTALL, "cluster_state.yml", lambda task: (task.get(INSPECT) or {}).get("cluster") is True)
    assert read[INSPECT]["client"] == CLIENT
    runs = []

    def run(command, data, timeout, limit, environment):
        runs.append((command, timeout))
        return 1, b"", b""

    monkeypatch.setattr(cluster_read.native.bounded_process, "run", run)
    cluster_read.read("/oc", "/work/kubeconfig", {"release": {"version": "4.21.0"}, "nodes": []}, "")
    assert len(runs) == 2
    for command, timeout in runs:
        bounds = [value for value in command if value.startswith("--request-timeout")]
        assert bounds == ["--request-timeout=30s"] and 0 < timeout <= 30, command


def skip():
    return found(INSTALL, "boot.yml", lambda task: "containercluster_install_agent_state.powered" in str(
        task.get("when", "")))


def bounded():
    return found(INSTALL, "boot.yml", lambda task: "timeout" in task and "block" in task)


def test_the_boot_phase_deadline_is_set_once_before_the_first_node():
    prepare = trusted(INSTALL / "tasks" / "prepare_boot.yml")
    setting = [index for index, task in enumerate(prepare)
               if "containercluster_install_agent_boot_deadline" in (task.get("ansible.builtin.set_fact") or {})]
    marking = [index for index, task in enumerate(prepare)
               if str((task.get("ansible.builtin.copy") or {}).get("dest", "")).endswith("/.bootwright-booted")]
    assert len(setting) == len(marking) == 1 and marking[0] < setting[0]
    assert "loop" not in prepare[setting[0]]
    plays = trusted(ROLES.parent / "playbooks/containercluster/install_apply.yml")
    phases = [play["tasks"][0]["ansible.builtin.import_role"]["tasks_from"] for play in plays]
    assert phases.index("prepare_boot") < phases.index("node_boot")
    boot = plays[phases.index("node_boot")]
    assert boot["hosts"] == "bootwright_nodes"
    assert "hostvars[bootwright_controller_host]" in boot["vars"]["containercluster_install_agent_boot_deadline"]
    deadline = prepare[setting[0]]["ansible.builtin.set_fact"]["containercluster_install_agent_boot_deadline"]
    rendered = int(scope(INSTALL).template(deadline))
    assert now() + INSTALL_BUDGETS["bootSeconds"] - 2 <= rendered <= now() + INSTALL_BUDGETS["bootSeconds"]
    assert "containercluster_install_agent_boot_deadline:" not in (INSTALL / "tasks" / "boot.yml").read_text()


def test_every_boot_step_runs_under_what_the_boot_phase_has_left():
    steps = [task for task in role_tasks.flatten(bounded()["block"])
             if (task.get("ansible.builtin.include_tasks") == "port.yml"
                 and task.get("vars", {}).get("containercluster_install_agent_port_operation") == "media_insert")
             or "ansible.builtin.include_role" in task]
    effects = [task for task in role_tasks.flatten(skip()["block"])
               if (task.get("ansible.builtin.include_tasks") == "port.yml"
                   and task.get("vars", {}).get("containercluster_install_agent_port_operation") == "media_insert")
               or "ansible.builtin.include_role" in task]
    assert steps == effects and len(steps) == 4
    assert bounded() in skip()["block"]
    # ansible-core reads a timeout of 0 as none at all (_alarm_timeout.py:
    # `if not timeout: return`), so a step reached at or past the deadline
    # still gets one second rather than no bound.
    for left, want in ((297, 297), (1, 1), (0, 1), (-40, 1)):
        rendering = scope(INSTALL, containercluster_install_agent_boot_deadline=now() + left)
        assert max(1, want - 2) <= int(rendering.template(bounded()["timeout"])) <= want
    # A refusal of the node's pre-boot proof is named between the two
    # (test_containercluster_pre_boot.py), after the budget, which it never is.
    rescue = bounded()["rescue"]
    stopped, named, other = rescue[0], rescue[-2], rescue[-1]
    assert "containercluster_install_agent_lifecycle_refusal" in named["ansible.builtin.set_fact"]
    rendering = scope(INSTALL, containercluster_install_agent_node={"machine": "sno-01"},
                      ansible_failed_result={"failed": True, "timedout": {"period": 12}})
    assert rendering.evaluate_conditional(stopped["when"])
    assert rendering.template(stopped["ansible.builtin.fail"]["msg"]).startswith(
        "the boot phase's budget of 311 seconds was spent before every node was booted")
    assert not scope(INSTALL, ansible_failed_result={"failed": True, "msg": "refused"}).evaluate_conditional(
        stopped["when"])
    assert "when" not in other and "ansible.builtin.fail" in other


@pytest.mark.parametrize("left", [120, -30])
def test_a_node_the_skip_leaves_alone_takes_none_of_the_boot_budget(left):
    guard = found(INSTALL, "boot.yml", lambda task: "ansible.builtin.fail" in task
                  and "containercluster_install_agent_boot_deadline" in str(task.get("when", "")))
    assert guard in skip()["block"] and skip()["block"].index(guard) < skip()["block"].index(bounded())
    for powered, boots in ((["sno-01"], False), ([], True)):
        rendering = scope(INSTALL, containercluster_install_agent_boot_deadline=now() + left,
                          containercluster_install_agent_node={"machine": "sno-01"},
                          containercluster_install_agent_state={"powered": powered, "ownMedia": ["sno-01"]})
        # A node already running from this image runs nothing, so a spent
        # budget neither stops it nor is spent on it.
        assert rendering.evaluate_conditional(skip()["when"]) is boots
        assert (boots and rendering.evaluate_conditional(guard["when"])) is (boots and left < 0)


@pytest.fixture(name="lisbon")
def fixture_lisbon(monkeypatch):
    """A controller whose local time observes daylight saving (Europe/Lisbon)."""
    monkeypatch.setenv("TZ", "Europe/Lisbon")
    time.tzset()
    yield
    monkeypatch.undo()
    time.tzset()


def test_every_budget_clock_counts_true_seconds_across_a_daylight_saving_change(lisbon):
    """Lisbon leaves summer time at 2026-10-25T01:00Z; a clock read as local time jumps an hour there."""
    assert lisbon is None
    start = int(datetime.datetime(2026, 10, 25, 0, 30, tzinfo=datetime.timezone.utc).timestamp())
    clock = Clock(start)
    # The installer wait counts epoch seconds, which no time zone moves.
    with mock.patch.object(waiting.time, "time", lambda: start + 0.5):
        assert waiting.now() == start
    boot = found(INSTALL, "prepare_boot.yml",
                 lambda task: "containercluster_install_agent_boot_deadline" in (task.get("ansible.builtin.set_fact") or {}))
    assert int(scope(INSTALL, now=clock).template(
        boot["ansible.builtin.set_fact"]["containercluster_install_agent_boot_deadline"])) == start + INSTALL_BUDGETS["bootSeconds"]
    stepping = found(INSTALL, "boot.yml", lambda task: "timeout" in task and "block" in task)
    proving = [task["ansible.builtin.wait_for"] for task in role_tasks.flatten(trusted(INSTALL / "tasks" / "readiness.yml"))
               if "ansible.builtin.wait_for" in task]
    assert [proof["port"] for proof in proving] == [22, 8090]
    for elapsed in (1799, 1800, 1801, 3600, 7199, 7200, 9000):
        clock.seconds = start + elapsed
        for proof in proving:
            left = int(scope(INSTALL, now=clock, containercluster_install_agent_boot_deadline=start + 7200).template(
                proof["timeout"]))
            assert left == max(7200 - elapsed, 1)
    for elapsed in (1799, 1800, 1801, 3600):
        clock.seconds = start + elapsed
        step = int(scope(INSTALL, now=clock, containercluster_install_agent_boot_deadline=start + 7200).template(
            stepping["timeout"]))
        assert step == 7200 - elapsed


def test_a_failed_wait_names_the_kubeconfig_it_restored(tmp_path):
    work = work_area(tmp_path / "work", kubeconfig()[:200], kubeconfig())
    wait = Wait([(1, HOST_ERROR, 60)], work=work, identity=anchor())
    message = wait.run()
    assert wait.restores == [RESTORED]
    assert "restored the installer's kubeconfig from the kept copy before waiting" in message


@pytest.mark.serial
@pytest.mark.parametrize("script", [
    [(0, "", 10**6)],
    [(1, TIMED_OUT, 60), (1, TIMED_OUT, 4690)],
    [(1, STALLED, 60), (1, TIMED_OUT, 60), (0, "", 10**6)],
])
def test_bootstrap_timeout_or_budget_spent_reads_hosts_once(tmp_path, service, script):
    wait = Wait(script, work=asset_state(tmp_path / "work"), nodes=NODES)
    message = wait.run()
    assert message.startswith("nodes master-1, master-2 never registered")
    assert "reached ready" not in message and "never started installing" not in message
    assert service.seen == [READ]
    assert TOKEN not in message


# The installer gives up on its own compiled deadline just short of the
# budget, and no attempt follows: only its timeout makes the wait read, and a
# cluster held short of every declared node never reached ready.
@pytest.mark.serial
def test_the_installers_own_timeout_reads_hosts_and_names_no_stall(tmp_path, service):
    wait = Wait([(1, TIMED_OUT, 4690)], work=asset_state(tmp_path / "work"), nodes=NODES)
    message = wait.run()
    assert wait.runs == [(0, 4703)] and wait.pauses == [13]
    assert wait.result["rc"] == 1 and wait.result["outcome"] == "timeout"
    assert len(wait.reads) == 1 and service.seen == [READ]
    assert message.startswith("nodes master-1, master-2 never registered with the assisted service on the "
                              "rendezvous host. The rendezvous node starts installing only once every declared "
                              "node reports known.")
    assert "reached ready" not in message and "never started installing" not in message
    assert "the installer stopped watching on its own compiled deadline" in message
    assert TOKEN not in message


def test_install_wait_timeout_never_reads_assisted_hosts(tmp_path, service):
    wait = Wait([(0, "", 10**6)], work=asset_state(tmp_path / "work"), nodes=NODES)
    wait.run(milestone="install-complete")
    assert wait.reads == [] and service.seen == []


@pytest.mark.serial
def test_bootstrap_diagnosis_leads_with_missing_then_bad_state_nodes(tmp_path, service):
    hosts = json.loads(listed("master-0", "master-2"))
    hosts[0]["hosts"][0].update(
        status="insufficient", status_info="disk is too small",
        validations_info=json.dumps({"hardware": [{"id": "has-min-valid-disks", "status": "failure"}]}),
    )
    service.answer = (200, json.dumps(hosts).encode())
    wait = Wait([(0, "", 10**6)], work=asset_state(tmp_path / "work"), nodes=NODES)
    message = wait.run()
    assert message.startswith("node master-1 never registered")
    assert "node master-0 is insufficient: disk is too small" in message
    assert "has-min-valid-disks" in message
    assert message.index("master-1") < message.index("master-0")
    assert service.seen == [READ]


def test_compact_retry_boots_only_the_stopped_or_unbooted_nodes():
    state = {"powered": ["master-0"], "ownMedia": ["master-0", "master-1"]}
    booted = []
    for name in ("master-0", "master-1", "master-2"):
        rendering = scope(INSTALL, containercluster_install_agent_node={"machine": name},
                          containercluster_install_agent_state=state)
        if rendering.evaluate_conditional(skip()["when"]):
            booted.append(name)
    assert booted == ["master-1", "master-2"]
