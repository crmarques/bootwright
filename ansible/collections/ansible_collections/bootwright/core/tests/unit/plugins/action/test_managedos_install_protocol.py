"""A managed-OS installation names its target's refused pre-boot proof to its runner.

The apply's rescue names the refusal the substrate's proof left (roles/
managedos_install_anaconda/tasks/apply.yml), and the runner reports the
diagnostic Go gave that reason for the Machine (internal/substrate, preboot.go)
instead of the adapter's failure. A reason Go gave none would break the
runner's protocol, so the action publishes only the ones the proof names.
"""

from __future__ import annotations

from types import SimpleNamespace

import pytest

from ansible_collections.bootwright.core.plugins.action import managedos_install_protocol as protocol


def run(args, monkeypatch):
    published = []
    monkeypatch.setattr(protocol, "emit", lambda message, **kwargs: published.append(message))
    module = protocol.ActionModule.__new__(protocol.ActionModule)
    module._task = SimpleNamespace(args=args)
    return module.run(task_vars={}), published


@pytest.mark.parametrize("reason", ["hardware-mismatch", "identity-mismatch", "machine-running"])
def test_each_pre_boot_refusal_is_named_to_the_runner(reason, monkeypatch):
    result, published = run({"phase": "refused", "reason": reason}, monkeypatch)
    assert result == {"changed": False}
    assert published == [{"phase": "refused", "reason": reason}]


@pytest.mark.parametrize("reason", [None, "", "release-stamp", "machine-running-node-0", "machine-running ", "media-changed-tree"],
                         ids=repr)
def test_a_refusal_the_installation_does_not_name_is_never_published(reason, monkeypatch):
    args = {"phase": "refused"}
    if reason is not None:
        args["reason"] = reason
    result, published = run(args, monkeypatch)
    assert result == {"failed": True, "msg": "the installation capability result could not be published"}
    assert not published


def test_prepared_publishes_only_the_marker(monkeypatch):
    acknowledged = []
    monkeypatch.setattr(protocol, "emit", lambda message, acknowledge=False: acknowledged.append((message, acknowledge)))
    module = protocol.ActionModule.__new__(protocol.ActionModule)
    module._task = SimpleNamespace(args={"phase": "prepared", "marker": " the-marker\n", "registration": {"state": "owned"},
                                         "digest": "1" * 64})
    assert module.run(task_vars={}) == {"changed": False}
    assert acknowledged == [({"phase": "prepared", "preparation": {"marker": "the-marker"}}, True)]
    for marker in (None, "", "x" * (protocol.MAX_MARKER + 1)):
        result, published = run({"phase": "prepared", "marker": marker}, monkeypatch)
        assert result == {"failed": True, "msg": "the installation capability result could not be published"}
        assert not published


# A boot image the attempt finds changed since the plan froze it is named with
# the diagnostic Go gave it (selection.go). The tree image is the shared
# package-tree publication's to name, so the installation never names it.
def test_a_changed_boot_image_is_named_to_the_runner(monkeypatch):
    result, published = run({"phase": "refused", "reason": "media-changed-boot"}, monkeypatch)
    assert result == {"changed": False}
    assert published == [{"phase": "refused", "reason": "media-changed-boot"}]


@pytest.mark.parametrize("reason", ["ssh-policy-unavailable", "ssh-policy-host-excluded", "ssh-policy-fleet-excluded",
                                    "ssh-policy-generated-excluded", "fips-not-enabled", "root-encryption-unproved",
                                    "registration-input", "registration-foreign", "registration-refused",
                                    "registration-identity-collision", "registration-unavailable", "registration-repositories",
                                    "registration-unreachable"])
def test_early_key_policy_refusals_use_the_go_owned_public_diagnostic(reason, monkeypatch):
    result, published = run({"phase": "refused", "reason": reason}, monkeypatch)
    assert result == {"changed": False}
    assert published == [{"phase": "refused", "reason": reason}]


DIGEST = "1" * 64
IDENTITY = "fedcba9876543210" * 4


def test_presence_carries_the_tree_identity_the_inspection_read():
    for observed, carried in ((IDENTITY, IDENTITY), ("", ""), (None, "")):
        evidence = protocol.presence({"power": "On", "observation": {"tree": True, "treeIdentity": observed}}, DIGEST)
        assert evidence["treeIdentity"] == carried


@pytest.mark.parametrize("identity", ["abc", IDENTITY.upper(), IDENTITY + "0", 7], ids=repr)
def test_a_malformed_tree_identity_is_never_published(identity):
    with pytest.raises(ValueError, match="tree identity"):
        protocol.presence({"power": "On", "observation": {"treeIdentity": identity}}, DIGEST)


def test_a_removal_reports_no_tree_identity():
    evidence = protocol.absence({"observation": {"treeIdentity": IDENTITY}}, DIGEST)
    assert evidence["treeIdentity"] == ""


def proved(**extra):
    """A completion's arguments that prove every other postcondition."""
    arguments = {"digest": DIGEST, "power": "On", "marker": "{}", "hostKey": "ssh-ed25519 AAAA", "address": "192.0.2.10",
                 "reachable": True, "network": True, "networkReboot": True, "observation": {"image": True}}
    arguments.update(extra)
    return arguments


# A FIPS installation completes only when the installed system proved FIPS
# mode; any other installation is unaffected by what it reports.
def test_a_fips_installation_completes_only_with_the_fips_proof():
    evidence, refused = protocol.completion(proved(fipsRequired=True, fips=False))
    assert evidence["fips"] is False and evidence["postcondition"] is False
    assert refused is not None and refused.endswith("not proved: fips")
    evidence, refused = protocol.completion(proved(fipsRequired="True", fips="True"))
    assert evidence["fips"] is True and evidence["postcondition"] is True and refused is None
    for arguments in (proved(), proved(fips=False), proved(fipsRequired=False)):
        evidence, refused = protocol.completion(arguments)
        assert evidence["fips"] is False and evidence["postcondition"] is True and refused is None
    assert protocol.absence({"observation": {}}, DIGEST)["fips"] is False


# An encrypting installation completes only when the installed system proved
# its root filesystem on a TPM2-bound LUKS device; any other installation is
# unaffected by what it reports.
def test_an_encrypting_installation_completes_only_with_the_root_encryption_proof():
    evidence, refused = protocol.completion(proved(rootEncryptionRequired=True, rootEncryption=False))
    assert evidence["rootEncryption"] is False and evidence["postcondition"] is False
    assert refused is not None and refused.endswith("not proved: rootEncryption")
    evidence, refused = protocol.completion(proved(rootEncryptionRequired="True", rootEncryption="True"))
    assert evidence["rootEncryption"] is True and evidence["postcondition"] is True and refused is None
    for arguments in (proved(), proved(rootEncryption=False), proved(rootEncryptionRequired=False)):
        evidence, refused = protocol.completion(arguments)
        assert evidence["rootEncryption"] is False and evidence["postcondition"] is True and refused is None
    assert protocol.absence({"observation": {}}, DIGEST)["rootEncryption"] is False


CONSUMER = "0f1e2d3c-4b5a-6978-8796-a5b4c3d2e1f0"


# A registering installation completes only when the worker read a registration
# this installation owns; any other installation is unaffected by what it reads.
def test_a_registering_installation_completes_only_with_an_owned_registration():
    for read in ({"consumer": "", "state": "absent"}, {"consumer": CONSUMER, "state": "pending"},
                 {"consumer": "", "state": "foreign"}, {}, None):
        evidence, refused = protocol.completion(proved(registrationRequired=True, registration=read))
        assert evidence["postcondition"] is False
        assert refused is not None and refused.endswith("not proved: registration")
    evidence, refused = protocol.completion(proved(registrationRequired="True", registration={"consumer": CONSUMER, "state": "owned"}))
    assert evidence["registration"] == {"consumer": CONSUMER, "state": "owned"}
    assert evidence["postcondition"] is True and refused is None
    for arguments in (proved(), proved(registration={"consumer": "", "state": "absent"}), proved(registrationRequired=False)):
        evidence, refused = protocol.completion(arguments)
        assert evidence["postcondition"] is True and refused is None
    assert protocol.completion(proved())[0]["registration"] == {"consumer": "", "state": ""}
    assert protocol.absence({"observation": {}}, DIGEST)["registration"] == {"consumer": "", "state": ""}


@pytest.mark.parametrize("read", [
    {"consumer": CONSUMER, "state": "registered"},
    {"consumer": "", "state": "owned"},
    {"consumer": "", "state": "pending"},
    {"consumer": CONSUMER.upper(), "state": "owned"},
    {"consumer": CONSUMER + "0", "state": "owned"},
    {"consumer": CONSUMER, "state": "absent"},
    {"consumer": CONSUMER, "state": "foreign"},
    {"consumer": CONSUMER, "state": "owned", "changed": True},
    {"state": "absent"},
    {"consumer": 7, "state": "owned"},
    "owned",
], ids=repr)
def test_a_malformed_registration_is_never_published(read, monkeypatch):
    with pytest.raises(ValueError, match="registration"):
        protocol.presence(proved(registration=read), DIGEST)
    result, published = run(dict(proved(registration=read), phase="completed", outcome="changed"), monkeypatch)
    assert result.get("failed") is True and not published


# A registering removal publishes its absence only when the machine holds no
# registration its installation created; any other removal is unaffected.
def test_a_registering_removal_publishes_only_a_released_registration(monkeypatch):
    for read in ({"consumer": CONSUMER, "state": "unregistered"}, {"consumer": "", "state": "absent"},
                 {"consumer": "", "state": "retained"}):
        evidence, refused = protocol.completion({"digest": DIGEST, "removed": True, "observation": {},
                                                 "registrationRequired": "True", "registration": read})
        assert evidence["registration"] == read and evidence["postcondition"] is True and refused is None
        result, published = run({"phase": "completed", "outcome": "changed", "digest": DIGEST, "removed": True,
                                 "observation": {}, "registrationRequired": True, "registration": read}, monkeypatch)
        assert result == {"changed": False} and published[0]["evidence"]["registration"] == read
    for read in ({"consumer": CONSUMER, "state": "owned"}, {"consumer": CONSUMER, "state": "pending"}, {}, None):
        evidence, refused = protocol.completion({"digest": DIGEST, "removed": True, "observation": {},
                                                 "registrationRequired": True, "registration": read})
        assert evidence["postcondition"] is False
        assert refused is not None and refused.endswith("still present: registration")
    evidence, refused = protocol.completion({"digest": DIGEST, "removed": True, "observation": {"image": True},
                                             "registrationRequired": True, "registration": {}})
    assert refused is not None and refused.endswith("still present: image, registration")
    for arguments in ({}, {"registrationRequired": False, "registration": {"consumer": CONSUMER, "state": "owned"}}):
        evidence, refused = protocol.completion(dict(arguments, digest=DIGEST, removed=True, observation={}))
        assert evidence["postcondition"] is True and refused is None
    for read in ({"consumer": "", "state": "unregistered"}, {"consumer": CONSUMER, "state": "retained"}):
        with pytest.raises(ValueError, match="registration"):
            protocol.registration(read)


KERNEL = "5.14.0-611.5.1.el9_7.x86_64"
COMPLETE = {"kernel": KERNEL, "packages": 212, "rebooted": True, "release": "9.8", "state": "complete", "transaction": 3}
NONE = {"kernel": "", "packages": 0, "rebooted": False, "release": "", "state": "none", "transaction": 0}


# An installation updating within its release completes only with a complete
# update; one that updates nothing records state none without reading the guest.
def test_an_updating_installation_completes_only_with_a_complete_update():
    for read in (dict(COMPLETE, state="upgraded", rebooted=False), dict(COMPLETE, state="pending", rebooted=False, transaction=0),
                 dict(NONE, state="absent"), dict(NONE, state="foreign"), {}, None):
        evidence, refused = protocol.completion(proved(updateRequired=True, update=read))
        assert evidence["postcondition"] is False
        assert refused is not None and refused.endswith("not proved: update")
    evidence, refused = protocol.completion(proved(updateRequired="True", update=COMPLETE))
    assert evidence["update"] == COMPLETE and evidence["postcondition"] is True and refused is None
    unchanged = dict(COMPLETE, packages=0, rebooted=False, transaction=0)
    assert protocol.completion(proved(updateRequired=True, update=unchanged))[0]["postcondition"] is True
    for arguments in (proved(), proved(updateRequired=False), proved(update=NONE)):
        evidence, refused = protocol.completion(arguments)
        assert evidence["update"] == NONE and evidence["postcondition"] is True and refused is None
    evidence, refused = protocol.completion(proved(update=COMPLETE))
    assert evidence["postcondition"] is False and refused.endswith("not proved: update")
    assert protocol.absence({"observation": {}}, DIGEST)["update"] == dict(NONE, state="")


def test_an_observation_publishes_an_unfinished_update(monkeypatch):
    for read in (dict(COMPLETE, state="upgraded", rebooted=False), dict(NONE, state="foreign"), {}):
        result, published = run(dict(proved(updateRequired=True, update=read), phase="completed", outcome="unchanged", observed=True),
                                monkeypatch)
        assert result == {"changed": False}
        assert published[0]["evidence"]["update"] == (read or dict(NONE, state="")) and published[0]["evidence"]["postcondition"] is False


@pytest.mark.parametrize("read", [
    dict(COMPLETE, state="installed"),
    dict(COMPLETE, rebooted=False),
    dict(COMPLETE, state="upgraded"),
    dict(COMPLETE, release="9"),
    dict(COMPLETE, release="9.08"),
    dict(COMPLETE, kernel=""),
    dict(COMPLETE, kernel="5.14 0"),
    dict(COMPLETE, transaction=-1),
    dict(COMPLETE, packages=True),
    dict(COMPLETE, transaction="3"),
    dict(COMPLETE, changed=True),
    dict(NONE, release="9.8"),
    dict(NONE, state="absent", packages=1),
    {"state": "complete"},
    "complete",
], ids=repr)
def test_a_malformed_update_is_never_published(read, monkeypatch):
    with pytest.raises(ValueError, match="update"):
        protocol.presence(proved(update=read, updateRequired=True), DIGEST)
    result, published = run(dict(proved(update=read, updateRequired=True), phase="completed", outcome="changed"), monkeypatch)
    assert result.get("failed") is True and not published


@pytest.mark.parametrize("reason, detail", [
    ("update-release-unavailable", None), ("update-release-mismatch", "9.6"), ("update-release-drift", "9.9@rhel-9-baseos"),
    ("update-no-repository", None), ("update-foreign", None), ("update-unavailable", "metadata"), ("update-failed", "transaction"),
])
def test_each_update_refusal_is_named_with_its_detail(reason, detail, monkeypatch):
    args = {"phase": "refused", "reason": reason}
    if detail is not None:
        args["detail"] = detail
    result, published = run(args, monkeypatch)
    assert result == {"changed": False}
    assert published == [dict({"phase": "refused", "reason": reason}, **({"detail": detail} if detail else {}))]


@pytest.mark.parametrize("detail", ["https://mirror.example.test/rhel", "9.9 from repo", "x" * 161, "cause\n"], ids=repr)
def test_a_detail_outside_the_closed_set_is_never_published(detail, monkeypatch):
    result, published = run({"phase": "refused", "reason": "update-failed", "detail": detail}, monkeypatch)
    assert result == {"changed": False} and published == [{"phase": "refused", "reason": "update-failed"}]
