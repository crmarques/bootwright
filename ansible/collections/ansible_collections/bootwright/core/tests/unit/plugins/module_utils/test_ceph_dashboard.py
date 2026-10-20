"""The Ceph dashboard phase keeps its administrator password in protected custody.

These temporary-filesystem tests inject native effects, root metadata and the
bcrypt verifier; they do not execute Ceph or open a network connection.
"""

from __future__ import annotations

import base64
import contextlib
import copy
import hashlib
import json
import os
import stat
from types import SimpleNamespace

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_native

CERTIFICATE_DER = b"synthetic-self-signed-dashboard-certificate"
CERTIFICATE_PEM = (b"-----BEGIN CERTIFICATE-----\n" + base64.encodebytes(CERTIFICATE_DER) + b"-----END CERTIFICATE-----\n")
URL = "https://node-1.example.test:8443/"


class Markers:
    def __init__(self, request):
        self.path = os.path.join(ceph_native.ROOT, request["fsid"])
        self.records = {}
        self.writes = []
        self.after = lambda name, value: None

    def owned(self):
        return True

    def read(self, name):
        return copy.deepcopy(self.records.get(name))

    def write(self, name, value):
        ceph_native.require(name not in self.records or self.records[name] == value, "ownership")
        if name in self.records:
            return
        self.records[name] = copy.deepcopy(value)
        self.writes.append(name)
        self.after(name, value)


def hashed(password):
    return "$2b$12$" + hashlib.sha256(password.encode()).hexdigest()


@pytest.fixture
def dashboard(tmp_path, monkeypatch):
    hosts = [{"name": "node-" + str(i), "machine": "machine-" + str(i), "address": "192.0.2." + str(i), "user": "operator", "port": 22,
              "devices": [{"path": "/dev/vdb", "serial": "disk" + str(i), "sizeBytes": 1024 ** 3}], "labels": [], "location": {},
              "sshKeyRef": "fleet-key"} for i in range(1, 4)]
    names = [host["name"] for host in hosts]
    request = {"controller": "controller", "version": "ceph-core-v22", "action": "cluster", "cluster": "example", "owner": "apply-example",
               "fsid": "4c0a4300-aa43-458c-86d7-ac2256d1fc00", "image": "registry.example.test/ceph@sha256:" + "a" * 64,
               "cephadm": {"path": "/usr/sbin/cephadm", "packageVersion": "20.2.1-405.el9cp"},
               "release": "9.1", "ansiblePackageVersion": "3.2.0",
               "packageRepository": "https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/9/rhel9/x86_64",
               "bootstrap": "node-1", "monIP": "192.0.2.1", "clusterSSHUser": "operator",
               "clusterSSHTransport": "native", "clusterKeyRef": "cluster-key", "registryRef": "registry-key",
               "registry": "registry.example.test", "callHome": "enabled", "publicNetworks": ["192.0.2.0/24"], "clusterNetworks": [], "hosts": hosts,
               "services": [{"service_type": "mon", "placement": {"hosts": names}},
                            {"service_type": "mgr", "placement": {"hosts": names, "count": 2}},
                            {"service_type": "osd", "service_id": "data", "placement": {"hosts": names}, "spec": {"data_devices": {"paths": ["/dev/vdb"]}}}]}
    root = tmp_path / "root"
    root.mkdir(mode=0o755)
    directory = root
    for part in (ceph_native.ROOT + "/" + request["fsid"]).strip("/").split("/"):
        directory /= part
        directory.mkdir(mode=0o700 if part == request["fsid"] else 0o755)
    original_open, original_fstat = os.open, os.fstat

    def open_file(path, flags, mode=0o777, *, dir_fd=None):
        if path == "/" and dir_fd is None:
            path = str(root)
        return original_open(path, flags, mode, dir_fd=dir_fd)

    def fstat(fd):
        actual = original_fstat(fd)
        fields = {name: getattr(actual, name) for name in dir(actual) if name.startswith("st_")}
        fields.update(st_uid=0, st_gid=0)
        return SimpleNamespace(**fields)

    monkeypatch.setattr(ceph_native.os, "open", open_file)
    monkeypatch.setattr(ceph_native.os, "fstat", fstat)
    monkeypatch.setattr(ceph_native.os, "fchown", lambda fd, uid, gid: ceph_native.require(uid == gid == 0))
    state = SimpleNamespace(calls=[], modules=[], config={}, keys=[], users={}, unloaded=0, generated=0, redeploys=0, scheduled=False, polls=0,
                            settle=0, creates=0, create_code=0, lose_create=False, crypt=True, imports=0, containers=["a1" * 6, "b2" * 6],
                            publication_delay=0, published=True, last_publication=False, ports={}, repairs=[], repaired={},
                            active="node-2.ghijkl", gid=1, ignored=set(), ignored_calls=[], events=[],
                            managers=2, deferred=0, pending=[], unavailable=0, orchestrating=0, served=True, starting=set(),
                            standbys=True, promotes=True)
    markers = Markers(request)

    def manager_rows():
        if state.scheduled and state.polls >= state.settle:
            containers = ["c3" * 6, "d4" * 6]
        else:
            containers = list(state.containers)
        return [{"daemon_type": "mgr", "daemon_id": host + "." + suffix, "hostname": host,
                 "container_id": state.repaired.get(host + "." + suffix, container),
                 # orchestrator/_interface.py:1270-1275 DaemonDescriptionStatus: running is 1, starting is 2.
                 "status": 2 if host + "." + suffix in state.starting else 1,
                 "ports": state.ports.get(host + "." + suffix, [8443, 8765])}
                for host, suffix, container in zip(("node-1", "node-2")[:state.managers], ("abcdef", "ghijkl"), containers)]

    def redeploy(daemon):
        if daemon == state.active:
            # cephadm module.py:3214-3216 fails the active MGR over itself and prepares the redeploy from an empty services map.
            pytest.fail("redeploy of the active manager " + daemon)
        state.events.append(("redeployed", daemon))
        assert daemon not in state.repairs
        state.repairs.append(daemon)
        state.repaired[daemon] = ("e5" if daemon.startswith("node-1.") else "f6") * 6
        state.ports[daemon] = [8443, 8765]

    def execute_pending():
        # serve.py:1696-1708 runs a scheduled redeploy in a later serve-loop pass, on whichever MGR is active by then.
        due = [daemon for daemon, poll in state.pending if poll <= state.polls]
        state.pending = [(daemon, poll) for daemon, poll in state.pending if poll > state.polls]
        for daemon in due:
            redeploy(daemon)

    def run(argv, data=b"", timeout=60):
        del timeout
        state.calls.append((list(argv), data))
        assert argv[0] == "/usr/bin/ceph"
        if "--format" in argv:
            args = argv[argv.index("--format") + 2:]
        else:
            args = argv[argv.index("--keyring") + 2:]
        if args[:3] == ["dashboard", "ac-user-create", "admin"]:
            assert args == ["dashboard", "ac-user-create", "admin", "-i", "-", "administrator"]
            state.creates += 1
            if state.create_code:
                return state.create_code, b"PRIVATE-NATIVE-ERROR " + data
            state.users["admin"] = {"username": "admin", "password": hashed(data.decode()), "roles": ["administrator"], "enabled": True,
                                    "pwdUpdateRequired": False}
            if state.lose_create:
                raise ceph_native.Refusal("native-timeout")
            return 0, b""
        assert data == b"", "only the user creation reads standard input"
        if args == ["fsid"]:
            value = {"fsid": request["fsid"]}
        elif args == ["mgr", "module", "ls"]:
            value = {"enabled_modules": list(state.modules), "always_on_modules": [], "force_disabled_modules": []}
        elif args == ["mgr", "module", "enable", "dashboard"]:
            state.modules.append("dashboard")
            state.unloaded = 2
            return 0, b""
        elif args == ["config", "dump"]:
            value = [{"section": "mgr", "name": name, "value": item, "mask": ""} for name, item in sorted(state.config.items())]
        elif args[:3] == ["config", "set", "mgr"]:
            state.config[args[3]] = args[4]
            return 0, b""
        elif args == ["config-key", "ls"]:
            value = list(state.keys)
        elif args == ["config-key", "get", "mgr/dashboard/crt"]:
            assert "mgr/dashboard/crt" in state.keys
            return 0, CERTIFICATE_PEM
        elif args == ["dashboard", "create-self-signed-cert"]:
            assert "dashboard" in state.modules and state.config == {"mgr/dashboard/ssl": "true", "mgr/dashboard/ssl_server_port": "8443"}
            if state.unloaded:
                state.unloaded -= 1
                return 95, b"Error ENOTSUP: Module 'dashboard' is not loaded"
            state.keys.append("mgr/dashboard/crt")
            return 0, b"Self-signed certificate created\n"
        elif args == ["orch", "ps", "--daemon_type", "mgr", "--refresh"]:
            state.polls += 1
            execute_pending()
            value = manager_rows()
        elif args == ["orch", "redeploy", "mgr"]:
            assert state.last_publication, "redeploy raced dashboard endpoint publication"
            assert "dashboard-redeploy.json" in markers.records
            state.redeploys += 1
            state.scheduled = True
            state.polls = 0
            return 0, b"Scheduled to redeploy mgr\n"
        elif args[:3] == ["orch", "daemon", "redeploy"]:
            daemon = args[3].removeprefix("mgr.")
            assert state.last_publication
            attempt = "dashboard-port-" + hashlib.sha256(daemon.encode()).hexdigest() + ".json"
            assert attempt in markers.records
            assert state.unavailable <= 0 and state.served, "redeploy before the new active orchestrator serves"
            state.events.append(("repair", daemon))
            if daemon in state.ignored:
                state.ignored_calls.append(daemon)
                assert len(state.ignored_calls) <= 4, "repeated repair submissions"
                return 0, b"Scheduled daemon redeploy"
            if state.deferred:
                state.pending.append((daemon, state.polls + state.deferred))
            else:
                redeploy(daemon)
            return 0, b"Scheduled daemon redeploy"
        elif args == ["mgr", "services"]:
            state.last_publication = state.published and state.publication_delay <= 0
            state.publication_delay -= 1
            value = {"dashboard": URL} if state.last_publication else {}
            state.events.append(("services", state.last_publication))
        elif args == ["mgr", "dump"] and state.unavailable > 0:
            # MgrMonitor.cc:924-929 drop_active empties the active name and gid and marks the map unavailable until a standby takes over.
            state.unavailable -= 1
            value = {"available": False, "active_name": "", "active_gid": 0, "standbys": []}
        elif args == ["mgr", "dump"]:
            value = {"available": True, "active_name": state.active, "active_gid": state.gid,
                     "standbys": [{"name": name} for name in ("node-1.abcdef", "node-2.ghijkl") if name != state.active and state.standbys]}
        elif args[:2] == ["mgr", "fail"]:
            # MgrMonitor.cc:930 drop_active clears the services map until the new active publishes its endpoints again.
            assert args[2:] == [state.active]
            state.events.append(("fail", state.active))
            if state.promotes:
                state.active = "node-1.abcdef" if state.active == "node-2.ghijkl" else "node-2.ghijkl"
            state.gid += 1
            state.publication_delay = 2
            state.unavailable, state.orchestrating, state.served = 2, 1, False
            return 0, b""
        elif args == ["orch", "status"]:
            state.served = state.orchestrating <= 0
            state.orchestrating -= 1
            value = {"available": state.served, "backend": "cephadm"}
            state.events.append(("orch-status", value["available"]))
        elif args == ["dashboard", "ac-user-show"]:
            value = sorted(state.users)
        elif args == ["dashboard", "ac-user-show", "admin"]:
            value = copy.deepcopy(state.users["admin"])
        else:
            pytest.fail("unexpected native command: " + repr(args))
        return 0, json.dumps(value).encode()

    class Crypt:
        @staticmethod
        def crypt(password, salt):
            assert salt.startswith("$2b$")
            return hashed(password)

    def import_module(name):
        assert name == "crypt"
        state.imports += 1
        if not state.crypt:
            raise ImportError(name)
        return Crypt

    monkeypatch.setattr(ceph_native.importlib, "import_module", import_module)
    original_password = ceph_native.dashboard_password

    def generate():
        state.generated += 1
        return original_password()

    monkeypatch.setattr(ceph_native, "dashboard_password", generate)
    clock = SimpleNamespace(value=0.0, pauses=[])

    def pause(seconds):
        clock.pauses.append(seconds)
        clock.value += seconds

    native = ceph_native.Worker(request, request["bootstrap"], run=run, markers=markers)
    native.verify_tool = lambda: {"synthetic-artifact": True}
    native.bootstrap_hold = contextlib.nullcontext
    native.clock = lambda: clock.value
    native.pause = pause
    return SimpleNamespace(native=native, state=state, markers=markers, request=request, directory=directory, clock=clock)


def retained(world):
    return (world.directory / "dashboard-material.json").read_bytes().decode("ascii")


def commands(world):
    return [argv[argv.index("--keyring") + 2:] for argv, unused in world.state.calls]


def mutations(world):
    reads = (["fsid"], ["mgr", "module", "ls"], ["config", "dump"], ["config-key", "ls"], ["config-key", "get", "mgr/dashboard/crt"],
             ["orch", "ps", "--daemon_type", "mgr", "--refresh"], ["mgr", "services"], ["dashboard", "ac-user-show"], ["dashboard", "ac-user-show", "admin"],
             ["mgr", "dump"], ["orch", "status"])
    return [args for args in commands(world) if [a for a in args if a not in ("--format", "json")] not in reads]


def test_first_apply_enables_tls_certifies_redeploys_once_and_creates_admin_on_stdin(dashboard):
    result = dashboard.native.dashboard()
    password = result["private"]
    assert result["changed"] is True and ceph_native.dashboard_policy(password) and password == retained(dashboard)
    assert stat.S_IMODE(os.stat(dashboard.directory / "dashboard-material.json").st_mode) == 0o600
    assert [args[2:] if args[:2] == ["--format", "json"] else args for args in mutations(dashboard)] == [
        ["mgr", "module", "enable", "dashboard"], ["config", "set", "mgr", "mgr/dashboard/ssl", "true"],
        ["config", "set", "mgr", "mgr/dashboard/ssl_server_port", "8443"], ["dashboard", "create-self-signed-cert"],
        ["dashboard", "create-self-signed-cert"], ["dashboard", "create-self-signed-cert"], ["orch", "redeploy", "mgr"],
        ["dashboard", "ac-user-create", "admin", "-i", "-", "administrator"]]
    assert dashboard.state.unloaded == 0 and dashboard.clock.pauses == [ceph_native.DASHBOARD_INTERVAL] * 2
    assert dashboard.state.redeploys == 1 and dashboard.state.creates == 1 and dashboard.state.generated == 1
    assert dashboard.markers.writes == ["dashboard-reserved.json", "dashboard-id.json", "dashboard-redeploy.json", "dashboard-redeployed.json",
                                        "dashboard-attempt.json", "dashboard-applied.json"]
    stdin = [data for argv, data in dashboard.state.calls if data]
    assert stdin == [password.encode()]
    assert all(password not in " ".join(argv) for argv, unused in dashboard.state.calls)
    assert password not in json.dumps(dashboard.markers.records)
    assert hashed(password) not in json.dumps(result) and set(result) == {"changed", "private"}


def test_a_converged_apply_repeats_no_effect_and_recaptures_the_same_password(dashboard):
    first = dashboard.native.dashboard()["private"]
    before = len(mutations(dashboard))
    again = dashboard.native.dashboard()
    assert again == {"changed": False, "private": first}
    assert len(mutations(dashboard)) == before and dashboard.state.generated == 1


@pytest.mark.parametrize("candidate,valid", [
    ("Xq7-Lm2_Rv9.Tk4-Wz8_Hn3.Pb6-Jd5_", True),
    ("Xq7-Lm2_Rv9.Tk4-Wz8_Hn3.Pb6-Jd5", False),
    ("Xq7-Lm2_Rv9.Tk4-Wz8_Hn3.Pb6-Jd5!", False),
    ("Xq7-Lm2_Rv9.Tk4-AdMiN_Hn3.Pb6-Jd", False),
    ("Xq7-Lm2_Rv9.Tk4-Wz8_CePh.Pb6-Jd5", False),
    ("Xq7-Lm2_Rv9.Tk4-Wz8_Hn3.Pb6-nfs_", False),
    ("Xq7-Lm2_Rv9.Tk4-Wz8_Hn3.Pb6-Jjj_", False),
    ("Xq7-Lm2_Rv9.Tk4-Wz8_Hn3.Pb6-Xyz_", False),
    ("Xq7-Lm2_Rv9.Tk4-Wz8_Hn3.Pb6-543_", False),
    ("Xq7-Lm2_Rv9.Tk4-Wz8_Hn3.Pb6-aBc_", False),
])
def test_generator_policy_is_the_local_copy_of_the_dashboard_rules(candidate, valid):
    assert ceph_native.dashboard_policy(candidate) is valid


def test_generator_draws_again_until_the_policy_holds():
    draws = iter("Xq7-Lm2_Rv9.Tk4-Wz8_CePh.Pb6-Jd5" + "Xq7-Lm2_Rv9.Tk4-Wz8_Hn3.Pb6-Jd5_")
    seen = []

    def choice(alphabet):
        assert alphabet == ceph_native.DASHBOARD_ALPHABET
        seen.append(next(draws))
        return seen[-1]

    assert ceph_native.dashboard_password(choice) == "Xq7-Lm2_Rv9.Tk4-Wz8_Hn3.Pb6-Jd5_"
    assert len(seen) == 64
    for unused in range(200):
        value = ceph_native.dashboard_password()
        assert len(value) == 32 and set(value) <= set(ceph_native.DASHBOARD_ALPHABET) and ceph_native.dashboard_policy(value)
    with pytest.raises(ceph_native.Refusal, match="^dashboard-policy$"):
        ceph_native.dashboard_password(lambda alphabet: "a")


def test_a_lost_creation_response_recaptures_the_retained_password_without_regenerating(dashboard):
    dashboard.state.lose_create = True
    with pytest.raises(ceph_native.Refusal, match="^native-timeout$"):
        dashboard.native.dashboard()
    assert dashboard.state.creates == 1 and "admin" in dashboard.state.users
    assert "dashboard-attempt.json" in dashboard.markers.records and "dashboard-applied.json" not in dashboard.markers.records
    first = retained(dashboard)
    dashboard.state.lose_create = False
    result = dashboard.native.dashboard()
    assert result["private"] == first and dashboard.state.generated == 1
    assert dashboard.state.creates == 1 and dashboard.state.redeploys == 1
    assert "dashboard-applied.json" in dashboard.markers.records


def test_an_interrupted_phase_before_the_user_creates_it_once_with_the_same_bytes(dashboard):
    dashboard.state.settle = 10 ** 6
    with pytest.raises(ceph_native.Refusal, match="^native-timeout$"):
        dashboard.native.dashboard()
    assert dashboard.clock.value >= ceph_native.DASHBOARD_BUDGET and dashboard.state.creates == 0 and dashboard.state.redeploys == 1
    first = retained(dashboard)
    dashboard.state.settle = 0
    dashboard.clock.value = 0
    assert dashboard.native.dashboard()["private"] == first
    assert dashboard.state.generated == 1 and dashboard.state.creates == 1 and dashboard.state.redeploys == 1


def test_an_unscheduled_redeploy_after_a_lost_response_is_scheduled_again(dashboard):
    real = dashboard.native.tunnel_effect
    lost = []

    def lose_scheduling(*arguments, **options):
        if arguments == ("orch", "redeploy", "mgr") and not lost:
            lost.append(arguments)
            raise ceph_native.Refusal("native-timeout")
        return real(*arguments, **options)

    dashboard.native.tunnel_effect = lose_scheduling
    with pytest.raises(ceph_native.Refusal, match="^native-timeout$"):
        dashboard.native.dashboard()
    assert lost and dashboard.state.redeploys == 0 and "dashboard-redeploy.json" in dashboard.markers.records
    dashboard.native.dashboard()
    assert dashboard.state.redeploys == 1 and "dashboard-redeployed.json" in dashboard.markers.records


def test_a_lost_redeploy_response_tolerates_replacement_during_the_publication_wait(dashboard):
    effect = dashboard.native.tunnel_effect
    lost = []

    def lose_reply(*arguments, **options):
        result = effect(*arguments, **options)
        if arguments == ("orch", "redeploy", "mgr") and not lost:
            # cephadm accepted the scheduling and has not run it yet; only its reply is lost.
            lost.append(arguments)
            dashboard.state.settle = 10 ** 6
            raise ceph_native.Refusal("native-timeout")
        return result

    dashboard.native.tunnel_effect = lose_reply
    with pytest.raises(ceph_native.Refusal, match="^native-timeout$"):
        dashboard.native.dashboard()
    assert lost and dashboard.state.redeploys == 1 and "dashboard-redeployed.json" not in dashboard.markers.records
    pause = dashboard.native.pause
    replacing = [MANAGER_ONE, MANAGER_TWO]

    def replace(seconds):
        pause(seconds)
        if replacing:
            # serve.py:1696-1708 runs the accepted redeploy one daemon per serve-loop pass.
            daemon = replacing.pop(0)
            dashboard.state.repaired[daemon] = ("e5" if daemon == MANAGER_ONE else "f6") * 6
            dashboard.state.events.append(("replaced", daemon))

    dashboard.state.publication_delay = 2
    dashboard.native.pause = replace
    dashboard.native.dashboard()
    assert replacing == [] and ("replaced", MANAGER_TWO) in dashboard.state.events
    assert dashboard.state.redeploys == 1 and "dashboard-redeployed.json" in dashboard.markers.records
    assert dashboard.state.creates == 1


@pytest.mark.parametrize("absence", ["starting", "missing"])
def test_a_manager_not_running_during_the_initial_schedule_wait_is_waited_out(dashboard, absence):
    def restart(name, unused_value):
        if name == "dashboard-redeploy.json":
            if absence == "starting":
                dashboard.state.starting = {MANAGER_ONE}
            else:
                # orch ps lists fewer MGRs than declared for one poll: a missing row is waited out like a starting one.
                dashboard.state.managers = 1

    pause = dashboard.native.pause

    def started(seconds):
        pause(seconds)
        dashboard.state.starting = set()
        dashboard.state.managers = 2

    dashboard.markers.after = restart
    dashboard.native.pause = started
    dashboard.native.dashboard()
    calls = [args[2:] if args[:2] == ["--format", "json"] else args for args in commands(dashboard)]
    scheduled = calls.index(["orch", "redeploy", "mgr"])
    assert dashboard.clock.pauses == [ceph_native.DASHBOARD_INTERVAL] * 3
    assert calls[:scheduled].count(["orch", "ps", "--daemon_type", "mgr", "--refresh"]) == 3
    assert dashboard.state.redeploys == 1 and "dashboard-redeployed.json" in dashboard.markers.records


def test_a_user_without_this_operations_attempt_marker_is_never_adopted(dashboard):
    dashboard.state.users["admin"] = {"username": "admin", "password": hashed("Xq7-Lm2_Rv9.Tk4-Wz8_Hn3.Pb6-Jd5_"), "roles": ["administrator"],
                                      "enabled": True, "pwdUpdateRequired": False}
    with pytest.raises(ceph_native.Refusal, match="^dashboard-identity$"):
        dashboard.native.dashboard()
    assert ["--format", "json", "dashboard", "ac-user-show"] in commands(dashboard)
    assert dashboard.state.creates == 0 and dashboard.state.imports == 0
    assert "dashboard-attempt.json" not in dashboard.markers.records and "dashboard-applied.json" not in dashboard.markers.records


def test_a_password_changed_outside_bootwright_is_drift_and_never_reset(dashboard):
    dashboard.native.dashboard()
    dashboard.state.users["admin"]["password"] = hashed("Operator-Chosen_Password.Value-9")
    before = len(mutations(dashboard))
    with pytest.raises(ceph_native.Refusal, match="^dashboard-drift$"):
        dashboard.native.dashboard()
    with pytest.raises(ceph_native.Refusal, match="^dashboard-drift$"):
        dashboard.native.dashboard_read()
    assert dashboard.state.imports == 3 and len(mutations(dashboard)) == before
    assert not any("ac-user-set-password" in args for args in commands(dashboard))


@pytest.mark.parametrize("change", ["roles", "enabled", "pwdUpdateRequired"])
def test_a_changed_account_is_drift(dashboard, change):
    dashboard.native.dashboard()
    dashboard.state.users["admin"][change] = {"roles": ["read-only"], "enabled": False, "pwdUpdateRequired": True}[change]
    with pytest.raises(ceph_native.Refusal, match="^dashboard-drift$"):
        dashboard.native.dashboard_read()


def test_a_missing_verifier_refuses_and_never_skips_the_check(dashboard):
    dashboard.state.crypt = False
    with pytest.raises(ceph_native.Refusal, match="^dashboard-verify$"):
        dashboard.native.dashboard()
    assert dashboard.state.imports == 1 and dashboard.state.creates == 1
    assert "dashboard-applied.json" not in dashboard.markers.records


def test_a_verifier_without_bcrypt_refuses(dashboard, monkeypatch):
    monkeypatch.setattr(ceph_native.importlib, "import_module", lambda name: SimpleNamespace(crypt=lambda password, salt: None))
    with pytest.raises(ceph_native.Refusal, match="^dashboard-verify$"):
        dashboard.native.dashboard()
    assert "dashboard-applied.json" not in dashboard.markers.records


def test_a_missing_retained_file_while_the_user_exists_refuses_identity(dashboard):
    dashboard.native.dashboard()
    (dashboard.directory / "dashboard-material.json").unlink()
    with pytest.raises(ceph_native.Refusal, match="^dashboard-identity$"):
        dashboard.native.dashboard()
    with pytest.raises(ceph_native.Refusal, match="^dashboard-identity$"):
        dashboard.native.dashboard_read()
    assert dashboard.state.generated == 1 and dashboard.state.creates == 1


def test_cephs_own_policy_refusal_is_bounded_and_never_retried_with_a_new_password(dashboard):
    dashboard.state.create_code = 22
    with pytest.raises(ceph_native.Refusal, match="^dashboard-policy$") as refused:
        dashboard.native.dashboard()
    assert dashboard.state.creates == 1 and dashboard.state.generated == 1
    assert "PRIVATE" not in str(refused.value) and retained(dashboard) not in str(refused.value)
    dashboard.state.create_code = 1
    with pytest.raises(ceph_native.Refusal, match="^native-command$"):
        dashboard.native.dashboard()
    assert dashboard.state.creates == 2 and dashboard.state.generated == 1


def test_dashboard_read_is_read_only_and_offers_nothing_before_completion(dashboard):
    assert dashboard.native.dashboard_read() == {}
    assert mutations(dashboard) == [] and dashboard.markers.writes == []
    password = dashboard.native.dashboard()["private"]
    writes, effects = list(dashboard.markers.writes), len(mutations(dashboard))
    assert dashboard.native.dashboard_read() == {"private": password}
    assert dashboard.markers.writes == writes and len(mutations(dashboard)) == effects


def test_the_dashboard_read_phase_skips_the_release_gate_and_the_dashboard_phase_does_not(dashboard, monkeypatch):
    monkeypatch.setattr(ceph_native, "supported_transport", lambda request: False)
    with pytest.raises(ceph_native.Refusal, match="^unsupported-release$"):
        ceph_native.dispatch({"request": dashboard.request, "host": "node-1", "phase": "dashboard", "private": {}})
    monkeypatch.setattr(ceph_native, "Worker", lambda request, host: dashboard.native)
    assert ceph_native.dispatch({"request": dashboard.request, "host": "node-1", "phase": "dashboard_read", "private": {}}) == {}


def test_the_tunnel_mount_and_unit_observation_is_reproved_after_the_redeploy(dashboard):
    dashboard.request["clusterSSHTransport"] = "verified-tunnel"
    events = []
    real = dashboard.native.ceph

    def ceph(*arguments, **options):
        if arguments == ("orch", "redeploy", "mgr"):
            events.append("redeploy")
        return real(*arguments, **options)

    dashboard.native.ceph = ceph
    dashboard.native.transport_hold = contextlib.nullcontext
    dashboard.native.cache_hold = contextlib.nullcontext
    dashboard.native.verify_transport_bundle = lambda: events.append("bundle")
    dashboard.native.verify_tunnel_configuration = lambda: events.append("configuration")
    dashboard.native.daemons = lambda: events.append("units") or [{"name": "mgr.node-1.abcdef"}]
    dashboard.native.verify_manager_mounts = lambda daemons: events.append(("mounts", daemons[0]["name"]))
    dashboard.native.dashboard()
    assert events == ["redeploy", "bundle", "configuration", "units", ("mounts", "mgr.node-1.abcdef")]


def test_observation_projects_monitor_addresses_and_dashboard_access_without_secrets(dashboard):
    password = dashboard.native.dashboard()["private"]
    vector = [{"type": "v2", "addr": "192.0.2.2:3300", "nonce": 0}, {"type": "v1", "addr": "192.0.2.2:6789", "nonce": 0}]
    second = {"name": "node-2", "public_addrs": {"addrvec": vector}}
    first = {"name": "node-1", "public_addrs": {"addrvec": [{"type": "v2", "addr": "[2001:db8::1]:3300", "nonce": 0}]}}
    answers = {("mon", "dump"): {"mons": [second, first]}}
    real = dashboard.native.ceph
    dashboard.native.ceph = lambda *arguments, **options: copy.deepcopy(answers[arguments]) if arguments in answers else real(*arguments, **options)
    expected = dashboard.request["services"][0]["placement"]["hosts"]
    assert dashboard.native.monitor_addresses(expected) == [{"host": "node-1", "address": "2001:db8::1"}, {"host": "node-2", "address": "192.0.2.2"}]
    observed = dashboard.native.dashboard_observation()
    assert observed == {"url": URL, "certificateSHA256": hashlib.sha256(CERTIFICATE_DER).hexdigest(), "reachable": False}
    assert password not in json.dumps(observed)
    answers[("mon", "dump")]["mons"][0]["public_addrs"]["addrvec"][0]["addr"] = "not-an-address:3300"
    with pytest.raises(ceph_native.Refusal, match="^native-output$"):
        dashboard.native.monitor_addresses(expected)


def cluster_evidence(request, **dashboard):
    value = {"version": "ceph-core-evidence-v8", "request": "d" * 64, "action": "cluster", "owner": request["owner"], "fsid": request["fsid"],
             "owned": True, "exists": True, "ready": True, "removed": False, "partial": False, "health": "HEALTH_OK",
             "hosts": [h["name"] for h in request["hosts"]], "monitors": ["node-1"], "monAddrs": [{"host": "node-1", "address": "192.0.2.1"}],
             "mgrActive": "node-1", "mgrStandbys": ["node-2"], "osds": [], "resource": {"id": "", "path": ""}, "client": "",
             "dashboard": {"url": URL, "certificateSHA256": hashlib.sha256(CERTIFICATE_DER).hexdigest(), "reachable": False},
             "endpoint": {"certificateSHA256": "", "reachable": False}, "daemons": []}
    value["dashboard"].update(dashboard)
    return value


@pytest.mark.parametrize("field,value", [("url", "http://node-1.example.test:8443/"), ("url", "https://node-1.example.test:9443/"),
                                         ("certificateSHA256", "A" * 64), ("certificateSHA256", 7), ("reachable", 1), ("password", "x"),
                                         ("monAddrs", [{"host": "node-1", "address": "node-1"}]), ("monAddrs", [{"host": "foreign", "address": "192.0.2.9"}]),
                                         ("version", "ceph-core-evidence-v1")])
def test_evidence_v2_bounds_the_dashboard_and_monitor_projection(dashboard, field, value):
    good = cluster_evidence(dashboard.request)
    assert ceph_core.evidence(copy.deepcopy(good), "d" * 64, dashboard.request)["dashboard"]["url"] == URL
    if field in ("monAddrs", "version"):
        good[field] = value
    else:
        good["dashboard"][field] = value
    with pytest.raises(ceph_native.Refusal, match="^evidence$"):
        ceph_core.evidence(good, "d" * 64, dashboard.request)


def test_reachability_requires_a_url_a_certificate_and_a_matching_tls_peer(dashboard):
    connected = []

    def connect(address, port):
        connected.append((address, port))
        return CERTIFICATE_DER

    assert ceph_core.evidence(cluster_evidence(dashboard.request, reachable=True), "d" * 64, dashboard.request)
    with pytest.raises(ceph_native.Refusal, match="^evidence$"):
        ceph_core.evidence(cluster_evidence(dashboard.request, url="", reachable=True), "d" * 64, dashboard.request)
    assert ceph_core.dashboard_reachable(dashboard.request, cluster_evidence(dashboard.request), connect) is True
    assert connected == [("192.0.2.1", 8443)]
    assert ceph_core.dashboard_reachable(dashboard.request, cluster_evidence(dashboard.request, certificateSHA256="b" * 64), connect) is False
    assert ceph_core.dashboard_reachable(dashboard.request, cluster_evidence(dashboard.request, url=""), connect) is False
    assert len(connected) == 2

    def refused(address, port):
        connected.append((address, port))
        raise ConnectionRefusedError("port closed")

    assert ceph_core.dashboard_reachable(dashboard.request, cluster_evidence(dashboard.request), refused) is False
    assert len(connected) == 3


class PhaseTransport:
    def __init__(self, request, read):
        self.request, self.read, self.calls = request, read, []

    def call(self, host, phase, private=None, *, timeout=None, devices=None):
        del private, devices
        self.calls.append((host["name"], phase, timeout))
        if phase == "observe":
            return dict(cluster_evidence(self.request), request="")
        if phase == "host_state":
            return {"name": host["name"], "owned": True, "exists": True}
        assert phase == "dashboard_read"
        return copy.deepcopy(self.read)


def test_a_ready_observe_recaptures_the_password_and_proves_reachability(dashboard, tmp_path):
    password = "Xq7-Lm2_Rv9.Tk4-Wz8_Hn3.Pb6-Jd5_"
    transport = PhaseTransport(dashboard.request, {"private": password})
    output = tmp_path / "dashboard"
    result = ceph_core.execute(dashboard.request, "observe", "d" * 64, transport, {"dashboard": str(output)}, connect=lambda address, port: CERTIFICATE_DER)
    assert result["evidence"]["ready"] and result["evidence"]["dashboard"]["reachable"] is True
    assert output.read_bytes() == password.encode() and stat.S_IMODE(os.stat(output).st_mode) == 0o600
    assert password not in json.dumps(result) and [phase for unused, phase, unused in transport.calls].count("dashboard_read") == 1


def test_an_unreachable_dashboard_port_is_unproved_access_not_a_failure(dashboard, tmp_path):
    attempts = []

    def unreachable(address, port):
        attempts.append((address, port))
        raise TimeoutError("timed out")

    transport = PhaseTransport(dashboard.request, {"private": "Xq7-Lm2_Rv9.Tk4-Wz8_Hn3.Pb6-Jd5_"})
    result = ceph_core.execute(dashboard.request, "observe", "d" * 64, transport, {"dashboard": str(tmp_path / "dashboard")}, connect=unreachable)
    assert attempts == [("192.0.2.1", 8443)]
    assert result["evidence"]["ready"] and result["evidence"]["dashboard"]["reachable"] is False


def test_an_unfinished_dashboard_phase_observes_partial_work_and_writes_nothing(dashboard, tmp_path):
    transport = PhaseTransport(dashboard.request, {})
    output = tmp_path / "dashboard"

    def never(address, port):
        pytest.fail("probe " + address + ":" + str(port))

    result = ceph_core.execute(dashboard.request, "observe", "d" * 64, transport, {"dashboard": str(output)}, connect=never)
    assert result["evidence"]["partial"] and not result["evidence"]["ready"] and not output.exists()


@pytest.mark.parametrize("outputs", [{}, {"client": "/x"}, {"dashboard": "/x", "client": "/y"}])
def test_the_cluster_action_names_exactly_its_dashboard_output(dashboard, outputs):
    with pytest.raises(ceph_native.Refusal, match="^(material|request)$"):
        ceph_core.execute(dashboard.request, "observe", "d" * 64, PhaseTransport(dashboard.request, {}), outputs)


@pytest.mark.parametrize("value", ["", "short", "Xq7-Lm2_Rv9.Tk4-Wz8_Hn3.Pb6-Jd5!", 7])
def test_the_output_writer_refuses_anything_but_a_policy_password(tmp_path, value):
    with pytest.raises(ceph_native.Refusal, match="^material$"):
        ceph_core.dashboard_output(str(tmp_path / "dashboard"), value)
    assert not (tmp_path / "dashboard").exists()


def test_the_manager_restarts_the_phase_causes_are_waited_out_within_its_budget(dashboard):
    run = dashboard.native.run
    after = {"enable": [], "redeploy": []}

    def restarting(argv, data=b"", timeout=60):
        args = argv[argv.index("--keyring") + 2:]
        args = args[2:] if args[:2] == ["--format", "json"] else args
        if args == ["dashboard", "create-self-signed-cert"] and len(after["enable"]) < 1:
            # Enabling the module respawned the manager: its first command hangs until the client gives up.
            after["enable"].append(args)
            raise ceph_native.Refusal("native-timeout")
        if args == ["orch", "ps", "--daemon_type", "mgr", "--refresh"] and dashboard.state.scheduled and len(after["redeploy"]) < 2:
            # The redeploy restarts the manager that serves the orchestrator.
            after["redeploy"].append(args)
            return 1, b"Error ENOENT: Module not found"
        return run(argv, data=data, timeout=timeout)

    dashboard.native.run = restarting
    result = dashboard.native.dashboard()
    assert result["changed"] is True and dashboard.state.redeploys == 1 and dashboard.state.creates == 1
    assert len(after["enable"]) == 1 and len(after["redeploy"]) == 2
    assert dashboard.clock.value < ceph_native.DASHBOARD_BUDGET


MANAGER_ONE = "node-1.abcdef"
MANAGER_TWO = "node-2.ghijkl"


def repair_attempt(daemon):
    return "dashboard-port-" + hashlib.sha256(daemon.encode()).hexdigest() + ".json"


def test_certificate_presence_does_not_schedule_before_dashboard_publication(dashboard):
    dashboard.state.keys = [ceph_native.DASHBOARD_CERTIFICATE]
    dashboard.state.publication_delay = 3
    dashboard.native.dashboard()
    calls = [args[2:] if args[:2] == ["--format", "json"] else args for args in commands(dashboard)]
    scheduled = calls.index(["orch", "redeploy", "mgr"])
    assert calls[:scheduled].count(["mgr", "services"]) >= 4
    assert dashboard.clock.pauses == [ceph_native.DASHBOARD_INTERVAL] * 3
    assert dashboard.state.redeploys == 1


def test_unpublished_dashboard_never_schedules_and_waits_only_its_existing_budget(dashboard):
    dashboard.state.published = False
    with pytest.raises(ceph_native.Refusal, match="^native-timeout$"):
        dashboard.native.dashboard()
    assert dashboard.state.redeploys == 0 and dashboard.state.repairs == []
    assert "dashboard-redeployed.json" not in dashboard.markers.records
    assert dashboard.clock.value >= ceph_native.DASHBOARD_BUDGET


def test_endpoint_loss_during_initial_failover_repairs_only_the_missing_manager(dashboard):
    effect = dashboard.native.tunnel_effect
    pause = dashboard.native.pause

    def schedule(*args, **options):
        result = effect(*args, **options)
        if args == ("orch", "redeploy", "mgr"):
            dashboard.state.published = False
            dashboard.state.ports[MANAGER_ONE] = [8765, 8765]
        return result

    def publish(seconds):
        pause(seconds)
        if dashboard.state.scheduled:
            dashboard.state.published = True

    dashboard.native.tunnel_effect = schedule
    dashboard.native.pause = publish
    dashboard.native.dashboard()
    assert dashboard.state.redeploys == 1 and dashboard.state.repairs == [MANAGER_ONE]
    assert dashboard.state.ports[MANAGER_ONE] == [8443, 8765]
    assert "dashboard-redeployed.json" in dashboard.markers.records


def test_completed_legacy_redeploy_requires_ports_on_every_manager_before_acceptance(dashboard):
    password = dashboard.native.dashboard()["private"]
    original_done = copy.deepcopy(dashboard.markers.records["dashboard-redeployed.json"])
    dashboard.state.ports = {MANAGER_ONE: [8765], MANAGER_TWO: [8765, 8765]}
    assert dashboard.native.dashboard() == {"changed": True, "private": password}
    assert dashboard.state.redeploys == 1 and dashboard.state.repairs == [MANAGER_ONE, MANAGER_TWO]
    assert dashboard.markers.records["dashboard-redeployed.json"] == original_done
    assert dashboard.state.creates == dashboard.state.generated == 1
    before = len(mutations(dashboard))
    assert dashboard.native.dashboard() == {"changed": False, "private": password}
    assert len(mutations(dashboard)) == before


def test_a_replacement_without_the_dashboard_port_cannot_spend_a_second_repair_pass(dashboard):
    dashboard.state.ports[MANAGER_ONE] = [8765]
    effect = dashboard.native.ceph

    def still_missing(*args, **options):
        result = effect(*args, **options)
        if args[:3] == ("orch", "daemon", "redeploy"):
            dashboard.state.ports[MANAGER_ONE] = [8765]
        return result

    dashboard.native.ceph = still_missing
    with pytest.raises(ceph_native.Refusal, match="^dashboard-manager$"):
        dashboard.native.dashboard()
    assert dashboard.state.repairs == [MANAGER_ONE]
    assert "dashboard-redeployed.json" not in dashboard.markers.records
    with pytest.raises(ceph_native.Refusal, match="^dashboard-manager$"):
        dashboard.native.dashboard()
    assert dashboard.state.repairs == [MANAGER_ONE]


@pytest.mark.parametrize("after_effect", [False, True])
def test_lost_repair_response_observes_before_mutation_and_resubmits_once_per_apply(dashboard, after_effect):
    dashboard.state.ports = {MANAGER_ONE: [8765], MANAGER_TWO: [8765]}
    if after_effect:
        effect = dashboard.native.ceph

        def lose(*args, **options):
            result = effect(*args, **options)
            if args == ("orch", "daemon", "redeploy", "mgr." + MANAGER_ONE):
                raise ceph_native.Refusal("native-timeout")
            return result
        dashboard.native.ceph = lose
    else:
        def interrupted(name, unused_value):
            if name == repair_attempt(MANAGER_ONE):
                raise ceph_native.Refusal("native-timeout")
        dashboard.markers.after = interrupted
    with pytest.raises(ceph_native.Refusal, match="^native-timeout$"):
        dashboard.native.dashboard()
    dashboard.markers.after = lambda *args: None
    dashboard.clock.value = 0
    if after_effect:
        dashboard.native.ceph = effect
        dashboard.native.dashboard()
        assert dashboard.state.repairs == [MANAGER_ONE, MANAGER_TWO]
    else:
        assert repair_attempt(MANAGER_ONE) in dashboard.markers.records and dashboard.state.repairs == []
        dashboard.native.dashboard()
        # The standby's intent left unchanged by the earlier apply is requested once more before the active MGR fails over.
        assert dashboard.state.repairs == [MANAGER_ONE, MANAGER_TWO]
        assert dashboard.state.events.index(("redeployed", MANAGER_ONE)) < dashboard.state.events.index(("fail", MANAGER_TWO))
    assert "dashboard-redeployed.json" in dashboard.markers.records


@pytest.mark.parametrize("hold", ["transport_hold", "cache_hold"])
def test_a_hold_refusal_on_a_repair_leaves_no_intent_and_the_next_apply_submits_it(dashboard, hold):
    dashboard.request["clusterSSHTransport"] = "verified-tunnel"
    dashboard.state.ports[MANAGER_ONE] = [8765]
    refused = []

    @contextlib.contextmanager
    def refusing():
        if dashboard.state.redeploys and not refused:
            refused.append(hold)
            raise ceph_native.Refusal("native-tunnel-unproved")
        yield

    dashboard.native.transport_hold = contextlib.nullcontext
    dashboard.native.cache_hold = contextlib.nullcontext
    setattr(dashboard.native, hold, refusing)
    dashboard.native.verify_transport_bundle = lambda: None
    dashboard.native.verify_tunnel_configuration = lambda: None
    dashboard.native.daemons = lambda: [{"name": "mgr." + MANAGER_ONE}]
    dashboard.native.verify_manager_mounts = lambda daemons: None
    with pytest.raises(ceph_native.Refusal, match="^native-tunnel-unproved$"):
        dashboard.native.dashboard()
    assert refused == [hold] and dashboard.state.redeploys == 1
    assert repair_attempt(MANAGER_ONE) not in dashboard.markers.records and dashboard.state.repairs == []
    dashboard.native.dashboard()
    assert dashboard.state.repairs == [MANAGER_ONE] and "dashboard-redeployed.json" in dashboard.markers.records


def test_an_acknowledged_repair_that_never_replaces_the_manager_is_submitted_once_per_apply(dashboard):
    dashboard.state.ports[MANAGER_ONE] = [8765]
    dashboard.state.ignored = {MANAGER_ONE}
    with pytest.raises(ceph_native.Refusal, match="^native-timeout$"):
        dashboard.native.dashboard()
    assert dashboard.state.ignored_calls == [MANAGER_ONE]
    dashboard.clock.value = 0
    with pytest.raises(ceph_native.Refusal, match="^native-timeout$"):
        dashboard.native.dashboard()
    assert dashboard.state.ignored_calls == [MANAGER_ONE, MANAGER_ONE]
    assert "dashboard-redeployed.json" not in dashboard.markers.records


def test_repairing_the_active_manager_fails_over_and_waits_for_publication_first(dashboard):
    dashboard.state.active = MANAGER_ONE
    dashboard.state.ports = {MANAGER_ONE: [8765], MANAGER_TWO: [8765]}
    dashboard.state.deferred = 2
    dashboard.native.dashboard()
    events = dashboard.state.events
    failed = events.index(("fail", MANAGER_ONE))
    repaired = events.index(("repair", MANAGER_ONE))
    assert events.index(("repair", MANAGER_TWO)) < events.index(("redeployed", MANAGER_TWO)) < failed < repaired
    assert repaired < events.index(("redeployed", MANAGER_ONE))
    assert ("orch-status", False) in events[failed:repaired] and ("orch-status", True) in events[failed:repaired]
    assert ("services", False) in events[failed:repaired] and ("services", True) in events[failed:repaired]
    assert events.count(("fail", MANAGER_ONE)) == 1 and dashboard.state.active == MANAGER_TWO
    assert dashboard.state.repairs == [MANAGER_TWO, MANAGER_ONE]
    assert dashboard.state.ports == {MANAGER_ONE: [8443, 8765], MANAGER_TWO: [8443, 8765]}
    assert "dashboard-redeployed.json" in dashboard.markers.records


def test_a_single_declared_manager_missing_the_port_refuses_without_failing_it_over(dashboard):
    dashboard.request["services"][1]["placement"]["count"] = 1
    dashboard.state.managers = 1
    dashboard.state.active = MANAGER_ONE
    dashboard.state.ports = {MANAGER_ONE: [8765]}
    with pytest.raises(ceph_native.Refusal, match="^dashboard-manager$"):
        dashboard.native.dashboard()
    assert dashboard.state.redeploys == 1 and "dashboard-port-repair.json" in dashboard.markers.records
    assert not any(event[0] == "fail" for event in dashboard.state.events)
    assert not any(args[:2] == ["mgr", "fail"] for args in mutations(dashboard))
    assert repair_attempt(MANAGER_ONE) not in dashboard.markers.records and dashboard.state.repairs == []


def test_interruption_after_plan_before_any_intent_allows_only_frozen_targets(dashboard):
    dashboard.state.ports[MANAGER_ONE] = [8765]

    def interrupted(name, unused_value):
        if name == "dashboard-port-repair.json":
            raise ceph_native.Refusal("native-timeout")
    dashboard.markers.after = interrupted
    with pytest.raises(ceph_native.Refusal, match="^native-timeout$"):
        dashboard.native.dashboard()
    assert dashboard.state.repairs == []
    dashboard.markers.after = lambda *args: None
    dashboard.native.dashboard()
    assert dashboard.state.repairs == [MANAGER_ONE]


def test_malformed_peer_attempt_blocks_all_repair_effects(dashboard):
    dashboard.state.ports = {MANAGER_ONE: [8765], MANAGER_TWO: [8765]}

    def corrupt_peer(name, unused_value):
        if name == "dashboard-port-repair.json":
            dashboard.markers.records[repair_attempt(MANAGER_TWO)] = {"version": "foreign"}
    dashboard.markers.after = corrupt_peer
    with pytest.raises(ceph_native.Refusal, match="^dashboard-manager$"):
        dashboard.native.dashboard()
    assert dashboard.state.repairs == []


@pytest.mark.parametrize("drift", ["scope", "daemon", "container", "new-missing-peer"])
def test_repair_plan_scope_and_exact_targets_refuse_drift_before_effects(dashboard, drift):
    dashboard.state.ports[MANAGER_ONE] = [8765]
    read = dashboard.native.manager_containers

    def changed_snapshot():
        rows = read()
        if "dashboard-port-repair.json" in dashboard.markers.records:
            if drift == "daemon":
                rows["node-1.zzzzzz"] = rows.pop(MANAGER_ONE)
            elif drift == "container":
                rows[MANAGER_ONE] = ("e5" * 6, True, False)
            elif drift == "new-missing-peer":
                rows[MANAGER_TWO] = (rows[MANAGER_TWO][0], True, False)
        return rows

    def foreign_scope(name, value):
        if name == "dashboard-port-repair.json" and drift == "scope":
            dashboard.markers.records[name]["scope"] = dict(value["scope"], owner="foreign-operation")
            raise ceph_native.Refusal("native-timeout")
    dashboard.native.manager_containers = changed_snapshot
    dashboard.markers.after = foreign_scope
    if drift == "scope":
        with pytest.raises(ceph_native.Refusal, match="^native-timeout$"):
            dashboard.native.dashboard()
    with pytest.raises(ceph_native.Refusal, match="^dashboard-manager$"):
        dashboard.native.dashboard()
    assert dashboard.state.repairs == []


@pytest.mark.parametrize("drift", ["scheduled", "plan", "completion"])
def test_manager_drift_from_the_recorded_redeploy_refuses_as_manager_drift(dashboard, drift):
    if drift == "scheduled":
        def replaced(name, unused_value):
            if name == "dashboard-redeploy.json":
                dashboard.state.containers = ["e5" * 6, "f6" * 6]
        dashboard.markers.after = replaced
    else:
        dashboard.native.dashboard()
        name = "dashboard-redeploy.json" if drift == "plan" else "dashboard-redeployed.json"
        dashboard.markers.records[name]["containers"] = ["e5" * 6, "f6" * 6, "a1" * 6][:2 if drift == "completion" else 3]
    with pytest.raises(ceph_native.Refusal, match="^dashboard-manager$"):
        dashboard.native.dashboard()


@pytest.mark.parametrize("ports", [[True], ["8443"], [-1], [65536], None])
def test_manager_port_metadata_requires_bounded_exact_integer_ports(dashboard, ports):
    dashboard.state.ports[MANAGER_ONE] = ports
    with pytest.raises(ceph_native.Refusal, match="^native-output$"):
        dashboard.native.manager_containers()
    assert dashboard.state.redeploys == 0


def test_duplicate_manager_containers_refuse_before_initial_attempt(dashboard):
    dashboard.state.containers = ["a1" * 6, "a1" * 6]
    with pytest.raises(ceph_native.Refusal, match="^foreign-service$"):
        dashboard.native.dashboard()
    assert "dashboard-redeploy.json" not in dashboard.markers.records
    assert dashboard.state.redeploys == 0


def test_publication_lost_after_first_url_requires_a_new_snapshot_before_scheduling(dashboard):
    published = dashboard.native.dashboard_published
    calls = []

    def disappear():
        calls.append(True)
        if len(calls) == 2:
            dashboard.state.publication_delay = 2
        return published()

    dashboard.native.dashboard_published = disappear
    dashboard.native.dashboard()
    native_calls = [args[2:] if args[:2] == ["--format", "json"] else args for args in commands(dashboard)]
    schedule = native_calls.index(["orch", "redeploy", "mgr"])
    assert native_calls[:schedule].count(["orch", "ps", "--daemon_type", "mgr", "--refresh"]) == 3
    assert dashboard.state.redeploys == 1


def test_legacy_retry_waits_for_publication_before_scheduling_again(dashboard):
    effect = dashboard.native.tunnel_effect
    dashboard.native.tunnel_effect = lambda *args, **options: ceph_native.require(False, "native-timeout")
    with pytest.raises(ceph_native.Refusal, match="^native-timeout$"):
        dashboard.native.dashboard()
    record = copy.deepcopy(dashboard.markers.records["dashboard-redeploy.json"])
    dashboard.native.tunnel_effect = effect
    dashboard.state.published = False
    with pytest.raises(ceph_native.Refusal, match="^native-timeout$"):
        dashboard.native.dashboard()
    assert dashboard.state.redeploys == 0
    assert dashboard.markers.records["dashboard-redeploy.json"] == record


def test_incomplete_declared_manager_set_cannot_create_initial_attempt(dashboard):
    dashboard.request["services"][1]["placement"]["count"] = 3
    with pytest.raises(ceph_native.Refusal, match="^not-ready$"):
        dashboard.native.dashboard()
    assert "dashboard-redeploy.json" not in dashboard.markers.records
    assert dashboard.state.redeploys == 0


def test_the_active_manager_is_not_failed_over_while_no_standby_is_registered(dashboard):
    dashboard.state.active = MANAGER_ONE
    dashboard.state.ports = {MANAGER_ONE: [8765]}
    dashboard.state.standbys = False
    with pytest.raises(ceph_native.Refusal, match="^native-timeout$"):
        dashboard.native.dashboard()
    assert not any(event[0] == "fail" for event in dashboard.state.events)
    assert not any(args[:2] == ["mgr", "fail"] for args in mutations(dashboard))


def test_the_active_manager_is_failed_over_at_most_once_per_apply(dashboard):
    dashboard.state.active = MANAGER_ONE
    dashboard.state.ports = {MANAGER_ONE: [8765]}
    dashboard.state.promotes = False
    with pytest.raises(ceph_native.Refusal, match="^native-timeout$"):
        dashboard.native.dashboard()
    assert dashboard.state.events.count(("fail", MANAGER_ONE)) == 1
