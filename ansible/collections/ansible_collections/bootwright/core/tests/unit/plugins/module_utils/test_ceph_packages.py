"""Ceph package effects require a complete frozen publisher-authenticated closure."""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

import copy
import hashlib
import inspect
import io
import json
import os
from pathlib import Path
import signal
import ssl
import stat
import subprocess
import sys
import time
import urllib.error
from types import SimpleNamespace

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import (
    ceph_crypto_policy as policy, ceph_native, ceph_package_sources as sources, ceph_package_worker,
    ceph_packages as packages, native_apply, native_resolution, native_signatures,
)


ACTUAL_PLATFORM = packages.actual_platform
SUPPORTED = ceph_native.supported_transport


@pytest.fixture(autouse=True)
def synthetic_platform(monkeypatch):
    monkeypatch.setattr(packages, "actual_platform", lambda: {"os": "rhel", "release": "9.8", "architecture": "amd64"})


@pytest.fixture
def package_request():
    hosts = [{"name": "node-" + str(i), "machine": "machine-" + str(i), "address": "192.0.2." + str(i), "user": "operator", "port": 22,
              "devices": [{"path": "/dev/vdb", "serial": "disk" + str(i), "sizeBytes": 1 << 30}], "labels": [], "location": {},
              "sshKeyRef": "fleet-key"} for i in range(1, 4)]
    names = [h["name"] for h in hosts]
    return {"controller": "controller", "version": "ceph-core-v22", "cluster": "example", "owner": "apply-123",
            "fsid": "4c0a4300-aa43-458c-86d7-ac2256d1fc00", "image": "registry.example.test/ceph@sha256:" + "a" * 64,
            "cephadm": {"path": "/usr/sbin/cephadm", "packageVersion": "20.2.1-405.el9cp"},
            "release": "synthetic-release", "ansiblePackageVersion": "5.0.3-1", "packageRepository": "https://publisher.example.test/ceph",
            "bootstrap": "node-1",
            "monIP": "192.0.2.1", "clusterSSHUser": "operator", "clusterSSHTransport": "native", "clusterKeyRef": "cluster-key", "registryRef": "registry-key",
            "registry": "registry.example.test", "callHome": "enabled", "publicNetworks": ["192.0.2.0/24"], "clusterNetworks": [], "hosts": hosts,
            "services": [{"service_type": "mon", "placement": {"hosts": names}}, {"service_type": "mgr", "placement": {"hosts": names, "count": 2}},
                         {"service_type": "osd", "service_id": "data", "placement": {"hosts": names}, "spec": {"data_devices": {"paths": ["/dev/vdb"]}}}],
            "packages": {"version": "ceph-packages-v3", "licenseAccepted": True,
                         "sources": [{"host": name, "profile": "rhel", "media": "rhel-dvd.iso", "mediaSHA256": "c" * 64, "mediaSize": 1000000,
                                      "publicationPath": "/var/lib/bootwright-services/example/artifact-server/artifacts/public/os/rhel/tree",
                                      "url": "https://192.0.2.10:8443/os/rhel/tree", "certificateRef": "serving-tls"} for name in names]}}


def plan_fixture(actions=True, grant=None):
    grant = grant or packages.transaction_request({"cephadm": {"packageVersion": "20.2.1-405.el9cp"}, "ansiblePackageVersion": "5.0.3-1"})
    values = []
    roots = []
    for key, name, requested in native_resolution.selected_roots(grant):
        epoch, rest = ("0:1-1" if requested == "latest" else requested if ":" in requested else "0:" + requested).split(":")
        version, release = rest.split("-", 1)
        identity = {"name": name, "epoch": int(epoch), "version": version, "release": release, "architecture": "noarch" if name != "ceph-common" else "x86_64"}
        inherited = key == "inherited-policy"
        base = "https://192.0.2.10:8443/os/rhel/tree/BaseOS" if inherited else sources.IBM_BASE
        source = {"id": "native-" + hashlib.sha256(name.encode()).hexdigest()[:32], "url": base + "/" + name + ".rpm",
                  "sha256": "f" * 64, "bytes": 64}
        values.append(dict(identity, signer=native_signatures.RHEL_SIGNER if inherited else native_signatures.IBM_CEPH_SIGNER, source=source))
        roots.append({"key": key, "requested": requested, "package": identity})
    plan = {"format": native_resolution.FORMAT, "platform": grant["platform"], "requirements": grant["requirements"], "requests": grant["versions"],
            "solver": "dnf4", "solverVersion": "4.14.0", "roots": roots, "repositories": [], "packages": values,
            "actions": [{"kind": "install", "after": r["package"], "sourceID": p["source"]["id"], "reason": "root"} for r, p in zip(roots, values)] if actions
            else [], "beforeSHA256": "a" * 64, "afterSHA256": "b" * 64 if actions else "a" * 64}
    plan["digest"] = native_resolution.digest(plan)
    return plan


class Journal:
    def __init__(self):
        self.records = {}
        self.writes = []

    def __enter__(self):
        return self

    def __exit__(self, *args):
        pass

    def read(self, name):
        return self.records.get(name)

    def write(self, name, value):
        assert name not in self.records or self.records[name] == value
        self.records[name] = copy.deepcopy(value)
        self.writes.append(name)


class Acquisition:
    def __init__(self):
        self.calls = []

    def repositories(self, scratch):
        self.calls.append("metadata")
        return []

    def publisher_key(self, scratch):
        self.calls.append("key")
        return {native_signatures.IBM_CEPH_SIGNER: str(Path(scratch, "key"))}

    def payloads(self, scratch, plan):
        self.calls.append("payloads")
        return {p["source"]["id"]: str(Path(scratch, p["source"]["id"] + ".rpm")) for p in plan["packages"]}


@pytest.fixture
def boundary(package_request, monkeypatch):
    plan = plan_fixture()
    journal, acquisition = Journal(), Acquisition()
    state = {"inventorySHA256": plan["beforeSHA256"], "rootsReady": False}
    events = []
    monkeypatch.setattr(ceph_native, "supported_transport", lambda request: True)
    monkeypatch.setattr(os, "geteuid", lambda: 0)
    monkeypatch.setattr(packages, "host_admission", lambda value: None)
    monkeypatch.setattr(native_resolution, "solve4", lambda *args: copy.deepcopy(plan))
    monkeypatch.setattr(native_resolution, "inspect", lambda *args: state.copy())
    monkeypatch.setattr(native_apply, "verified_payloads", lambda *args: events.append("payload-integrity"))
    monkeypatch.setattr(native_signatures, "verify", lambda *args: events.append("publisher-verification"))
    monkeypatch.setattr(packages, "trusted_headers", lambda p, files: {v["source"]["id"]: "d" * 64 for v in p["packages"]})
    monkeypatch.setattr(packages, "installed_headers", lambda *args: True)
    monkeypatch.setattr(packages, "integrity", lambda *args: True)
    proofs = []

    def query(argv):
        assert events[:2] == ["payload-integrity", "publisher-verification"]
        selected = next(p for p in plan["packages"] if p["source"]["id"] + ".rpm" == Path(argv[-1]).name)
        paths = policy.TARGETS if selected["name"] == "crypto-policies" else {policy.GENERATOR}
        return ("algo|8\n" + "".join(path + "|" + "e" * 64 + "|" + str(stat.S_IFREG | 0o644) + "|root|root|0|1\n" for path in sorted(paths))).encode()

    monkeypatch.setattr(packages, "rpm_query", query)
    monkeypatch.setattr(policy, "installed", lambda record, unused: proofs.append(record))

    def apply(grant, scratch):
        assert os.environ["ACCEPT_EULA"] == "yes"
        assert journal.records["decision.json"]["plan"] == plan
        assert journal.records["verified.json"]["planDigest"] == plan["digest"]
        assert events == ["payload-integrity", "publisher-verification"]
        assert acquisition.calls == ["metadata", "key", "payloads"]
        events.append("transaction")
        state.update(inventorySHA256=plan["afterSHA256"], rootsReady=True)
        return {"changed": True, "planDigest": plan["digest"], "afterSHA256": plan["afterSHA256"]}

    monkeypatch.setattr(native_apply, "apply", apply)
    return SimpleNamespace(plan=plan, journal=journal, acquisition=acquisition, state=state, events=events, proofs=proofs)


def prepare(package_request, boundary):
    return packages.prepare(package_request, "node-1", "public-certificate", acquisition=boundary.acquisition, journal=boundary.journal)


@pytest.mark.parametrize("site, reason, written", [("repositories", "package-resolution", []), ("payloads", "package-signature", ["decision.json"])])
def test_an_unverified_repository_fails_its_package_phase_before_any_effect(package_request, boundary, site, reason, written):
    message = "TLS verification of the package repository " + package_request["packageRepository"] + " failed against trust bundle Secret repository-ca"

    def unverified(*args):
        raise sources.RepositoryTrustFailure(message)

    setattr(boundary.acquisition, site, unverified)
    with pytest.raises(ceph_native.NativeFailure) as failure:
        prepare(package_request, boundary)
    assert str(failure.value) == reason and failure.value.native_error["cause"] == message
    assert boundary.journal.writes == written and "transaction" not in boundary.events


def test_frozen_complete_closure_and_publisher_checks_precede_eula_transaction(package_request, boundary):
    first = prepare(package_request, boundary)
    assert first == {"changed": True, "ready": True, "planDigest": boundary.plan["digest"]}
    assert boundary.journal.writes == ["decision.json", "verified.json", "complete.json"]
    assert boundary.events[-1] == "transaction"
    replay = prepare(package_request, boundary)
    assert replay == dict(first, changed=False)
    assert boundary.acquisition.calls == ["metadata", "key", "payloads"]
    assert boundary.events.count("transaction") == 1
    assert "ACCEPT_EULA" not in os.environ


def test_killed_transaction_recovers_only_exact_authenticated_after_state(package_request, boundary, monkeypatch):
    def interrupted(*args):
        boundary.state.update(inventorySHA256=boundary.plan["afterSHA256"], rootsReady=True)
        raise RuntimeError("lost transport")

    monkeypatch.setattr(native_apply, "apply", interrupted)
    with pytest.raises(ceph_native.Refusal, match="^package-transaction$"):
        prepare(package_request, boundary)
    assert "complete.json" not in boundary.journal.records
    result = prepare(package_request, boundary)
    assert result["changed"] is True and result["ready"] is True
    assert boundary.acquisition.calls.count("metadata") == 1
    assert boundary.acquisition.calls.count("payloads") == 2


def test_same_nevra_foreign_header_is_not_recovered_or_claimed_ready(package_request, boundary, monkeypatch):
    boundary.state.update(inventorySHA256=boundary.plan["afterSHA256"], rootsReady=True)
    monkeypatch.setattr(packages, "installed_headers", lambda *args: False)
    with pytest.raises(ceph_native.Refusal, match="package-drift"):
        prepare(package_request, boundary)
    assert "complete.json" not in boundary.journal.records
    assert "transaction" not in boundary.events


def test_partial_or_foreign_inventory_never_gets_a_fresh_plan_or_transaction(package_request, boundary, monkeypatch):
    boundary.state.update(inventorySHA256="f" * 64)
    with pytest.raises(ceph_native.Refusal, match="package-drift"):
        prepare(package_request, boundary)
    monkeypatch.setattr(native_resolution, "solve4", lambda *args: pytest.fail("re-resolved an immutable decision"))
    with pytest.raises(ceph_native.Refusal, match="package-drift"):
        prepare(package_request, boundary)
    assert boundary.acquisition.calls.count("metadata") == 1
    assert "transaction" not in boundary.events


def test_replay_checks_installed_closure_without_repository_access(package_request, boundary, monkeypatch):
    prepare(package_request, boundary)
    monkeypatch.setattr(packages, "integrity", lambda *args: False)
    with pytest.raises(ceph_native.Refusal, match="package-drift"):
        prepare(package_request, boundary)
    assert boundary.acquisition.calls == ["metadata", "key", "payloads"]


@pytest.mark.parametrize("inventory", ["a" * 64, "f" * 64])
def test_completed_replay_refuses_unrelated_inventory_without_rewriting_or_effects(package_request, boundary, monkeypatch, inventory):
    prepare(package_request, boundary)
    # The owned roots, authenticated headers and payloads remain valid. Only
    # the host inventory has reverted or acquired an unrelated package change.
    boundary.state["inventorySHA256"] = inventory
    records = copy.deepcopy(boundary.journal.records)
    writes = list(boundary.journal.writes)
    calls = list(boundary.acquisition.calls)
    events = list(boundary.events)
    monkeypatch.setattr(native_resolution, "solve4", lambda *args: pytest.fail("re-resolved a completed package decision"))
    monkeypatch.setattr(native_apply, "apply", lambda *args: pytest.fail("mutated a completed package decision"))
    with pytest.raises(ceph_native.Refusal, match="package-drift"):
        prepare(package_request, boundary)
    assert boundary.state["rootsReady"] is True
    assert boundary.journal.records == records and boundary.journal.writes == writes
    assert boundary.acquisition.calls == calls and boundary.events == events


@pytest.mark.parametrize("field,value", [("licenseAccepted", False), ("version", "other"), ("sources", [])])
def test_missing_grants_refuse_before_package_effects(package_request, boundary, field, value):
    package_request["packages"][field] = value
    with pytest.raises(ceph_native.Refusal):
        prepare(package_request, boundary)
    assert boundary.acquisition.calls == [] and boundary.journal.writes == []


def test_an_unsupported_release_prevents_even_package_acquisition(package_request, boundary, monkeypatch):
    monkeypatch.setattr(ceph_native, "supported_transport", SUPPORTED)
    with pytest.raises(ceph_native.Refusal, match="^unsupported-release$"):
        prepare(package_request, boundary)
    assert boundary.acquisition.calls == [] and boundary.journal.writes == []


def test_license_acceptance_is_yes_scoped_and_never_inferred_from_environment(monkeypatch):
    monkeypatch.setenv("ACCEPT_EULA", "operator-value")
    with pytest.raises(ceph_native.Refusal):
        with packages.accepted_license(False):
            pytest.fail("unaccepted license grant")
    with pytest.raises(RuntimeError):
        with packages.accepted_license(True):
            assert os.environ["ACCEPT_EULA"] == "yes"
            raise RuntimeError("vendor failure")
    assert os.environ["ACCEPT_EULA"] == "operator-value"


def test_shared_resolver_ceph_policy_has_only_exact_provider_roots():
    grant = packages.transaction_request({"cephadm": {"packageVersion": "20.2.1-405.el9cp"}, "ansiblePackageVersion": "5.0.3-1"})
    assert native_resolution.selected_roots(grant) == [("ceph", "cephadm", "20.2.1-405.el9cp"), ("ceph", "ceph-common", "20.2.1-405.el9cp"),
                                                       ("ansible", "cephadm-ansible", "5.0.3-1"), ("license", "ibm-storage-ceph-license", "latest"),
                                                       ("broker", "openssl", "latest"),
                                                       ("inherited-policy", "crypto-policies", "latest"),
                                                       ("inherited-policy", "crypto-policies-scripts", "latest")]
    for change in (dict(policy="unknown"), dict(versions={"ceph": "latest", "license": "latest"}),
                   dict(requirements={"cephPackages": True, "containerRuntime": True})):
        with pytest.raises(ValueError):
            native_resolution.selected_roots(dict(grant, **change))


def test_publisher_keys_are_scoped_to_ibm_and_rhel(monkeypatch):
    grant = packages.transaction_request({"cephadm": {"packageVersion": "20.2.1-405.el9cp"}, "ansiblePackageVersion": "5.0.3-1"})
    grant["publisherKeys"] = {native_signatures.IBM_CEPH_SIGNER: "/private/ibm-key"}
    seen = []
    monkeypatch.setattr(native_signatures, "read_key", lambda path: seen.append(path) or b"public-key")
    native_signatures.key_for(grant, native_signatures.IBM_CEPH_SIGNER)
    native_signatures.key_for(grant, native_signatures.RHEL_SIGNER)
    assert seen == ["/private/ibm-key", "/etc/pki/rpm-gpg/RPM-GPG-KEY-redhat-release"]
    with pytest.raises(ValueError):
        native_signatures.key_for(grant, "f" * 40)


def test_publisher_keys_refuse_an_extra_key_and_a_redhat_signer_on_a_foreign_platform(monkeypatch):
    events = []
    live_key_doubles(monkeypatch, events)
    seen = []
    read = native_signatures.read_key
    monkeypatch.setattr(native_signatures, "read_key", lambda path: seen.append(path) or read(path))
    extra = live_key_grant()
    extra["publisherKeys"] = {native_signatures.IBM_CEPH_SIGNER: "/private/ibm-key", "e" * 40: "/private/extra-key"}
    with pytest.raises(ValueError, match="Ceph publisher authority"):
        native_signatures.key_for(extra, native_signatures.IBM_CEPH_SIGNER)
    assert seen == []
    with pytest.raises(ValueError, match="Ceph publisher authority"):
        native_signatures.import_ceph_keys(extra)
    assert seen == [] and not [event for event in events if event[0] == "import"]
    foreign = live_key_grant()
    foreign["platform"] = dict(foreign["platform"], os="centos")
    with pytest.raises(ValueError, match="native publisher platform"):
        native_signatures.key_for(foreign, native_signatures.RHEL_SIGNER)
    assert seen == []


@pytest.mark.parametrize("relative",
                         ["../escape", "/absolute", "https://foreign.example.test/file", "repodata/%2e%2e/escape", "repodata/file?token=value",
                          "repodata/file#fragment"])
def test_metadata_location_cannot_expand_acquisition_authority(relative):
    with pytest.raises(ceph_native.Refusal):
        sources.child_url("https://publisher.example.test/repo", relative)


def test_acquisition_never_uses_proxy_redirect_or_unverified_tls(package_request, monkeypatch):
    contexts, openers = [], []

    def context(**kwargs):
        contexts.append(kwargs)
        return SimpleNamespace()

    monkeypatch.setattr(sources.ssl, "create_default_context", context)
    monkeypatch.setattr(sources.urllib.request, "build_opener", lambda *handlers: openers.append(handlers) or SimpleNamespace())
    sources.Acquisition(package_request["packages"]["sources"][0], "public-certificate", repository=sources.IBM_BASE)
    assert contexts == [{"cadata": "public-certificate"}, {}]
    assert all(handlers[0].proxies == {} for handlers in openers)
    with pytest.raises(ceph_native.Refusal):
        sources.NoRedirect().redirect_request(None, None, 302, None, None, "https://foreign.example.test")


def recording_openers(monkeypatch):
    contexts, openers = [], []

    def context(**kwargs):
        contexts.append(kwargs)
        return SimpleNamespace(minimum_version=None)

    monkeypatch.setattr(sources.ssl, "create_default_context", context)
    monkeypatch.setattr(sources.urllib.request, "build_opener", lambda *handlers: openers.append(handlers) or SimpleNamespace(handlers=handlers))
    return contexts, openers


def test_acquisition_without_a_bundle_keeps_the_host_trust_store(package_request, monkeypatch):
    source = package_request["packages"]["sources"][0]
    contexts, openers = recording_openers(monkeypatch)
    acquisition = sources.Acquisition(source, "public-certificate", repository="https://mirror.example.test/ceph")
    assert contexts == [{"cadata": "public-certificate"}, {}] and len(openers) == 2
    assert acquisition.repository_opener is acquisition.public and acquisition.anchor == "the host trust store"
    contexts.clear()
    openers.clear()
    acquisition = sources.Acquisition(source, "public-certificate", repository="https://mirror.example.test/ceph", trust="bundle", trust_ref="repository-ca")
    assert contexts == [{"cadata": "public-certificate"}, {}, {"cadata": "bundle"}] and len(openers) == 3
    assert acquisition.repository_opener is not acquisition.public and acquisition.repository_opener is not acquisition.local
    assert acquisition.anchor == "trust bundle Secret repository-ca"
    for handlers in openers:
        assert handlers[0].proxies == {} and isinstance(handlers[1], sources.NoRedirect)
    bundle = acquisition.repository_opener.handlers[2]._context
    assert bundle.minimum_version == sources.ssl.TLSVersion.TLSv1_2
    for trust, trust_ref in (("bundle", None), (None, "repository-ca"), ("bundle", "Bad Name"), ("", "repository-ca"), (b"bundle", "repository-ca")):
        with pytest.raises(ceph_native.Refusal, match="^material$"):
            sources.Acquisition(source, "public-certificate", repository="https://mirror.example.test/ceph", trust=trust, trust_ref=trust_ref)

    def rejecting(**kwargs):
        if "bundle" in kwargs.get("cadata", ""):
            raise sources.ssl.SSLError("no certificate")
        return SimpleNamespace(minimum_version=None)

    monkeypatch.setattr(sources.ssl, "create_default_context", rejecting)
    with pytest.raises(ceph_native.Refusal, match="^material$"):
        sources.Acquisition(source, "public-certificate", repository="https://mirror.example.test/ceph", trust="bundle", trust_ref="repository-ca")


class Recorder:
    def __init__(self, name, seen):
        self.name, self.seen = name, seen

    def open(self, request, timeout):
        self.seen.append((self.name, request.full_url))
        response = io.BytesIO(b"payload")
        response.status, response.headers, response.geturl = 200, {}, lambda: request.full_url
        return response


def test_vendor_mode_unchanged(package_request, monkeypatch):
    seen = []
    recording_openers(monkeypatch)
    source = package_request["packages"]["sources"][0]
    acquisition = sources.Acquisition(source, "public-certificate", repository=sources.IBM_BASE)
    assert acquisition.repository_opener is acquisition.public
    acquisition.local, acquisition.public = Recorder("local", seen), Recorder("public", seen)
    acquisition.repository_opener = acquisition.public
    for url in (sources.IBM_BASE + "/repodata/repomd.xml", sources.IBM_KEY, source["url"] + "/BaseOS/repodata/repomd.xml"):
        acquisition.stream(url, io.BytesIO(), 7)
    assert [entry[0] for entry in seen] == ["public", "public", "local"]
    called = []
    monkeypatch.setattr(packages, "prepare", lambda *args, **kwargs: called.append(kwargs) or {})
    payload = {"request": dict(package_request, packageRepository=sources.IBM_BASE), "host": "node-1", "private": {"certificate": "public-certificate"}}
    packages.entrypoint(payload)
    assert called == [{"trust": None}]
    with pytest.raises(ceph_native.Refusal, match="^material$"):
        packages.entrypoint(dict(payload, private={"certificate": "public-certificate", "packageRepositoryCA": "bundle"}))
    bound = copy.deepcopy(payload)
    bound["request"]["packages"]["packageRepositoryTrustBundleRef"] = "repository-ca"
    with pytest.raises(ceph_native.Refusal, match="^material$"):
        packages.entrypoint(bound)
    packages.entrypoint(dict(bound, private={"certificate": "public-certificate", "packageRepositoryCA": "bundle"}))
    assert called == [{"trust": None}, {"trust": "bundle"}]


def test_signer_rule_unchanged_for_a_mirror(package_request, monkeypatch, tmp_path):
    seen = []
    recording_openers(monkeypatch)
    mirror = "https://mirror.example.test/ceph"
    acquisition = sources.Acquisition(package_request["packages"]["sources"][0], "public-certificate", repository=mirror,
                                      trust="bundle", trust_ref="repository-ca")
    acquisition.local, acquisition.public, acquisition.repository_opener = Recorder("local", seen), Recorder("public", seen), Recorder("repository", seen)
    plan = plan_fixture()
    root = next(p for p in plan["packages"] if p["name"] == "cephadm")
    root["source"] = dict(root["source"], url=mirror + "/cephadm.rpm", sha256=hashlib.sha256(b"payload").hexdigest(), bytes=7)
    plan["packages"] = [root]
    acquisition.payloads(str(tmp_path), plan)
    assert seen == [("repository", mirror + "/cephadm.rpm")]
    root["signer"] = native_signatures.RHEL_SIGNER
    with pytest.raises(ceph_native.Refusal, match="^packages$"):
        acquisition.payloads(str(tmp_path), plan)
    assert len(seen) == 1
    seen.clear()
    dvd = package_request["packages"]["sources"][0]["url"] + "/BaseOS/repodata/repomd.xml"
    for url in (mirror + "/repodata/repomd.xml", sources.IBM_KEY, dvd):
        acquisition.stream(url, io.BytesIO(), 7)
    assert seen == [("repository", mirror + "/repodata/repomd.xml"), ("public", sources.IBM_KEY), ("local", dvd)]


@pytest.mark.parametrize("trust, trust_ref, anchor", [(None, None, "the host trust store"), ("bundle", "repository-ca", "trust bundle Secret repository-ca")])
def test_a_failed_key_connection_names_the_key_location_never_the_repository(package_request, monkeypatch, trust, trust_ref, anchor):
    recording_openers(monkeypatch)
    mirror = "https://mirror.example.test/ceph"
    acquisition = sources.Acquisition(package_request["packages"]["sources"][0], "public-certificate", repository=mirror, trust=trust, trust_ref=trust_ref)

    def unverified(request, timeout):
        raise urllib.error.URLError(ssl.SSLCertVerificationError("unknown authority"))

    failing = SimpleNamespace(open=unverified)
    acquisition.public = failing
    if trust is None:
        acquisition.repository_opener = failing
    with pytest.raises(sources.RepositoryTrustFailure) as failure:
        acquisition.stream(sources.IBM_KEY, io.BytesIO(), 7)
    assert str(failure.value) == "TLS verification of the publisher key " + sources.IBM_KEY + " failed against the host trust store"
    assert mirror not in str(failure.value)
    acquisition.repository_opener = failing
    with pytest.raises(sources.RepositoryTrustFailure) as failure:
        acquisition.stream(mirror + "/repodata/repomd.xml", io.BytesIO(), 7)
    assert str(failure.value) == "TLS verification of the package repository " + mirror + " failed against " + anchor
    acquisition.local = failing
    with pytest.raises(urllib.error.URLError):
        acquisition.stream(package_request["packages"]["sources"][0]["url"] + "/BaseOS/repodata/repomd.xml", io.BytesIO(), 7)


class Constructed(Exception):
    pass


@pytest.mark.parametrize("trust, trust_ref", [(None, None), ("bundle", "repository-ca")])
def test_prepare_builds_the_acquisition_with_the_bound_repository_bundle(package_request, boundary, monkeypatch, trust, trust_ref):
    received = []

    def recording(*args, **kwargs):
        received.append((args, kwargs))
        raise Constructed()

    monkeypatch.setattr(sources, "Acquisition", recording)
    if trust_ref is not None:
        package_request["packages"]["packageRepositoryTrustBundleRef"] = trust_ref
    with pytest.raises(Constructed):
        packages.prepare(package_request, "node-1", "public-certificate", trust=trust, journal=boundary.journal)
    assert received == [((sources.source_for(package_request, "node-1"), "public-certificate"),
                         {"repository": package_request["packageRepository"], "trust": trust, "trust_ref": trust_ref})]
    assert boundary.journal.writes == []


def test_package_scope_admits_only_a_named_repository_trust_bundle(package_request):
    package_request["packages"]["packageRepositoryTrustBundleRef"] = "repository-ca"
    assert sources.source_for(package_request, "node-1")["host"] == "node-1"
    ceph_native.validate_scope(package_request)
    for value in ("", "Bad Name", "a\nb", 5):
        package_request["packages"]["packageRepositoryTrustBundleRef"] = value
        with pytest.raises(ceph_native.Refusal, match="^request$"):
            ceph_native.validate_scope(package_request)
        with pytest.raises(ceph_native.Refusal):
            sources.source_for(package_request, "node-1")
    package_request["packages"]["packageRepositoryTrustBundleRef"] = "repository-ca"
    package_request["packages"]["version"] = "ceph-packages-v2"
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.validate_scope(package_request)


def test_stream_download_enforces_hash_size_budget_and_origin(package_request):
    source = package_request["packages"]["sources"][0]
    acquisition = object.__new__(sources.Acquisition)
    acquisition.repository_base = sources.IBM_BASE
    acquisition.repository_opener = None
    acquisition.source, acquisition.total, acquisition.deadline = source, 0, sources.time.monotonic() + 10

    class Response(io.BytesIO):
        status = 200
        headers = {}

        def geturl(self):
            return source["url"] + "/BaseOS/test.rpm"

    acquisition.local = SimpleNamespace(open=lambda *args, **kwargs: Response(b"payload"))
    url = source["url"] + "/BaseOS/test.rpm"
    output = io.BytesIO()
    assert acquisition.stream(url, output, 7, hashlib.sha256(b"payload").hexdigest(), 7)
    assert output.getvalue() == b"payload"
    for limit, digest, size in [(6, None, None), (7, "a" * 64, 7), (7, None, 8)]:
        with pytest.raises(ceph_native.Refusal):
            acquisition.stream(url, io.BytesIO(), limit, digest, size)
    with pytest.raises(ceph_native.Refusal):
        acquisition.stream("https://foreign.example.test/repo/test.rpm", io.BytesIO(), 7)


def test_root_payload_uses_acquired_hash_without_release_allowlist(tmp_path):
    acquisition = object.__new__(sources.Acquisition)
    acquisition.repository_base = sources.IBM_BASE
    plan = plan_fixture()
    plan["packages"][0]["source"]["sha256"] = "f" * 64
    seen = []
    acquisition.file = lambda *args: seen.append(args)
    acquisition.payloads(str(tmp_path), plan)
    assert seen[0][3] == "f" * 64


def test_fixed_bundle_uses_stdin_and_sanitizes_vendor_output_and_errors():
    source = inspect.getsource(ceph_package_worker)
    assert len(source.encode()) < 32768
    bundle = {name: '\"\"\"Fixed test helper.\"\"\"\n' for name in ceph_package_worker.MODULES}
    bundle["ceph_packages"] += ("\ndef entrypoint(payload):\n print('private vendor output')\n"
                                " return {'changed': False, 'ready': True, 'planDigest': 'd' * 64}\n")
    envelope = {"sources": bundle, "payload": {"private": "private-input"}}
    result = subprocess.run([sys.executable, "-I", "-c", source], input=json.dumps(envelope).encode(), capture_output=True, check=False, timeout=10)
    assert result.returncode == 0 and result.stderr == b""
    assert json.loads(result.stdout) == {"ok": True, "result": {"changed": False, "ready": True, "planDigest": "d" * 64}}
    envelope["sources"]["ceph_packages"] += "\ndef entrypoint(payload):\n raise ValueError(payload['private'])\n"
    failed = subprocess.run([sys.executable, "-I", "-c", source], input=json.dumps(envelope).encode(), capture_output=True, check=False, timeout=10)
    assert json.loads(failed.stdout) == {"ok": False, "reason": "packages"}
    assert b"private-input" not in failed.stdout and failed.stderr == b""


TREEINFO = b"""[general]
family = Red Hat Enterprise Linux
version = 9.8
arch = x86_64
[tree]
arch = x86_64
variants = BaseOS,AppStream
[variant-BaseOS]
repository = BaseOS
[variant-AppStream]
repository = AppStream
"""


def test_repository_selection_proves_actual_publication_pin_and_tree_layout(package_request):
    acquisition = object.__new__(sources.Acquisition)
    acquisition.repository_base = sources.IBM_BASE
    acquisition.source = package_request["packages"]["sources"][0]
    base = acquisition.source["url"]
    documents = {base + "/.bootwright-tree-identity": ("c" * 64 + "\n").encode(), base + "/.treeinfo": TREEINFO}
    acquisition.bytes = lambda url, limit: documents[url]
    seen = []
    acquisition.repository = lambda scratch, identifier, url, signer: seen.append((identifier, url, signer)) or {"id": identifier}
    result = acquisition.repositories("/private/stage")
    assert len(result) == 3
    assert seen[:2] == [("rhel-baseos", base + "/BaseOS", native_signatures.RHEL_SIGNER),
                        ("rhel-appstream", base + "/AppStream", native_signatures.RHEL_SIGNER)]
    assert sources.source_for(package_request, "node-1")["publicationPath"].startswith("/var/lib/bootwright-services/")
    for metadata in (TREEINFO.replace(b"x86_64", b"aarch64"),
                     TREEINFO.replace(b"repository = BaseOS", b"repository = ../foreign")):
        documents[base + "/.treeinfo"] = metadata
        seen.clear()
        with pytest.raises(ceph_native.Refusal):
            acquisition.repositories("/private/stage")
        assert seen == []
    documents[base + "/.treeinfo"] = TREEINFO
    documents[base + "/.bootwright-tree-identity"] = ("f" * 64).encode()
    with pytest.raises(ceph_native.Refusal):
        acquisition.repositories("/private/stage")


def metadata_record(data, expanded_size=None, digest=None):
    checksum = digest or hashlib.sha256(data).hexdigest()
    size = expanded_size if expanded_size is not None else len(data)
    xml = ('<data xmlns="http://linux.duke.edu/metadata/repo" type="primary"><open-checksum type="sha256">' + checksum +
           '</open-checksum><open-size>' + str(size) + '</open-size></data>')
    return sources.ET.fromstring(xml)


def test_metadata_decompression_proves_open_checksum_and_bounds_expansion(tmp_path):
    import gzip

    acquisition = object.__new__(sources.Acquisition)
    acquisition.repository_base = sources.IBM_BASE
    acquisition.expanded, acquisition.deadline = 0, sources.time.monotonic() + 10
    payload = b"<metadata>bounded native input</metadata>"
    compressed = gzip.compress(payload)
    path = tmp_path / "primary.xml.gz"
    path.write_bytes(compressed)
    acquisition.metadata_member(str(path), metadata_record(payload), hashlib.sha256(compressed).hexdigest(), len(compressed))
    assert acquisition.expanded == len(payload)
    for record in (metadata_record(payload, 256 * 1024 * 1024 + 1), metadata_record(payload, len(payload) - 1), metadata_record(payload, digest="f" * 64)):
        with pytest.raises(ceph_native.Refusal):
            acquisition.metadata_member(str(path), record, "a" * 64, len(compressed))
    malicious = b'<!DOCTYPE metadata [<!ENTITY outside SYSTEM "file:///private">]><metadata />'
    path.write_bytes(gzip.compress(malicious))
    with pytest.raises(ceph_native.Refusal):
        acquisition.metadata_member(str(path), metadata_record(malicious), "a" * 64, path.stat().st_size)
    acquisition.expanded = 512 * 1024 * 1024
    with pytest.raises(ceph_native.Refusal, match="package-limit"):
        acquisition.metadata_member(str(path), metadata_record(malicious), "a" * 64, path.stat().st_size)


def test_xz_metadata_is_rejected_before_loading_an_unbounded_dictionary(tmp_path):
    acquisition = object.__new__(sources.Acquisition)
    acquisition.repository_base = sources.IBM_BASE
    acquisition.expanded, acquisition.deadline = 0, sources.time.monotonic() + 10
    path = tmp_path / "primary.xml.xz"
    # No decoder receives an untrusted XZ stream, including its dictionary header.
    path.write_bytes(b"untrusted XZ header")
    with pytest.raises(ceph_native.Refusal, match="packages"):
        acquisition.metadata_member(str(path), metadata_record(b"metadata"), "a" * 64, path.stat().st_size)


def test_remote_worker_deadline_kills_mutating_descendants_and_retains_decision(tmp_path):
    decision, effect = tmp_path / "decision.json", tmp_path / "unexecuted-effect"

    def blocked_transaction():
        decision.write_text("immutable decision", encoding="utf-8")
        descendant = os.fork()
        if descendant == 0:
            signal.signal(signal.SIGTERM, signal.SIG_IGN)
            time.sleep(0.75)
            effect.write_text("unbounded package mutation", encoding="utf-8")
            os._exit(0)
        while True:
            time.sleep(10)

    started = time.monotonic()
    result = ceph_package_worker.supervise(blocked_transaction, budget=0.2, grace=0.05)
    assert result == {"ok": False, "reason": "package-timeout"}
    assert time.monotonic() - started < 2
    time.sleep(0.75)
    assert decision.read_text(encoding="utf-8") == "immutable decision"
    assert not effect.exists()


@pytest.mark.parametrize("interrupt", [signal.SIGHUP, signal.SIGTERM, signal.SIGINT])
def test_remote_worker_startup_interruption_never_leaves_an_unsupervised_effect(tmp_path, interrupt):
    # Isolate the regression: with the original race a default HUP killed the
    # supervisor while its new worker survived long enough to perform this write.
    effect = tmp_path / "unexecuted-effect"
    fixture = """
import json, os, signal, sys, time
namespace = {"__name__": "fixed_worker_fixture"}
exec(sys.stdin.read(), namespace)
original = os.fork
def interrupted_fork():
    child = original()
    if child:
        os.kill(os.getpid(), int(sys.argv[2]))
    return child
os.fork = interrupted_fork
def delayed_effect():
    time.sleep(0.4)
    with open(sys.argv[1], "w", encoding="utf-8") as output:
        output.write("unbounded package mutation")
    return {"ok": True, "result": {}}
print(json.dumps(namespace["supervise"](delayed_effect, budget=0.1, grace=0.01)))
"""
    result = subprocess.run([sys.executable, "-I", "-c", fixture, str(effect), str(int(interrupt))], input=inspect.getsource(ceph_package_worker).encode(),
                            capture_output=True, check=False, timeout=3)
    assert result.returncode == 0 and result.stderr == b""
    assert json.loads(result.stdout) == {"ok": False, "reason": "package-timeout"}
    time.sleep(0.45)
    assert not effect.exists()


def test_fixed_publisher_import_follows_verification_and_before_inventory_check(monkeypatch, tmp_path):
    plan = plan_fixture()
    grant = dict(packages.transaction_request({"cephadm": {"packageVersion": "20.2.1-405.el9cp"}, "ansiblePackageVersion": "5.0.3-1"}),
                 plan=plan, operation="apply", payloads={})
    events = []
    base = SimpleNamespace(close=lambda: events.append("close"))
    observations = iter([{"inventorySHA256": plan["beforeSHA256"]}, {"inventorySHA256": plan["afterSHA256"], "rootsReady": True}])
    monkeypatch.setattr(os, "geteuid", lambda: 0)
    monkeypatch.setattr(native_apply, "verified_payloads", lambda *args: events.append("payloads"))
    monkeypatch.setattr(native_signatures, "verify", lambda *args: events.append("signature"))
    monkeypatch.setattr(native_signatures, "import_ceph_keys", lambda *args: events.append("publisher-import"))
    monkeypatch.setattr(native_resolution, "solve4", lambda *args, **kwargs: (plan, base, None))
    monkeypatch.setattr(native_resolution, "inspect", lambda *args: next(observations))
    monkeypatch.setattr(native_resolution, "snapshot_database", lambda *args: "/snapshot")
    monkeypatch.setattr(native_resolution, "verified_files", lambda *args: True)
    monkeypatch.setattr(native_apply, "run4", lambda *args: events.append("transaction"))
    assert native_apply.apply(grant, str(tmp_path))["changed"] is True
    assert events == ["payloads", "signature", "publisher-import", "transaction", "close"]
    events.clear()
    monkeypatch.setattr(native_resolution, "inspect", lambda *args: {"inventorySHA256": "f" * 64})
    with pytest.raises(ValueError, match="inventory changed"):
        native_apply.apply(grant, str(tmp_path))
    assert "publisher-import" not in events and "transaction" not in events


class RawKey:
    def __init__(self, fingerprint):
        self.fingerprint, self.raw_key = fingerprint, b"raw-" + fingerprint.encode()


def live_key_doubles(monkeypatch, events, results=None):
    files = {native_signatures.publisher_key({"os": "rhel"}): b"redhat-release-keys", "/private/ibm-key": b"ibm-ceph-key"}
    fingerprints = {b"redhat-release-keys": [native_signatures.RHEL_SIGNER, "f" * 40], b"ibm-ceph-key": [native_signatures.IBM_CEPH_SIGNER]}
    results = list(results or [])

    def rawkey2infos(stream):
        info = os.fstat(stream.fileno())
        data = stream.read()
        assert stat.S_ISREG(info.st_mode) and info.st_size == len(data)
        events.append(("parse", data))
        return [RawKey(value) for value in fingerprints[data]]

    class TransactionSet:
        def __init__(self, root):
            assert root == "/"

        def pgpImportPubkey(self, key):
            events.append(("import", key))
            return results.pop(0) if results else 0

        def closeDB(self):
            events.append("close-db")

    dnf = SimpleNamespace(crypto=SimpleNamespace(rawkey2infos=rawkey2infos), yum=SimpleNamespace(misc=SimpleNamespace(procgpgkey=lambda raw: raw)),
                          callback=SimpleNamespace(TransactionProgress=object))
    for name, value in (("dnf", dnf), ("dnf.crypto", dnf.crypto), ("dnf.yum", dnf.yum), ("dnf.yum.misc", dnf.yum.misc), ("dnf.callback", dnf.callback),
                        ("rpm", SimpleNamespace(TransactionSet=TransactionSet))):
        monkeypatch.setitem(sys.modules, name, value)
    monkeypatch.setattr(native_signatures, "read_key", lambda path: files[path])
    monkeypatch.setattr(os, "geteuid", lambda: 0)


def live_key_grant(inherited_policy=True):
    request = packages.transaction_request({"cephadm": {"packageVersion": "20.2.1-405.el9cp"}, "ansiblePackageVersion": "5.0.3-1"})
    if not inherited_policy:
        del request["versions"]["inheritedPolicy"]
    grant = dict(request, plan=plan_fixture(grant=copy.deepcopy(request)), operation="apply", payloads={})
    grant["publisherKeys"] = {native_signatures.IBM_CEPH_SIGNER: "/private/ibm-key"}
    return grant


def test_live_rpmdb_gains_exactly_each_plan_signers_pinned_key_from_a_real_descriptor(monkeypatch):
    events = []
    live_key_doubles(monkeypatch, events)
    native_signatures.import_ceph_keys(live_key_grant())
    assert events == [("parse", b"ibm-ceph-key"), ("parse", b"redhat-release-keys"), ("import", b"raw-" + native_signatures.IBM_CEPH_SIGNER.encode()),
                      "close-db", ("import", b"raw-" + native_signatures.RHEL_SIGNER.encode()), "close-db"]
    events.clear()
    native_signatures.import_ceph_keys(live_key_grant(inherited_policy=False))
    assert events == [("parse", b"ibm-ceph-key"), ("import", b"raw-" + native_signatures.IBM_CEPH_SIGNER.encode()), "close-db"]
    events.clear()
    live_key_doubles(monkeypatch, events, results=[0, 1])
    with pytest.raises(ValueError, match="key import"):
        native_signatures.import_ceph_keys(live_key_grant())
    events.clear()
    grant = live_key_grant()
    grant["plan"]["packages"][0]["signer"] = "e" * 40
    with pytest.raises(ValueError):
        native_signatures.import_ceph_keys(grant)
    assert not [event for event in events if event[0] == "import"]
    for change in ({"policy": "unknown"}, {"publisherKeys": {}}):
        with pytest.raises(ValueError):
            native_signatures.import_ceph_keys(dict(live_key_grant(), **change))
    monkeypatch.setattr(os, "geteuid", lambda: 1000)
    with pytest.raises(ValueError, match="import authority"):
        native_signatures.import_ceph_keys(live_key_grant())
    assert not [event for event in events if event[0] == "import"]


@pytest.mark.parametrize("signature", [0, 1])
def test_live_signature_check_follows_every_signer_import_and_still_refuses_a_failure(monkeypatch, tmp_path, signature):
    events = []
    live_key_doubles(monkeypatch, events)
    grant = live_key_grant()
    plan = grant["plan"]
    observations = iter([{"inventorySHA256": plan["beforeSHA256"]}, {"inventorySHA256": plan["afterSHA256"], "rootsReady": True}])

    def check(package):
        events.append(("check", package))
        return signature, "public key not installed"
    base = SimpleNamespace(conf=SimpleNamespace(), transaction=SimpleNamespace(install_set=["cephadm", "ceph-common"]), package_signature_check=check,
                           do_transaction=lambda progress: events.append("transaction"), close=lambda: events.append("close"))
    monkeypatch.setattr(native_apply, "verified_payloads", lambda *args: None)
    monkeypatch.setattr(native_signatures, "verify", lambda *args: events.append("isolated"))
    monkeypatch.setattr(native_resolution, "solve4", lambda *args, **kwargs: (plan, base, None))
    monkeypatch.setattr(native_resolution, "inspect", lambda *args: next(observations))
    monkeypatch.setattr(native_resolution, "snapshot_database", lambda *args: "/snapshot")
    monkeypatch.setattr(native_resolution, "verified_files", lambda *args: True)
    if signature:
        with pytest.raises(ValueError, match="native package signature"):
            native_apply.apply(grant, str(tmp_path))
    else:
        assert native_apply.apply(grant, str(tmp_path))["changed"] is True
    kinds = [event if isinstance(event, str) else event[0] for event in events]
    assert kinds[:kinds.index("check")] == ["isolated", "parse", "parse", "import", "close-db", "import", "close-db"]
    assert base.conf.gpgcheck is True and base.conf.localpkg_gpgcheck is True
    assert ("transaction" in kinds) is not bool(signature) and kinds[-1] == "close"


@pytest.mark.parametrize("release,machine,valid",
                         [(b'ID=rhel\nVERSION_ID="9.8"\n', "x86_64", True), (b'ID=rhel\nVERSION_ID="9.7"\n', "x86_64", True),
                          (b'ID=fedora\nVERSION_ID=43\n', "x86_64", True), (b'ID=rhel\nVERSION_ID="9.8"\n', "aarch64", False),
                          (b'ID=rhel\nID=rhel\nVERSION_ID="9.8"\n', "x86_64", False)])
def test_actual_guest_platform_is_proved_read_only(release, machine, valid, monkeypatch):
    monkeypatch.setattr(os.path, "islink", lambda path: False)
    monkeypatch.setattr(ceph_native, "read_regular", lambda *args, **kwargs: release)
    monkeypatch.setattr(os, "uname", lambda: SimpleNamespace(machine=machine))
    if valid:
        ACTUAL_PLATFORM()
    else:
        with pytest.raises(ceph_native.Refusal, match="package-platform"):
            ACTUAL_PLATFORM()


@pytest.mark.parametrize("reason", ["package-platform", "package-host", "foreign-cluster"])
def test_drift_or_foreign_host_refuses_before_any_package_effects(package_request, boundary, monkeypatch, reason):
    def refuse(value):
        raise ceph_native.Refusal(reason)

    monkeypatch.setattr(packages, "host_admission", refuse)
    with pytest.raises(ceph_native.Refusal, match=reason):
        prepare(package_request, boundary)
    assert boundary.acquisition.calls == [] and boundary.journal.writes == []
    assert "transaction" not in boundary.events


@pytest.mark.parametrize("entry,symlink", [("foreign-fsid", False), ("osd", True)])
def test_unknown_or_linked_native_data_cannot_authorize_packages(package_request, tmp_path, entry, symlink):
    path = tmp_path / entry
    if symlink:
        path.symlink_to(tmp_path, target_is_directory=True)
    else:
        path.mkdir(mode=0o700)
    descriptor = os.open(tmp_path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        with pytest.raises(ceph_native.Refusal):
            packages.ceph_directory(descriptor, package_request, False)
    finally:
        os.close(descriptor)


@pytest.fixture
def inherited_boundary(package_request, boundary, monkeypatch):
    plan = boundary.plan

    def prove(record, query):
        assert boundary.journal.records["verified.json"]["policy"] == record
        boundary.events.append("policy-installed")

    def apply(grant, scratch):
        assert grant["versions"]["inheritedPolicy"] == "latest"
        assert set(boundary.journal.records["verified.json"]["policy"]["files"]) == policy.FILES
        boundary.events.append("transaction")
        boundary.state.update(inventorySHA256=plan["afterSHA256"], rootsReady=True)
        return {"changed": True, "planDigest": plan["digest"], "afterSHA256": plan["afterSHA256"]}
    monkeypatch.setattr(policy, "installed", prove)
    monkeypatch.setattr(native_apply, "apply", apply)
    return boundary


def test_ordinary_transaction_adds_authenticated_inherited_policy_roots_before_transaction(package_request, inherited_boundary):
    b = inherited_boundary
    result = prepare(package_request, b)
    assert result["ready"] and b.events == ["payload-integrity", "publisher-verification", "transaction", "policy-installed"]
    assert b.journal.writes == ["decision.json", "verified.json", "complete.json"]
    assert packages.transaction_request({"cephadm": {"packageVersion": "20.2.1-405.el9cp"}, "ansiblePackageVersion": "5.0.3-1"})["versions"] == {
        "ceph": "20.2.1-405.el9cp", "ansible": "5.0.3-1", "license": "latest", "inheritedPolicy": "latest"}
    assert [p["name"] for p in b.plan["packages"] if p["signer"] == native_signatures.RHEL_SIGNER] == ["crypto-policies", "crypto-policies-scripts"]
    again = prepare(package_request, b)
    assert again == dict(result, changed=False) and b.events.count("transaction") == 1
    assert b.events.count("policy-installed") == 2


@pytest.mark.parametrize("damage", ["signer", "profile-header", "postcondition"])
def test_inherited_policy_authority_failure_cannot_publish_completed_packages(package_request, inherited_boundary, monkeypatch, damage):
    b = inherited_boundary
    if damage == "signer":
        b.plan["packages"][-1]["signer"] = native_signatures.IBM_CEPH_SIGNER
        b.plan["digest"] = native_resolution.digest({key: value for key, value in b.plan.items() if key != "digest"})
    elif damage == "profile-header":
        monkeypatch.setattr(packages, "rpm_query", lambda argv: b"algo|8\n")
    else:
        monkeypatch.setattr(policy, "installed", lambda *args: (unused for unused in ()).throw(ceph_native.Refusal("native-tunnel-unproved")))
    with pytest.raises(ceph_native.Refusal):
        prepare(package_request, b)
    assert "complete.json" not in b.journal.records
    if damage != "postcondition":
        assert "transaction" not in b.events


@pytest.mark.parametrize("damage", ["missing-policy", "wrong-header", "wrong-source", "wrong-host", "wrong-version"])
def test_completed_replay_reproves_the_original_inherited_policy_record(package_request, inherited_boundary, monkeypatch, damage):
    b = inherited_boundary
    prepare(package_request, b)
    verified = b.journal.records["verified.json"]
    if damage == "missing-policy":
        del verified["policy"]
    elif damage == "wrong-header":
        verified["policy"]["packages"][0]["headerSHA256"] = "f" * 64
    elif damage == "wrong-source":
        verified["policy"]["scope"]["source"]["mediaSHA256"] = "f" * 64
    elif damage == "wrong-host":
        verified["policy"]["scope"]["host"] = "foreign"
    else:
        verified["policy"]["version"] = "ceph-crypto-policy-v0"
    writes, events = list(b.journal.writes), list(b.events)
    monkeypatch.setattr(policy, "installed", lambda *args: pytest.fail("a changed policy record reached the installed files"))
    with pytest.raises(ceph_native.Refusal):
        prepare(package_request, b)
    assert b.journal.writes == writes and b.events == events


def test_native_package_failure_keeps_selection_and_native_cause():
    with pytest.raises(ceph_native.NativeFailure) as caught:
        with packages.phase("package-resolution", "cephadm-ansible-not a version"):
            raise ValueError("No match for argument: cephadm-ansible-not a version; password=synthetic-secret")
    assert caught.value.native_error["coordinate"] == "cephadm-ansible-not a version"
    assert "No match for argument" in caught.value.native_error["cause"]
    assert "synthetic-secret" not in str(caught.value.native_error)


@pytest.mark.parametrize("name", ["cephadm", "ceph-common", "cephadm-ansible", "nfs-ganesha"])
def test_selected_older_root_reaches_native_transaction_and_frozen_retry(package_request, boundary, name):
    if name == "nfs-ganesha":
        package_request["nfsGaneshaPackageVersion"] = "7.3-9"
        boundary.plan.clear()
        boundary.plan.update(plan_fixture(grant=packages.transaction_request(package_request)))
    action = next(row for row in boundary.plan["actions"] if row["after"]["name"] == name)
    action["kind"] = "downgrade"
    action["before"] = dict(action["after"], version="999")
    boundary.plan["digest"] = native_resolution.digest({key: value for key, value in boundary.plan.items() if key != "digest"})
    assert prepare(package_request, boundary)["changed"] is True
    assert prepare(package_request, boundary)["changed"] is False
    assert boundary.events.count("transaction") == 1
    assert boundary.acquisition.calls == ["metadata", "key", "payloads"]


def test_unrequested_host_policy_downgrade_refuses_before_effects(package_request, boundary):
    action = next(row for row in boundary.plan["actions"] if row["after"]["name"] == "crypto-policies")
    action["kind"] = "downgrade"
    action["before"] = dict(action["after"], version="999")
    boundary.plan["digest"] = native_resolution.digest({key: value for key, value in boundary.plan.items() if key != "digest"})
    with pytest.raises(ceph_native.Refusal, match="package-journal"):
        prepare(package_request, boundary)
    assert boundary.events == [] and boundary.journal.writes == []


def test_package_source_refuses_a_request_carrying_an_action(package_request):
    assert sources.source_for(package_request, "node-1")["host"] == "node-1"
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        sources.source_for(dict(package_request, action="cluster"), "node-1")
