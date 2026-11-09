"""The lend of a hosted KubeVirt provider's access, and its revoke, over a fake host API."""

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
SECRETS = "/api/v1/namespaces/guests/secrets"
SECRET = "bootwright-kubevirt-lend-0123456789abcdef"
TOKEN_PATH = "/api/v1/namespaces/guests/serviceaccounts/bootwright-kubevirt/token"
NOW = "2026-11-08T12:00:00Z"


def request(**extra):
    value = {"cluster": "host", "expirationSeconds": 1800, "namespace": "guests", "secret": SECRET,
             "serviceAccount": "bootwright-kubevirt", "sweep": True, "version": "substrate-access-kubevirt-v1"}
    value.update(extra)
    return value


def marked(name, uid, created, lifetime="1800"):
    return {"metadata": {"name": name, "uid": uid, "creationTimestamp": created, "labels": {access.MARK: lifetime}}}


class Host:
    """A namespace's Secrets and its ServiceAccount's token endpoint."""

    def __init__(self, status=201, granted=None, token="minted-token", secrets=None, refuse=None, bind=True, once=None, silent=None):
        self.status, self.granted, self.token, self.bind, self.requests = status, granted, token, bind, []
        self.secrets = dict(secrets or {})
        self.refuse = refuse or {}
        self.once = dict(once or {})
        self.silent = silent or set()

    def __call__(self, method, target, headers, payload):
        body = json.loads(payload) if payload else None
        self.requests.append((method, target, headers, body))
        route = target.split("?", 1)[0]
        if (method, route) in self.silent:
            raise kapi.Unanswered()
        if (method, route) in self.once:
            return self.once.pop((method, route)), b"{}"
        if (method, route) in self.refuse:
            return self.refuse[(method, route)], b"{}"
        if method == "POST" and route == SECRETS:
            name = body["metadata"]["name"]
            if name in self.secrets:
                return 409, b"{}"
            self.secrets[name] = {"metadata": dict(body["metadata"], uid="uid-" + name, creationTimestamp=NOW)}
            return 201, json.dumps(self.secrets[name]).encode()
        if method == "GET" and route == SECRETS:
            return 200, json.dumps({"items": [value for value in self.secrets.values() if access.MARK in value["metadata"].get("labels", {})]}).encode()
        if route.startswith(SECRETS + "/"):
            name = route.rsplit("/", 1)[1]
            live = self.secrets.get(name)
            if live is None:
                return 404, b"{}"
            if method == "GET":
                return 200, json.dumps(live).encode()
            if body["preconditions"]["uid"] != live["metadata"]["uid"]:
                return 409, b"{}"
            del self.secrets[name]
            return 200, b"{}"
        if method == "POST" and route == TOKEN_PATH:
            spec = dict(body["spec"])
            if self.granted is not None:
                spec["expirationSeconds"] = self.granted
            if not self.bind:
                spec.pop("boundObjectRef")
            return self.status, json.dumps({"spec": spec, "status": {"token": self.token}}).encode()
        raise AssertionError((method, target))


def admin(api):
    return kapi.Client({"server": SERVER, "ca": CA, "certificate": "cert", "key": "key"}, transport=api, sleep=lambda seconds: None)


def test_the_lent_kubeconfig_carries_the_host_server_and_authority_and_the_token_alone():
    api = Host()
    data, evidence = access.mint(admin(api), request())
    lent = kapi.accepted(yaml_load(data))
    assert lent == {"server": SERVER, "ca": CA, "token": "minted-token"}
    assert evidence == {"expirationSeconds": 1800, "postcondition": True, "secret": SECRET, "server": SERVER}
    assert "minted-token" not in json.dumps(evidence) and json.loads(data)["users"][0]["user"] == {"token": "minted-token"}


def test_the_token_is_bound_to_the_per_lend_secret_created_first():
    api = Host()
    access.mint(admin(api), request())
    created, ignored_list, token = api.requests
    assert created[:2] == ("POST", SECRETS)
    assert created[3]["metadata"] == {"name": SECRET, "namespace": "guests", "labels": {access.MARK: "1800"}} and "data" not in created[3]
    assert token[:2] == ("POST", TOKEN_PATH) and token[3]["kind"] == "TokenRequest"
    assert token[3]["spec"] == {"expirationSeconds": 1800,
                                "boundObjectRef": {"apiVersion": "v1", "kind": "Secret", "name": SECRET, "uid": "uid-" + SECRET}}
    assert SECRET in api.secrets


def test_a_long_installation_run_is_lent_a_token_outliving_it():
    # An eight-hour run plus the ten-minute minimum: no fixed ceiling bounds the run (B926).
    api = Host()
    ignored_data, evidence = access.mint(admin(api), request(expirationSeconds=8 * 3600 + 600))
    assert evidence["postcondition"] and evidence["expirationSeconds"] == 8 * 3600 + 600
    assert api.requests[-1][3]["spec"]["expirationSeconds"] == 8 * 3600 + 600


def test_a_shorter_granted_lifetime_proves_no_lend():
    ignored_data, evidence = access.mint(admin(Host(granted=900)), request())
    assert not evidence["postcondition"] and evidence["expirationSeconds"] == 900


@pytest.mark.parametrize("host, reason", [
    (Host(status=404), "is absent on host cluster host"),
    (Host(status=403), "refused the token"),
    (Host(bind=False), "not bound to its per-lend Secret"),
])
def test_a_host_that_mints_no_bound_token_refuses_by_name_and_deletes_its_secret(host, reason):
    with pytest.raises(kapi.Refused, match=reason):
        access.mint(admin(host), request())
    assert SECRET not in host.secrets and host.requests[-1][:2] == ("DELETE", SECRETS + "/" + SECRET)


def test_a_secret_of_the_lends_name_is_never_adopted():
    foreign = {SECRET: {"metadata": {"name": SECRET, "uid": "foreign"}}}
    api = Host(secrets=foreign)
    with pytest.raises(kapi.Refused, match="already exists"):
        access.mint(admin(api), request())
    assert api.secrets == foreign and [entry[0] for entry in api.requests] == ["POST"]


def test_a_namespace_that_refuses_the_secret_mints_nothing():
    api = Host(refuse={("POST", SECRETS): 403})
    with pytest.raises(kapi.Refused, match="refused the per-lend Secret"):
        access.mint(admin(api), request())
    assert all(target != TOKEN_PATH for ignored, target, *rest in api.requests)


def test_a_lend_sweeps_only_marked_secrets_whose_token_has_expired():
    api = Host(secrets={
        "expired": marked("expired", "u1", "2026-11-08T11:00:00Z", "600"),
        "live": marked("live", "u2", "2026-11-08T11:50:00Z", "1800"),
        "unmarked-lifetime": marked("unmarked-lifetime", "u3", "2026-11-08T01:00:00Z", "forever"),
        "plain": {"metadata": {"name": "plain", "uid": "u4", "creationTimestamp": "2026-11-01T00:00:00Z"}},
    })
    access.mint(admin(api), request())
    assert set(api.secrets) == {"live", "unmarked-lifetime", "plain", SECRET}
    deleted = [body for method, target, ignored, body in api.requests if method == "DELETE"]
    assert deleted == [{"apiVersion": "v1", "kind": "DeleteOptions", "preconditions": {"uid": "u1"}}]
    listed = [target for method, target, *rest in api.requests if method == "GET"]
    assert listed == [SECRETS + "?labelSelector=bootwright.io%2Flend&limit=64"]


def test_a_lend_that_may_not_sweep_deletes_nothing_but_its_own_secret():
    # A read-only or unrecorded execution's lend creates only its own Secret.
    api = Host(secrets={"expired": marked("expired", "u1", "2026-11-08T11:00:00Z", "600")})
    access.mint(admin(api), request(sweep=False))
    assert set(api.secrets) == {"expired", SECRET}
    assert [entry[:2] for entry in api.requests] == [("POST", SECRETS), ("POST", TOKEN_PATH)]


def expired(count):
    return {"old-%d" % index: marked("old-%d" % index, "u%d" % index, "2026-11-08T10:00:00Z", "600") for index in range(count)}


def deletes(api):
    return [target for method, target, *rest in api.requests if method == "DELETE"]


def test_a_sweep_stops_at_the_first_delete_the_host_does_not_answer():
    api = Host(secrets=expired(3), silent={("DELETE", SECRETS + "/old-0")})
    swept = access.sweep(admin(api), "guests", SECRET, NOW)
    assert swept == 0 and deletes(api) == [SECRETS + "/old-0"] and len(api.secrets) == 3


def test_a_sweep_issues_no_delete_once_its_time_is_spent():
    ticks = iter([0, 0, 10, access.SWEEP_SECONDS, access.SWEEP_SECONDS + 1])
    api = Host(secrets=expired(4))
    swept = access.sweep(admin(api), "guests", SECRET, NOW, clock=lambda: next(ticks))
    assert swept == 2 and deletes(api) == [SECRETS + "/old-0", SECRETS + "/old-1"] and set(api.secrets) == {"old-2", "old-3"}


def test_a_revoke_retries_a_delete_the_host_answered_transiently():
    api = Host()
    access.mint(admin(api), request())
    api.once[("DELETE", SECRETS + "/" + SECRET)] = 503
    assert access.revoke(admin(api), request())["postcondition"]
    assert SECRET not in api.secrets and deletes(api) == [SECRETS + "/" + SECRET] * 2


def test_a_sweep_the_host_does_not_answer_still_lends():
    api = Host(refuse={("GET", SECRETS): 503})
    ignored_data, evidence = access.mint(admin(api), request())
    assert evidence["postcondition"]


def test_a_revoke_deletes_exactly_the_marked_secret_and_proves_it_absent():
    api = Host()
    access.mint(admin(api), request())
    evidence = access.revoke(admin(api), request())
    assert evidence == {"expirationSeconds": 0, "postcondition": True, "secret": SECRET, "server": SERVER}
    assert SECRET not in api.secrets
    assert api.requests[-1][3] == {"apiVersion": "v1", "kind": "DeleteOptions", "preconditions": {"uid": "uid-" + SECRET}}


def test_a_revoke_of_a_secret_already_gone_holds():
    api = Host()
    assert access.revoke(admin(api), request())["postcondition"]
    assert [entry[0] for entry in api.requests] == ["GET"]


def test_a_revoke_leaves_a_secret_without_the_lend_mark():
    foreign = {SECRET: {"metadata": {"name": SECRET, "uid": "foreign"}}}
    api = Host(secrets=foreign)
    with pytest.raises(kapi.Refused, match="carries no lend mark"):
        access.revoke(admin(api), request())
    assert api.secrets == foreign


@pytest.mark.parametrize("changed", [{"version": "substrate-access-kubevirt-v0"}, {"expirationSeconds": 60}, {"expirationSeconds": True},
                                     {"secret": "Not/A-Name"}, {"secret": ""}, {"sweep": "yes"}, {"sweep": None}])
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
