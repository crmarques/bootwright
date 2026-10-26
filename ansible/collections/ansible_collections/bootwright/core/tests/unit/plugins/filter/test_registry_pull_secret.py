"""The mirror merge never changes another credential or discloses a refusal's input."""
import base64
import json

import pytest
from ansible.errors import AnsibleFilterError
from ansible_collections.bootwright.core.plugins.filter.registry_pull_secret import registry_pull_secret

MIRROR = {"host": "mirror.example.test:5000", "credentialsRef": "mirror", "credentialsType": "usernamePassword"}


def test_username_password_merge_preserves_original_and_password_whitespace():
    original = {"auths": {"other.example.test": {"auth": "existing"}}, "extra": True}
    result = json.loads(registry_pull_secret(json.dumps(original), MIRROR, "user", " secret "))
    assert result["auths"][MIRROR["host"]] == {"auth": base64.b64encode(b"user: secret ").decode()}
    assert result["auths"]["other.example.test"] == original["auths"]["other.example.test"]
    assert result["extra"] is True
    assert registry_pull_secret(json.dumps(result), MIRROR, "user", " secret ") == json.dumps(result, sort_keys=True, separators=(",", ":"))


def test_docker_config_merges_only_selected_host():
    mirror = dict(MIRROR, credentialsType="dockerConfigJson")
    result = json.loads(registry_pull_secret('{"auths":{}}', mirror,
                                             json.dumps({"auths": {mirror["host"]: {"auth": "selected"}, "other": {"auth": "unselected"}}})))
    assert result == {"auths": {mirror["host"]: {"auth": "selected"}}}


@pytest.mark.parametrize("original,credential", [
    ('{"auths":{"mirror.example.test:5000":{"auth":"sensitive-sentinel"}}}', "user"),
    ("sensitive-sentinel", "user"),
])
def test_refusals_never_echo_material(original, credential):
    with pytest.raises(AnsibleFilterError) as caught:
        registry_pull_secret(original, MIRROR, credential, "sensitive-sentinel")
    assert "sensitive-sentinel" not in str(caught.value)


def test_missing_host_in_docker_credential_refuses():
    with pytest.raises(AnsibleFilterError):
        registry_pull_secret('{"auths":{}}', dict(MIRROR, credentialsType="dockerConfigJson"), '{"auths":{}}')
