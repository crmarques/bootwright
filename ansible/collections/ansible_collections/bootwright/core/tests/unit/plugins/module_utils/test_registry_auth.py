"""One registry authentication rule set, shared by the Ceph discovery, the pull broker, the Ceph login and the mirrors."""

import base64
import json
import stat

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import (
    ceph_pull_broker, ceph_registry_discovery, registry_auth,
)

REGISTRY = "cp.icr.io"
NAMESPACE = "cp/ibm-ceph/ceph-9-rhel9"
CHALLENGE = 'Bearer realm="https://cp.icr.io/oauth/token",service="registry",scope="repository:%s:pull"' % NAMESPACE


def test_a_credential_encodes_user_and_password():
    assert registry_auth.basic("cp", "secret") == "Basic " + base64.b64encode(b"cp:secret").decode()
    assert registry_auth.decoded(registry_auth.credential("cp", "secret")) == ("cp", "secret")


@pytest.mark.parametrize("username, password", [
    ("", "secret"), ("cp", ""), ("c:p", "secret"), ("cp\n", "secret"), ("cp", "se\x00cret"), ("cp", "se\rcret"),
    ("u" * (registry_auth.MAX_USERNAME + 1), "secret"), ("cp", "p" * (registry_auth.MAX_PASSWORD + 1)), (None, "secret"),
])
def test_an_invalid_credential_refuses(username, password):
    with pytest.raises(registry_auth.Refused):
        registry_auth.credential(username, password)


@pytest.mark.parametrize("username", ["org+" + "robot" * 60, "robot$project+" + "name" * 80])
def test_a_long_robot_account_name_the_secret_api_admits_is_a_credential(username):
    assert len(username.encode()) > 256
    assert registry_auth.decoded(registry_auth.credential(username, "secret")) == (username, "secret")


def test_the_bounds_are_the_ones_every_reader_applies():
    longest = "u" * registry_auth.MAX_USERNAME
    assert registry_auth.decoded(registry_auth.credential(longest, "secret")) == (longest, "secret")


def test_an_auth_document_holds_one_authority(tmp_path):
    path = tmp_path / "auth.json"
    registry_auth.write(str(path), "mirror.example.test:8443", "bootwright", "secret")
    assert json.loads(path.read_bytes()) == {"auths": {"mirror.example.test:8443": {"auth": registry_auth.credential("bootwright", "secret")}}}
    assert stat.S_IMODE(path.stat().st_mode) == 0o600


def test_a_lent_auth_file_is_removed_after_use(tmp_path):
    with registry_auth.lent(str(tmp_path / "auth.json"), {"auths": {}}) as path:
        assert open(path, "rb").read() == b'{"auths":{}}'
    assert list(tmp_path.iterdir()) == []


def test_a_lent_auth_file_changed_by_its_borrower_refuses_and_is_still_removed(tmp_path):
    with pytest.raises(registry_auth.Refused):
        with registry_auth.lent(str(tmp_path / "auth.json"), {"auths": {}}) as path:
            with open(path, "ab") as stream:
                stream.write(b" ")
    assert list(tmp_path.iterdir()) == []


def test_a_challenge_names_the_token_path_and_admits_rfc6750_error_attributes():
    expected = "/oauth/token?service=registry&scope=repository%3A" + NAMESPACE.replace("/", "%2F") + "%3Apull"
    assert registry_auth.challenge(CHALLENGE, REGISTRY, NAMESPACE) == expected
    expired = CHALLENGE + ',error="invalid_token",error_description="The access token expired"'
    assert registry_auth.challenge(expired, REGISTRY, NAMESPACE) == expected


@pytest.mark.parametrize("value", [
    CHALLENGE.replace("https:", "http:"), CHALLENGE.replace("cp.icr.io/oauth", "foreign.invalid/oauth"),
    CHALLENGE.replace(":pull", ":pull,push"), CHALLENGE + ',other="value"', CHALLENGE.replace('service="registry"', 'service=""'),
    CHALLENGE + ',realm="https://cp.icr.io/oauth/token"', "Basic realm=x", None,
])
def test_an_unapproved_challenge_refuses(value):
    with pytest.raises(registry_auth.Refused):
        registry_auth.challenge(value, REGISTRY, NAMESPACE)


def test_a_token_answer_is_closed_and_bounded():
    assert registry_auth.token(b'{"token":"abc","expires_in":300}') == "abc"
    assert registry_auth.token(b'{"access_token":"abc"}') == "abc"
    for raw in (b'{"token":"abc","refresh_token":"x"}', b'{"token":"abc","access_token":"other"}', b'{"token":"a b"}',
                b'{"token":"abc","expires_in":true}', b'{"token":"abc","token":"abc"}', b"[]",
                b'{"token":"' + b"a" * (registry_auth.MAX_TOKEN + 1) + b'"}'):
        with pytest.raises(registry_auth.Refused):
            registry_auth.token(raw)


def test_discovery_and_the_pull_broker_apply_the_same_rules():
    for value in (CHALLENGE, CHALLENGE + ',error="invalid_token"'):
        upstream = object.__new__(ceph_pull_broker.Upstream)
        upstream.registry, upstream.namespace = REGISTRY, NAMESPACE
        assert upstream.token_path(value) == ceph_registry_discovery.challenge(value, REGISTRY, NAMESPACE)
    long_user = "u" * (registry_auth.MAX_USERNAME + 1)
    material = json.dumps({"url": REGISTRY, "username": long_user, "password": "secret"}).encode()
    with pytest.raises(ceph_registry_discovery.DiscoveryRefusal):
        ceph_registry_discovery.basic_material(material, REGISTRY)
    with pytest.raises(ceph_pull_broker.BrokerError):
        ceph_pull_broker.Upstream(REGISTRY, REGISTRY + "/" + NAMESPACE, {"url": REGISTRY, "username": long_user, "password": "secret"},
                                  0, connection=lambda host, **options: None)
