"""The image mirror fill proves skopeo first, lends its credential only to the copy, and publishes what it verified.

The role's defaults are evaluated here as Ansible does, against a request of
the frozen shape; the refusals it names are exactly those the protocol admits,
and its removal contacts nothing, because the Registry's own removal deletes
what was pushed.
"""
from __future__ import annotations

import pathlib

from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar, trust_as_template
from ansible_collections.bootwright.core.plugins.action import infrastructureservices_mirror_images_protocol as protocol

ROLE = pathlib.Path(__file__).resolve().parents[2] / "roles" / "infrastructureservices_mirror_images"
DIGEST = "sha256:" + "0123456789abcdef" * 4
REQUEST = {
    "egress": {"noProxy": []},
    "identity": {"context": "lab", "environment": "lab", "registry": "mirror-registry"},
    "images": [
        {"digest": DIGEST, "repository": "docker.io/library/registry", "source": "docker.io/library/registry@" + DIGEST},
        {"digest": DIGEST, "repository": "quay.io/metal3-io/sushy-tools", "source": "quay.io/metal3-io/sushy-tools@" + DIGEST},
    ],
    "mirror": {"credentialsRef": "mirror-registry-credentials", "credentialsType": "usernamePassword", "host": "203.0.113.20:5000",
               "trustBundleRef": "mirror-ca"},
    "placement": {"connection": "ssh", "machine": "mirror"},
    "version": "mirror-images-v1",
}
LOADER = DataLoader()


def tasks(name):
    return LOADER.load_from_file(str(ROLE / "tasks" / (name + ".yml")), trusted_as_template=True)


def templar(request):
    variables = dict(LOADER.load_from_file(str(ROLE / "defaults" / "main.yml"), trusted_as_template=True))
    variables.update(bootwright_mirror_images_request=request, bootwright_mirror_images_digest="0" * 64)
    return Templar(loader=LOADER, variables=variables)


def value(values, name):
    return values.template(trust_as_template("{{ infrastructureservices_mirror_images_%s }}" % name))


def test_a_request_of_the_frozen_shape_is_valid_in_every_field():
    values = templar(REQUEST)
    assert len(value(values, "valid_repositories")) == len(value(values, "valid_digests")) == 2
    assert value(values, "paired_sources") == value(values, "sources")


def test_an_image_by_tag_or_with_a_foreign_source_is_not_valid():
    images = [dict(REQUEST["images"][0], digest="latest"), dict(REQUEST["images"][1], source="quay.io/other@" + DIGEST)]
    values = templar(dict(REQUEST, images=images))
    assert value(values, "valid_digests") == [DIGEST]
    assert value(values, "paired_sources") != value(values, "sources")


def test_skopeo_is_proved_before_any_copy_and_its_absence_names_its_package():
    apply = tasks("apply")
    names = [task["name"] for task in apply]
    proof = next(index for index, task in enumerate(apply) if "ansible.builtin.command" in task)
    copy = next(index for index, task in enumerate(apply) if "bootwright.core.infrastructureservices_mirror_image" in task)
    assert apply[proof]["ansible.builtin.command"]["argv"] == ["/usr/bin/skopeo", "--version"] and proof < copy, names
    refused = apply[proof + 1]["bootwright.core.infrastructureservices_mirror_images_protocol"]
    assert refused["reason"] == "skopeo-missing" and refused["reason"] in protocol.REFUSALS
    assert "install the skopeo package" in apply[proof + 2]["ansible.builtin.fail"]["msg"]


def test_the_credential_reaches_only_the_copy_and_observation_and_never_a_log():
    for name in ("apply", "observe"):
        for task in tasks(name):
            arguments = task.get("bootwright.core.infrastructureservices_mirror_image")
            if arguments is None:
                continue
            assert task.get("no_log") is True
            assert "material.imagePassword" in arguments["password"] and "material.imageTrust" in arguments["trust"]
    for task in tasks("destroy"):
        assert "bootwright.core.infrastructureservices_mirror_image" not in task, "a removal reads nothing"
        assert "imagePassword" not in str(task)


def test_the_refusal_after_a_failed_copy_is_one_the_protocol_admits():
    apply = tasks("apply")
    named = next(task for task in apply if task.get("bootwright.core.infrastructureservices_mirror_images_protocol", {}).get("phase") == "refused"
                 and "filled" in task["bootwright.core.infrastructureservices_mirror_images_protocol"]["reason"])
    values = templar(REQUEST)
    for reason in ("image-digest-mismatch", "image-unreachable", "mirror-unreadable"):
        values.available_variables = dict(values.available_variables, infrastructureservices_mirror_images_filled={"reason": reason})
        assert values.template(named["bootwright.core.infrastructureservices_mirror_images_protocol"]["reason"]) == reason
        assert reason in protocol.REFUSALS
    values.available_variables = dict(values.available_variables, infrastructureservices_mirror_images_filled={"msg": "module failure"})
    assert values.template(named["bootwright.core.infrastructureservices_mirror_images_protocol"]["reason"]) == "image-unreachable"


def test_the_completion_proves_presence_only_for_every_requested_image_in_order():
    images = REQUEST["images"]
    held = [{"repository": image["repository"], "digest": image["digest"], "verified": True} for image in images]
    evidence, refused = protocol.completion({"images": images, "held": held, "digest": "0" * 64})
    assert refused is None and evidence["postcondition"] is True and evidence["version"] == "mirror-images-evidence-v1"
    evidence, refused = protocol.completion({"images": images, "held": held[:1], "digest": "0" * 64})
    assert evidence["postcondition"] is False and refused
    evidence, refused = protocol.completion({"images": images, "held": [], "digest": "0" * 64, "removed": True})
    assert refused is None and evidence["images"] == [] and evidence["postcondition"] is True


def test_every_entry_point_admits_a_request_of_the_frozen_shape_and_refuses_a_host_without_a_port():
    for name in ("apply", "observe", "destroy"):
        conditions = tasks(name)[0]["ansible.builtin.assert"]["that"]
        values = templar(REQUEST)
        assert all(values.template(trust_as_template("{{ %s }}" % condition)) is True for condition in conditions), name
        refused = templar(dict(REQUEST, mirror=dict(REQUEST["mirror"], host="203.0.113.20")))
        assert not all(refused.template(trust_as_template("{{ %s }}" % condition)) is True for condition in conditions), name
    bracketed = templar(dict(REQUEST, mirror=dict(REQUEST["mirror"], host="[2001:db8::20]:5000")))
    conditions = tasks("apply")[0]["ansible.builtin.assert"]["that"]
    assert all(bracketed.template(trust_as_template("{{ %s }}" % condition)) is True for condition in conditions)
