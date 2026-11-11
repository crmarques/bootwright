"""The release copy runs the release's own oc in a private run directory it removes, verifying the mirror, and publishes what it read back.

The role's defaults are evaluated here as Ansible does, against a request of
the frozen shape: the copy's arguments name the frozen layout and never
--insecure, its environment points at the run directory alone, the lent auth
document reaches only the task that writes it there, the directory is removed
in an always, and the refusals it names are exactly those the protocol admits.
"""
from __future__ import annotations

import pathlib

from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar, trust_as_template
from ansible_collections.bootwright.core.plugins.action import infrastructureservices_mirror_release_protocol as protocol

ROLE = pathlib.Path(__file__).resolve().parents[2] / "roles" / "infrastructureservices_mirror_release"
DIGEST = "sha256:" + "0123456789abcdef" * 4
REQUEST = {
    "client": {"checksums": "https://mirror.openshift.com/pub/openshift-v4/clients/ocp/4.21.15/sha256sum.txt",
               "file": "openshift-client-linux-4.21.15.tar.gz",
               "url": "https://mirror.openshift.com/pub/openshift-v4/clients/ocp/4.21.15/openshift-client-linux-4.21.15.tar.gz"},
    "egress": {"httpsProxy": "http://proxy.example.test:3128", "noProxy": [".example.test"]},
    "identity": {"cluster": "sno", "context": "lab", "environment": "lab", "registry": "mirror-registry"},
    "mirror": {"credentialsRef": "mirror-registry-credentials", "credentialsType": "usernamePassword", "host": "203.0.113.20:5000",
               "trustBundleRef": "mirror-ca"},
    "placement": {"connection": "ssh", "machine": "mirror"},
    "pullSecretRef": "openshift-pull-secret",
    "release": {"components": "quay.io/openshift-release-dev/ocp-v4.0-art-dev", "repository": "quay.io/openshift-release-dev/ocp-release",
                "tag": "4.21.15-x86_64", "version": "4.21.15"},
    "version": "mirror-release-v1",
}
LOADER = DataLoader()
SCRATCH = "/var/lib/bootwright-staging/runs/bootwright-scratch-1-1/mirror-release"
LENT = "/run/bootwright-release-abc"


def tasks(name):
    return LOADER.load_from_file(str(ROLE / "tasks" / (name + ".yml")), trusted_as_template=True)


def walk(entries):
    for task in entries:
        yield task
        for section in ("block", "always"):
            yield from walk(task.get(section) or [])


def templar(request, **extra):
    variables = dict(LOADER.load_from_file(str(ROLE / "defaults" / "main.yml"), trusted_as_template=True))
    variables.update(bootwright_mirror_release_request=request, bootwright_mirror_release_digest="0" * 64,
                     infrastructureservices_mirror_release_run={"path": SCRATCH},
                     infrastructureservices_mirror_release_prepared={"lent": LENT}, **extra)
    return Templar(loader=LOADER, variables=variables)


def rendered(values, text):
    return values.template(trust_as_template(text))


def copy_task():
    return next(task for task in walk(tasks("apply")) if "ansible.builtin.command" in task)


def test_the_copy_reads_the_pinned_digest_or_else_the_versions_tag_and_pushes_the_frozen_layout():
    argv = copy_task()["ansible.builtin.command"]["argv"]
    tagged = [rendered(templar(REQUEST), value) for value in argv]
    assert tagged == [
        SCRATCH + "/bin/oc", "adm", "release", "mirror", "-a", LENT + "/auth.json",
        "--from=quay.io/openshift-release-dev/ocp-release:4.21.15-x86_64",
        "--to=203.0.113.20:5000/quay.io/openshift-release-dev/ocp-v4.0-art-dev",
        "--to-release-image=203.0.113.20:5000/quay.io/openshift-release-dev/ocp-release:4.21.15-x86_64",
        "--insecure=false",
    ]
    pinned = templar(dict(REQUEST, release=dict(REQUEST["release"], digest=DIGEST)))
    assert rendered(pinned, argv[6]) == "--from=quay.io/openshift-release-dev/ocp-release@" + DIGEST
    assert not any("insecure=true" in value or value == "--insecure" for value in tagged)
    assert copy_task()["ansible.builtin.command"]["expand_argument_vars"] is False


def test_the_copy_reads_no_file_of_the_hosts_own_and_trusts_the_anchor_beside_the_system_roots():
    environment = rendered(templar(REQUEST), copy_task()["environment"])
    for name in ("HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_RUNTIME_DIR", "DOCKER_CONFIG"):
        assert environment[name].startswith(SCRATCH + "/"), name
    assert environment["REGISTRY_AUTH_FILE"] == LENT + "/auth.json", "the auth document lives on tmpfs alone"
    assert environment["SSL_CERT_DIR"].split(":") == [LENT + "/trust", "/etc/pki/tls/certs", "/etc/ssl/certs"]
    assert environment["HTTPS_PROXY"] == environment["https_proxy"] == "http://proxy.example.test:3128"
    assert environment["NO_PROXY"].split(",") == [".example.test", "203.0.113.20"]
    bracketed = rendered(templar(dict(REQUEST, mirror=dict(REQUEST["mirror"], host="[2001:db8::20]:5000"))), copy_task()["environment"])
    assert bracketed["no_proxy"].split(",") == [".example.test", "2001:db8::20"]


def test_the_run_directory_is_emptied_and_removed_in_an_always_whatever_the_copy_did():
    block = next(task for task in tasks("apply") if "block" in task)
    assert block["block"][0]["bootwright.core.lifecycle_scratch"] == {"root": "{{ bootwright_run_scratch }}", "scope": "mirror-release"}
    emptied, removed = block["always"]
    cleaned = {"mode": "clean", "scratch": "{{ infrastructureservices_mirror_release_scratch }}", "lent": "{{ infrastructureservices_mirror_release_lent }}"}
    assert emptied["bootwright.core.infrastructureservices_mirror_release"] == cleaned
    assert removed["bootwright.core.lifecycle_scratch"]["state"] == "absent"
    assert emptied["when"] == removed["when"] == "infrastructureservices_mirror_release_run is succeeded"
    assert copy_task() in block["block"]


def test_the_auth_document_reaches_only_the_task_that_writes_it_into_the_run_directory():
    lent = [task for name in ("apply", "observe", "destroy") for task in walk(tasks(name)) if "block" not in task and "releaseAuth" in str(task)]
    assert len(lent) == 1 and lent[0].get("no_log") is True
    assert lent[0]["bootwright.core.infrastructureservices_mirror_release"]["mode"] == "prepare"
    for task in walk(tasks("destroy")):
        assert "bootwright.core.infrastructureservices_mirror_release" not in task, "a removal reads nothing"
        assert "imagePassword" not in str(task)
    for name in ("apply", "observe"):
        for task in walk(tasks(name)):
            arguments = task.get("bootwright.core.infrastructureservices_mirror_release")
            if arguments is not None and arguments["mode"] != "clean":
                assert task.get("no_log") is True


def test_every_refusal_the_role_names_is_one_the_protocol_admits():
    reasons = []
    for task in walk(tasks("apply")):
        arguments = task.get("bootwright.core.infrastructureservices_mirror_release_protocol", {})
        if arguments.get("phase") == "refused":
            reasons.append(arguments["reason"])
    values = templar(REQUEST)
    named = set()
    for reason in reasons:
        for result in ({}, {"reason": "release-digest-mismatch"}, {"reason": "client-digest-mismatch"}):
            values.available_variables = dict(values.available_variables, infrastructureservices_mirror_release_found=result,
                                              infrastructureservices_mirror_release_prepared=result, infrastructureservices_mirror_release_verified=result)
            named.add(rendered(values, reason))
    assert named <= set(protocol.REFUSALS)
    assert {"mirror-unreadable", "client-unreachable", "release-unreachable", "release-incomplete", "release-digest-mismatch"} <= named


def test_the_completion_proves_a_release_held_whole_at_its_pin_and_publishes_what_was_read_back():
    release = dict(REQUEST["release"], digest=DIGEST)
    held = {"digest": DIGEST, "components": [{"name": "cli", "digest": DIGEST}], "missing": 0}
    evidence, refused = protocol.completion({"release": release, "held": held, "digest": "0" * 64})
    assert refused is None and evidence["postcondition"] is True and evidence["version"] == "mirror-release-evidence-v1"
    for partial in (dict(held, missing=1), dict(held, components=[]), dict(held, digest="sha256:" + "f" * 64)):
        evidence, refused = protocol.completion({"release": release, "held": partial, "digest": "0" * 64})
        assert evidence["postcondition"] is False and refused
    evidence, refused = protocol.completion({"release": release, "held": {}, "digest": "0" * 64, "removed": True})
    assert refused is None and evidence["absent"] is True and evidence["postcondition"] is True
    values = templar(REQUEST, infrastructureservices_mirror_release_found={"digest": DIGEST, "components": [], "missing": 0},
                     infrastructureservices_mirror_release_verified={"skipped": True})
    assert rendered(values, "{{ infrastructureservices_mirror_release_held.digest }}") == DIGEST
    values = templar(REQUEST, infrastructureservices_mirror_release_found={"digest": "", "components": [], "missing": 0},
                     infrastructureservices_mirror_release_verified=held)
    assert rendered(values, "{{ infrastructureservices_mirror_release_held.components | length }}") in (1, "1")


def test_every_entry_point_admits_a_request_of_the_frozen_shape_and_refuses_another_release():
    for name in ("apply", "observe", "destroy"):
        conditions = tasks(name)[0]["ansible.builtin.assert"]["that"]
        for request in (REQUEST, dict(REQUEST, release=dict(REQUEST["release"], digest=DIGEST)),
                        dict(REQUEST, client=dict(REQUEST["client"], url="https://downloads.example.test/ocp/4.21.15/openshift-client-linux-4.21.15.tar.gz"))):
            values = templar(request)
            assert all(rendered(values, "{{ %s }}" % condition) is True for condition in conditions), name
        for changed in (dict(REQUEST, release=dict(REQUEST["release"], repository="quay.io/example/release")),
                        dict(REQUEST, release=dict(REQUEST["release"], tag="latest")),
                        dict(REQUEST, client=dict(REQUEST["client"], checksums="https://example.test/sha256sum.txt")),
                        dict(REQUEST, client=dict(REQUEST["client"], url="http://mirror.openshift.com/openshift-client-linux-4.21.15.tar.gz")),
                        dict(REQUEST, mirror=dict(REQUEST["mirror"], host="203.0.113.20"))):
            values = templar(changed)
            assert not all(rendered(values, "{{ %s }}" % condition) is True for condition in conditions), name


def test_a_damaged_release_found_first_is_copied_over_while_an_unreadable_mirror_or_another_pin_refuses():
    for found, unreadable, whole in (
        ({"failed": True, "reason": "release-incomplete"}, False, False),
        ({"failed": True, "reason": "mirror-unreadable"}, True, False),
        ({"failed": True, "reason": "release-digest-mismatch"}, True, False),
        ({"failed": True, "msg": "module failure"}, True, False),
        ({"digest": DIGEST, "components": [{"name": "cli", "digest": DIGEST}], "missing": 0}, False, True),
        ({"digest": DIGEST, "components": [{"name": "cli", "digest": DIGEST}], "missing": 2}, False, False),
        ({"digest": "", "components": [], "missing": 0}, False, False),
    ):
        values = templar(REQUEST, infrastructureservices_mirror_release_found=found)
        assert rendered(values, "{{ infrastructureservices_mirror_release_unreadable | bool }}") is unreadable, found
        assert rendered(values, "{{ infrastructureservices_mirror_release_whole | bool }}") is whole, found
    block = next(task for task in tasks("apply") if "block" in task)
    assert block["when"] == "not (infrastructureservices_mirror_release_whole | bool)"
