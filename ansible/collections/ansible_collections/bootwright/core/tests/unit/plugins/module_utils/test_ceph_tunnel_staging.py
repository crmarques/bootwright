"""Candidate staging plans refuse identity drift before producing native argv."""

from __future__ import annotations

import copy
import json
import re

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native, ceph_tunnel_staging as staging
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_native_specializations as specs

CURRENT = "20.2.1-405.el9cp"


@pytest.fixture
def stage_request():
    return {"version": "ceph-core-v14", "clusterSSHTransport": "verified-tunnel", "owner": "apply-example",
            "fsid": "4c0a4300-aa43-458c-86d7-ac2256d1fc00", "image": "registry.example.test/ceph@sha256:" + "a" * 64,
            "cephadm": {"path": "/usr/sbin/cephadm", "packageVersion": "20.2.1-405.el9cp"},
            "bootstrap": "node-1.example.test", "monIP": "192.0.2.1", "clusterNetworks": [], "callHome": "enabled",
            "hosts": [{"name": "node-1.example.test", "machine": "machine-1", "address": "192.0.2.1", "port": 22},
                      {"name": "node-2.example.test", "machine": "machine-2", "address": "192.0.2.2", "port": 22}]}


def metadata(kind):
    return json.dumps({"service_name": kind, "ports": [9283, 8765] if kind == "mgr" else [],
                       "memory_request": None, "memory_limit": None}).encode()


def daemon_list(stage_request):
    intent = staging.stage_intent(stage_request)
    return [{"fsid": stage_request["fsid"], "style": "cephadm:v1", "container_image_name": stage_request["image"],
             "enabled": True, "state": "running", "name": kind + "." + intent[kind + "ID"]} for kind in ("mon", "mgr")]


def plan(stage_request):
    return staging.initial_redeploy_plan(stage_request, staging.stage_intent(stage_request), daemon_list(stage_request), [], metadata("mon"), metadata("mgr"))


def scratch(stage_request):
    return staging.ROOT + "/" + stage_request["fsid"] + "/.bootstrap-fixture"


def chosen(build=CURRENT):
    """The seams a host cephadm built from a signed build's declared names answers."""
    return specs.selection(specs.synthesized(build))


def todays_argv(stage_request):
    intent = staging.stage_intent(stage_request)
    return ["/usr/sbin/cephadm", "--image", stage_request["image"], "bootstrap", "--fsid", stage_request["fsid"],
            "--mon-id", "node-1", "--mgr-id", intent["mgrID"], "--mon-ip", "192.0.2.1",
            "--output-dir", staging.ROOT + "/" + stage_request["fsid"], "--skip-ssh", "--skip-monitoring-stack", "--skip-dashboard",
            "--skip-admin-label", "--allow-fqdn-hostname", "--no-cleanup-on-failure",
            "--config", scratch(stage_request) + "/ceph.conf", "--automatically-accept-license"]


@pytest.mark.parametrize("build", sorted(build for build, current in specs.BUILDS.items() if current))
def test_every_twenty_two_build_keeps_todays_bootstrap_argv(stage_request, build):
    selection = chosen(build)
    assert staging.bootstrap_argv(stage_request, scratch(stage_request), selection) == todays_argv(stage_request)
    stage_request["clusterNetworks"] = ["198.51.100.0/24"]
    assert staging.bootstrap_argv(stage_request, scratch(stage_request), selection) == todays_argv(stage_request) + ["--cluster-network", "198.51.100.0/24"]
    assert selection.applied == set()


def test_a_twenty_one_host_cephadm_bootstraps_without_the_license_flag(stage_request):
    selection = chosen("20.1.0-221.el9cp")
    argv = staging.bootstrap_argv(stage_request, scratch(stage_request), selection)
    assert argv == todays_argv(stage_request)[:-1] and "--skip-prepare-host" not in argv
    assert selection.applied == {"ceph-bootstrap-without-license-flag"}


@pytest.mark.parametrize("shape", sorted(specs.UNREADABLE))
def test_an_unanchored_host_cephadm_keeps_the_general_license_flag(stage_request, shape):
    selection = specs.selection(specs.UNREADABLE[shape]())
    assert staging.bootstrap_argv(stage_request, scratch(stage_request), selection) == todays_argv(stage_request)
    assert selection.applied == set()


def test_bootstrap_names_are_stable_and_match_provider_format(stage_request):
    original = staging.stage_intent(stage_request)
    stage_request["hosts"].reverse()
    assert staging.stage_intent(stage_request) == original
    assert original["monID"] == "node-1"
    assert re.fullmatch(r"node-1\.[a-z]{6}", original["mgrID"])
    stage_request["hosts"][0]["address"] = "192.0.2.3"
    assert staging.stage_intent(stage_request) != original


def test_bootstrap_has_no_native_ssh_or_host_enrollment_path(stage_request):
    argv = staging.bootstrap_argv(stage_request, scratch(stage_request), chosen())
    assert "--skip-ssh" in argv and "--no-cleanup-on-failure" in argv
    assert "--skip-prepare-host" not in argv
    assert "--mon-id" in argv and "--mgr-id" in argv
    assert not {"--ssh-user", "--ssh-config", "--ssh-private-key", "--ssh-public-key", "--apply-spec", "--single-host-defaults",
                "--registry-json", "--registry-url", "--registry-username", "--registry-password"} & set(argv)
    assert not any("registryJSON" in argument for argument in argv)
    assert "--skip-admin-label" in argv and "--skip-monitoring-stack" in argv and "--skip-dashboard" in argv
    stage_request["clusterNetworks"] = ["198.51.100.0/24"]
    assert staging.bootstrap_argv(stage_request, scratch(stage_request), chosen())[-2:] == ["--cluster-network", "198.51.100.0/24"]


@pytest.mark.parametrize("field,value", [("version", "ceph-core-v2"), ("clusterSSHTransport", "native"),
                                         ("owner", "../owner"), ("fsid", "../../foreign"), ("bootstrap", "foreign"),
                                         ("monIP", "192.0.2.99"), ("image", "registry.example.test/ceph:latest")])
def test_invalid_request_never_produces_staging_argv(stage_request, field, value):
    stage_request[field] = value
    with pytest.raises(staging.StageRefusal, match="^native-tunnel-stage-unproved$"):
        staging.bootstrap_argv(stage_request, scratch(stage_request), chosen())


@pytest.mark.parametrize("mutate", [lambda hosts: hosts.append(copy.deepcopy(hosts[0])),
                                    lambda hosts: hosts[1].update(name="node-1.other.test"),
                                    lambda hosts: hosts[1].update(machine="machine-1"),
                                    lambda hosts: hosts[1].update(address="192.0.2.1"),
                                    lambda hosts: hosts[1].update(address="not-an-IP"),
                                    lambda hosts: hosts[1].update(port=True)])
def test_ambiguous_bindings_refuse_before_native_effect(stage_request, mutate):
    mutate(stage_request["hosts"])
    with pytest.raises(staging.StageRefusal):
        staging.stage_intent(stage_request)


@pytest.mark.parametrize("path", ["/tmp/.bootstrap-fixture", "/var/lib/bootwright/ceph/foreign/.bootstrap-fixture",
                                  "/var/lib/bootwright/ceph/{fsid}/.bootstrap-fixture/..",
                                  "/var/lib/bootwright/ceph/{fsid}/../.bootstrap-fixture",
                                  "/var/lib/bootwright/ceph/{fsid}/arbitrary"])
def test_private_material_paths_cannot_be_redirected(stage_request, path):
    with pytest.raises(staging.StageRefusal):
        staging.bootstrap_argv(stage_request, path.format(fsid=stage_request["fsid"]), chosen())


@pytest.mark.parametrize("value", [["198.51.100.1/24"], ["198.51.100.0/24\n--ssh-user"], [False], "198.51.100.0/24"])
def test_cluster_networks_are_closed_literals(stage_request, value):
    stage_request["clusterNetworks"] = value
    with pytest.raises(staging.StageRefusal):
        staging.bootstrap_argv(stage_request, scratch(stage_request), chosen())


def test_paths_derive_from_frozen_identity_without_operator_path_input(stage_request):
    intent = staging.stage_intent(stage_request)
    files = staging.initial_files(stage_request)
    assert files["mgr"]["keyring"] == "/var/lib/ceph/" + stage_request["fsid"] + "/mgr." + intent["mgrID"] + "/keyring"
    assert files["mon"]["unit.meta"] == "/var/lib/ceph/" + stage_request["fsid"] + "/mon.node-1/unit.meta"


def test_redeploy_preserves_existing_metadata_ports_and_readonly_mount(stage_request):
    saved = plan(stage_request)
    argv = staging.redeploy_argv(stage_request, saved, scratch(stage_request))
    assert argv[0] == "/usr/sbin/cephadm" and "deploy" in argv and "--reconfig" not in argv
    assert argv[argv.index("--name") + 1] == "mgr." + saved["intent"]["mgrID"]
    assert json.loads(argv[argv.index("--meta-json") + 1]) == json.loads(metadata("mgr"))
    assert argv[argv.index("--tcp-ports") + 1] == "9283 8765"
    assert argv[-2:] == ["--extra-container-args=-v",
                         "--extra-container-args=" + staging.ROOT + "/" + stage_request["fsid"] + "/transport:" + staging.CONTAINER_ROOT + ":ro,z"]
    assert argv[argv.index("--config") + 1] == scratch(stage_request) + "/mgr.config"
    assert argv[argv.index("--keyring") + 1] == scratch(stage_request) + "/mgr.keyring"


@pytest.mark.parametrize("field,value", [("owner", "foreign-owner"), ("fsid", "7cd96a8d-ec71-4c0e-aa09-243a1585a353"),
                                         ("mgrID", "node-1.aaaaaa"), ("monID", "foreign")])
def test_stage_journal_drift_cannot_adopt_existing_daemons(stage_request, field, value):
    intent = staging.stage_intent(stage_request)
    intent[field] = value
    with pytest.raises(staging.StageRefusal):
        staging.initial_redeploy_plan(stage_request, intent, daemon_list(stage_request), [], metadata("mon"), metadata("mgr"))


@pytest.mark.parametrize("field,value", [("fsid", "foreign"), ("name", "mgr.foreign.aaaaaa"),
                                         ("container_image_name", "registry.example.test/ceph:latest"),
                                         ("style", "legacy"), ("enabled", 1), ("state", "stopped")])
def test_native_daemon_drift_refuses_before_redeploy(stage_request, field, value):
    daemons = daemon_list(stage_request)
    daemons[1][field] = value
    with pytest.raises(staging.StageRefusal):
        staging.initial_redeploy_plan(stage_request, staging.stage_intent(stage_request), daemons, [], metadata("mon"), metadata("mgr"))


@pytest.mark.parametrize("change", [lambda rows: rows.pop(), lambda rows: rows.append(copy.deepcopy(rows[0])),
                                    lambda rows: rows.__setitem__(1, copy.deepcopy(rows[0]))])
def test_missing_or_extra_native_daemons_are_ambiguous(stage_request, change):
    daemons = daemon_list(stage_request)
    change(daemons)
    with pytest.raises(staging.StageRefusal):
        staging.initial_redeploy_plan(stage_request, staging.stage_intent(stage_request), daemons, [], metadata("mon"), metadata("mgr"))


@pytest.mark.parametrize("field,value", [("service_name", "rgw"), ("ports", [9283, 8443]), ("ports", [9283, 9283]),
                                         ("ports", [True, 8765]), ("ports", "9283 8765"), ("ports", [-1, 8765]),
                                         ("memory_limit", 1), ("private_key", "confidential-fixture")])
def test_metadata_is_bounded_public_and_matches_minimal_daemon(stage_request, field, value):
    content = json.loads(metadata("mgr"))
    content[field] = value
    with pytest.raises(staging.StageRefusal) as failure:
        staging.initial_redeploy_plan(stage_request, staging.stage_intent(stage_request), daemon_list(stage_request), [], metadata("mon"),
                                      json.dumps(content).encode())
    assert str(failure.value) == "native-tunnel-stage-unproved"
    assert "confidential-fixture" not in str(failure.value)


@pytest.mark.parametrize("raw", [b'{"service_name":"mgr","service_name":"mon"}', b"not json", b"\xff", b" " * 4097])
def test_duplicate_or_malformed_metadata_never_enters_argv(raw):
    with pytest.raises(staging.StageRefusal):
        staging.parse_metadata(raw, "mgr")


def test_reordered_native_ports_are_preserved(stage_request):
    changed = json.loads(metadata("mgr"))
    changed["ports"].reverse()
    saved = staging.initial_redeploy_plan(stage_request, staging.stage_intent(stage_request), daemon_list(stage_request), [], metadata("mon"),
                                          json.dumps(changed).encode())
    argv = staging.redeploy_argv(stage_request, saved, scratch(stage_request))
    assert argv[argv.index("--tcp-ports") + 1] == "8765 9283"


def test_saved_plan_cannot_be_reused_for_different_owned_cluster(stage_request):
    saved = plan(stage_request)
    stage_request["owner"] = "new-owner"
    with pytest.raises(staging.StageRefusal):
        staging.redeploy_argv(stage_request, saved, scratch(stage_request))


@pytest.mark.parametrize("inventory", [[{"hostname": "node-1.example.test"}], None, {}, ()])
def test_no_initial_redeploy_after_any_host_enrollment(stage_request, inventory):
    with pytest.raises(staging.StageRefusal):
        staging.initial_redeploy_plan(stage_request, staging.stage_intent(stage_request), daemon_list(stage_request), inventory,
                                      metadata("mon"), metadata("mgr"))


@pytest.mark.parametrize("field,value", [("path", "/tmp/cephadm"), ("version", "20.2.0")])
def test_staging_does_not_extend_to_uninspected_provider_tools(stage_request, field, value):
    stage_request["cephadm"][field] = value
    with pytest.raises(staging.StageRefusal):
        staging.bootstrap_argv(stage_request, scratch(stage_request), chosen())


@pytest.mark.parametrize("build", sorted(specs.BUILDS))
def test_the_bootstrap_argv_leaves_the_call_home_path_to_the_worker(stage_request, build):
    enabled = staging.bootstrap_argv(stage_request, scratch(stage_request), chosen(build))
    stage_request["callHome"] = "disabled"
    assert staging.bootstrap_argv(stage_request, scratch(stage_request), chosen(build)) == enabled


def test_sshd_include_tree_is_closed_without_effects():
    raw = b'# Match User ignored-comment\nInclude "/etc/ssh/sshd_config.d/*.conf"\nUsePAM yes\n'
    assert staging.sshd_includes(raw) == ["/etc/ssh/sshd_config.d/*.conf"]
    assert staging.sshd_includes(b"Include=/etc/crypto-policies/back-ends/opensshserver.config\n") == ["/etc/crypto-policies/back-ends/opensshserver.config"]
    assert staging.sshd_includes(b"Include = /etc/ssh/sshd_config.d/50-redhat.conf\n") == ["/etc/ssh/sshd_config.d/50-redhat.conf"]


@pytest.mark.parametrize("raw", [b"Match Host forged.example.test\nAllowTcpForwarding yes\n", b"mAtCh User operator\n",
                                 b"Match=LocalAddress 192.0.2.1\n", b'"Match" User operator\n',
                                 b"Match LocalPort 22\n", b"Match Address 127.0.0.1\n"])
def test_unproved_conditional_server_policy_cannot_pass_partial_context_probe(raw):
    with pytest.raises(staging.StageRefusal):
        staging.sshd_includes(raw)


def test_owned_policy_only_accepts_exact_bytes_under_caller_owned_filename_proof():
    raw = b"Match User operator\n  AllowTcpForwarding local\nMatch all\n"
    assert staging.sshd_includes(raw, owned_policy=raw) == []
    with pytest.raises(staging.StageRefusal):
        staging.sshd_includes(raw + b"AllowTcpForwarding yes\n", owned_policy=raw)


@pytest.mark.parametrize("pattern", ["relative.conf", "/tmp/server.conf", "/etc/ssh/*.conf", "/etc/ssh/sshd_config.d/../server.conf",
                                     "/etc/ssh/sshd_config.d/*/server.conf", "/etc/ssh/sshd_config.d/$VARIABLE.conf",
                                     "/etc/ssh/sshd_config.d/%h.conf", "~/sshd_config", "/etc/ssh/sshd_config.d/file name.conf"])
def test_include_targets_cannot_expand_trusted_file_or_effect_scope(pattern):
    with pytest.raises(staging.StageRefusal):
        staging.sshd_includes(('Include "' + pattern + '"\n').encode())


@pytest.mark.parametrize("raw", [b"", b"Include\n", b"Include \"unterminated\n", b"UsePAM yes\x00\n", b"# \xff\n", b"#" * (256 * 1024 + 1)])
def test_malformed_server_configuration_refuses_before_policy_publication(raw):
    with pytest.raises(staging.StageRefusal):
        staging.sshd_includes(raw)


@pytest.mark.parametrize("state,container,image", [("stopped", "", ""), ("unknown", "", "request"), ("error", "", ""), ("unknown", "b" * 64, "request")])
def test_seed_between_containers_is_observed_while_an_action_refuses(stage_request, state, container, image):
    daemons = daemon_list(stage_request)
    daemons[1].update(state=state, enabled=False, container_id=container, container_image_name=stage_request["image"] if image else "")
    intent = staging.stage_intent(stage_request)
    assert staging.prove_seed_daemons(stage_request, daemons, observing=True) == intent
    staging.initial_redeploy_plan(stage_request, intent, daemons, [], metadata("mon"), metadata("mgr"), observing=True)
    with pytest.raises(staging.StageRefusal):
        staging.prove_seed_daemons(stage_request, daemons)
    with pytest.raises(staging.StageRefusal):
        staging.initial_redeploy_plan(stage_request, intent, daemons, [], metadata("mon"), metadata("mgr"))


@pytest.mark.parametrize("field,value", [("fsid", "foreign"), ("name", "mgr.foreign.aaaaaa"), ("style", "legacy"),
                                         ("container_image_name", "registry.example.test/ceph:latest"), ("container_id+no-image", "b" * 64)])
def test_observed_seed_contradiction_still_refuses(stage_request, field, value):
    daemons = daemon_list(stage_request)
    daemons[1].update(state="stopped", container_id="", container_image_name="")
    if field == "container_id+no-image":
        daemons[1]["container_id"] = value
    else:
        daemons[1][field] = value
    with pytest.raises(staging.StageRefusal):
        staging.prove_seed_daemons(stage_request, daemons, observing=True)
    with pytest.raises(staging.StageRefusal):
        staging.initial_redeploy_plan(stage_request, staging.stage_intent(stage_request), daemons, [], metadata("mon"), metadata("mgr"), observing=True)


def test_staging_accepts_authored_package_coordinate_without_catalog(stage_request):
    stage_request["cephadm"]["packageVersion"] = "synthetic+native-coordinate"
    assert "--skip-prepare-host" not in staging.bootstrap_argv(stage_request, scratch(stage_request), chosen())


def test_staging_admits_the_same_package_coordinate_bound_as_the_cluster_request(stage_request):
    stage_request["cephadm"]["packageVersion"] = "a" * 1024
    staging.stage_intent(stage_request)
    stage_request["cephadm"]["packageVersion"] = "a" * 1025
    with pytest.raises(staging.StageRefusal, match="^native-tunnel-stage-unproved$"):
        staging.stage_intent(stage_request)


def license_call(request, image=None, command=" orch accept-license --image "):
    """cephadm's CephContainer run of the bootstrap's cli() helper, as build_run_cmd joins it (20.2.2-349 container_types.py:160-166)."""
    image = image or request["image"]
    return ("/usr/bin/podman run --rm --ipc=host --stop-signal=SIGTERM --net=host --entrypoint /usr/bin/ceph --init -e CONTAINER_IMAGE=" + image +
            " -e NODE_NAME=node-1 -v /var/log/ceph/" + request["fsid"] + ":/var/log/ceph:z " + image + command + image)


def license_call_output(request, *, code=1, inner=22, refusal="Error EINVAL: no hosts defined", call=None, logged=None, raised="RuntimeError: ",
                        pair=True, between=(), stdout_tail=(), stderr_tail=(), garbled=False):
    """The bootstrap's private output when the MGR refuses the IBM license call on the zero-host seed (signed 20.2.x zipapps): call()
    logs the inner exit and the ceph CLI's stderr at INFO on stdout (call_wrappers.py:280-292, logging.py:118-160), and call_throws'
    uncaught RuntimeError ends stderr with a traceback while cephadm exits 1 (call_wrappers.py:303-310, __main__.py main())."""
    call = call or license_call(request)
    stdout = ["Verifying podman|docker is present...", "Non-zero exit code 1 from /usr/bin/podman run --rm " + request["image"] + " mgr stat",
              "/usr/bin/ceph: stderr Error ENOENT: synthetic"]
    if pair:
        stdout += ["Non-zero exit code %d from %s" % (inner, logged or call)] + list(between) + ["/usr/bin/ceph: stderr " + refusal]
    stdout += ["", "\t***************", "\tCephadm hit an issue during cluster installation. Current cluster files will NOT BE DELETED automatically."]
    stdout += list(stdout_tail)
    frame = b'  File "/usr/sbin/cephadm/__main__.py", line 3535, in command_bootstrap' + (b"\xff" if garbled else b"")
    stderr = [b"Traceback (most recent call last):", frame, (raised + "Failed command: " + call + ": " + refusal).encode(), b""]
    stderr += [line.encode() for line in stderr_tail]
    return code, ceph_native.NativeOutput(("\n".join(stdout) + "\n").encode(), b"\n".join(stderr) + b"\n")


def test_the_ibm_license_call_refusal_is_recognized_exactly(stage_request):
    assert staging.license_call_refusal(stage_request, *license_call_output(stage_request)) is True


OTHER_IMAGE = "registry.example.test/ceph@sha256:" + "b" * 64
UNRECOGNIZED = {
    "exit-0": {"code": 0}, "exit-2": {"code": 2}, "exit-22": {"code": 22}, "signal": {"code": -9},
    "inner-exit-1": {"inner": 1},
    "another-errno": {"refusal": "Error ENOENT: no hosts defined"}, "another-text": {"refusal": "Error EINVAL: no host defined"},
    "another-image": {"other-image": True}, "another-command": {"command": " orch host ls"},
    "logged-call-differs": {"logged-node": True},
    "later-stderr-failure": {"stderr_tail": ("ERROR: later failure",)},
    "prefixed-raise": {"raised": "WARNING: RuntimeError: "},
    "later-exit-pair": {"stdout_tail": ("Non-zero exit code 1 from /x", "/usr/bin/ceph: stderr y")},
    "split-pair": {"between": ("/usr/bin/ceph: stdout synthetic",)},
    "no-pair": {"pair": False},
    "undecodable-stderr": {"garbled": True},
    "relative-command": {"relative": True},
}


@pytest.mark.parametrize("case", sorted(UNRECOGNIZED))
def test_any_other_bootstrap_failure_is_not_the_license_call_refusal(stage_request, case):
    changes = dict(UNRECOGNIZED[case])
    if changes.pop("other-image", False):
        changes["call"] = license_call(stage_request, OTHER_IMAGE)
    if "command" in changes:
        changes["call"] = license_call(stage_request, command=changes.pop("command"))
    if changes.pop("logged-node", False):
        changes["logged"] = license_call(stage_request).replace("NODE_NAME=node-1", "NODE_NAME=node-2")
    if changes.pop("relative", False):
        changes["call"] = license_call(stage_request)[len("/usr/bin/"):]
    assert staging.license_call_refusal(stage_request, *license_call_output(stage_request, **changes)) is False


def test_output_without_its_private_stderr_is_not_the_license_call_refusal(stage_request):
    code, output = license_call_output(stage_request)
    assert staging.license_call_refusal(stage_request, code, bytes(output)) is False
    assert staging.license_call_refusal(stage_request, code, bytes(output) + output.stderr) is False
    assert staging.license_call_refusal(stage_request, code, ceph_native.NativeOutput(bytes(output), output.stderr.decode())) is False
