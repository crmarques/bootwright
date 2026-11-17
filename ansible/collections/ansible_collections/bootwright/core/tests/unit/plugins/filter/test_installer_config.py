"""The install configuration writer refuses without disclosing a path or a secret."""
import pytest
from ansible.errors import AnsibleFilterError
from ansible_collections.bootwright.core.plugins.filter.installer_config import installer_config

REQUEST = {"installConfig": {"metadata": {"name": "ocp"}, "pullSecret": "", "sshKey": ""}}


def material(tmp_path, **files):
    bound = {"sshKey": "ssh-ed25519 AAAA core"}
    for key, text in dict({"pullSecret": '{"auths": {}}\n'}, **files).items():
        (tmp_path / key).write_text(text)
        bound[key] = str(tmp_path / key)
    return bound


def test_a_selected_trust_bundle_the_runner_did_not_bind_refuses(tmp_path):
    with pytest.raises(AnsibleFilterError, match="trust bundle is not bound"):
        installer_config(dict(REQUEST, trustBundleRefs=["ca"]), material(tmp_path))


def test_a_bound_file_that_cannot_be_read_names_no_path(tmp_path):
    bound = material(tmp_path)
    bound["pullSecret"] = str(tmp_path / "missing-secret-path")
    with pytest.raises(AnsibleFilterError) as refused:
        installer_config(REQUEST, bound)
    assert "missing-secret-path" not in str(refused.value)


def test_a_mirror_credential_that_does_not_parse_discloses_nothing(tmp_path):
    mirror = {"host": "mirror.example.test", "credentialsRef": "mirror", "credentialsType": "dockerConfigJson"}
    with pytest.raises(AnsibleFilterError) as refused:
        installer_config(dict(REQUEST, registryMirror=mirror), material(tmp_path, mirrorAuth="not-json-s3cret"))
    assert "s3cret" not in str(refused.value)


def test_the_frozen_request_is_never_changed(tmp_path):
    request = dict(REQUEST, installConfig=dict(REQUEST["installConfig"]))
    installer_config(request, material(tmp_path))
    assert request["installConfig"] == REQUEST["installConfig"]
