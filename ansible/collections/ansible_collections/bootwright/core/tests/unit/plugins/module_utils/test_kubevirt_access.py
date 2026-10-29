"""The lend of a hosted KubeVirt provider's access over a fake host API."""

from __future__ import annotations

import json
import os
import stat

import pytest
from ansible.module_utils.common.yaml import yaml_load

from ansible_collections.bootwright.core.plugins.module_utils import kubevirt_access as access
from ansible_collections.bootwright.core.plugins.module_utils import kubevirt_api as kapi

CA = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"
SERVER = "https://api.host.example.test:6443"
TOKEN_PATH = "/api/v1/namespaces/guests/serviceaccounts/bootwright-kubevirt/token"


def request(**extra):
    value = {"cluster": "host", "expirationSeconds": 1800, "namespace": "guests", "serviceAccount": "bootwright-kubevirt",
             "version": "substrate-access-kubevirt-v1"}
    value.update(extra)
    return value


class Host:
    def __init__(self, status=201, granted=None, token="minted-token"):
        self.status, self.granted, self.token, self.requests = status, granted, token, []

    def __call__(self, method, target, headers, payload):
        self.requests.append((method, target, headers, json.loads(payload) if payload else None))
        body = json.loads(payload)
        spec = dict(body["spec"])
        if self.granted is not None:
            spec["expirationSeconds"] = self.granted
        return self.status, json.dumps({"spec": spec, "status": {"token": self.token}}).encode()


def admin(api):
    return kapi.Client({"server": SERVER, "ca": CA, "certificate": "cert", "key": "key"}, transport=api)


def test_the_lent_kubeconfig_carries_the_host_server_and_authority_and_the_token_alone():
    api = Host()
    data, evidence = access.mint(admin(api), request())
    lent = kapi.accepted(yaml_load(data))
    assert lent == {"server": SERVER, "ca": CA, "token": "minted-token"}
    assert evidence == {"expirationSeconds": 1800, "postcondition": True, "server": SERVER}
    method, target, ignored_headers, body = api.requests[0]
    assert (method, target) == ("POST", TOKEN_PATH) and body["kind"] == "TokenRequest" and body["spec"] == {"expirationSeconds": 1800}
    assert "minted-token" not in json.dumps(evidence) and json.loads(data)["users"][0]["user"] == {"token": "minted-token"}


def test_a_shorter_granted_lifetime_proves_no_lend():
    ignored_data, evidence = access.mint(admin(Host(granted=900)), request())
    assert not evidence["postcondition"] and evidence["expirationSeconds"] == 900


@pytest.mark.parametrize("status, reason", [(404, "is absent on host cluster host"), (403, "refused the token")])
def test_a_host_that_mints_nothing_refuses_by_name(status, reason):
    with pytest.raises(kapi.Refused, match=reason):
        access.mint(admin(Host(status=status)), request())


@pytest.mark.parametrize("changed", [{"version": "substrate-access-kubevirt-v0"}, {"expirationSeconds": 60}, {"expirationSeconds": True}])
def test_a_request_outside_its_bounds_reaches_no_host(changed):
    api = Host()
    with pytest.raises(kapi.Refused):
        access.mint(admin(api), request(**changed))
    assert api.requests == []


def test_the_output_is_written_root_only_and_never_over_another_file(tmp_path):
    output = tmp_path / "kubeconfig"
    access.write(str(output), "{}")
    assert stat.S_IMODE(os.stat(output).st_mode) == 0o600
    with pytest.raises(FileExistsError):
        access.write(str(output), "{}")
