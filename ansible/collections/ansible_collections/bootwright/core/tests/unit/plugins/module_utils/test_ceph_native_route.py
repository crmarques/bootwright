"""A proxied cluster's hosts route podman's own image pulls through the frozen registryProxy (B1600).

Root metadata is injected over a temporary filesystem, as in test_ceph_registry; nothing here runs podman.
"""

from __future__ import annotations

import contextlib
import hashlib
import os
from types import SimpleNamespace

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native, ceph_native_registry, ceph_native_stretch
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_registry as registry_cases

cluster_request = registry_cases.cluster_request
registry = registry_cases.registry

PROXY = "http://proxy.example.test:3128"


def expected(endpoint):
    return ("# Bootwright: this host's native image pulls leave through the Ceph cluster's frozen registryProxy.\n"
            "[engine]\n"
            'env = ["HTTPS_PROXY=' + endpoint + '", "HTTP_PROXY=' + endpoint + '"]\n'
            "\n"
            "[containers]\n"
            "http_proxy = false\n").encode()


@pytest.fixture
def routed(registry, tmp_path, monkeypatch):
    native, unused, material = registry
    parent = tmp_path / "root" / "etc" / "containers"
    parent.mkdir(mode=0o755)
    monkeypatch.setattr(ceph_native_registry, "ROUTE_PARENT", str(parent))
    monkeypatch.setattr(ceph_native_registry, "ROUTE_DIRECTORY", str(parent / "containers.conf.d"))
    native.request["registryProxy"] = PROXY
    return native, parent / "containers.conf.d", material


def path_of(directory, native):
    return directory / ceph_native_registry.route_name(native.request)


@pytest.mark.parametrize("proxy,endpoint", [
    (PROXY, PROXY),
    ("http://Proxy.Example.Test:8080", "http://proxy.example.test:8080"),
    ("http://proxy.example.test", "http://proxy.example.test"),
    ("http://192.0.2.9:3128", "http://192.0.2.9:3128"),
    ("http://[2001:db8:0::1]:3128", "http://[2001:db8::1]:3128"),
    ("http://proxy_x.example.test.:3128", "http://proxy_x.example.test.:3128"),
], ids=["name", "case", "no-port", "ipv4", "ipv6", "underscore-and-root-dot"])
def test_the_route_is_rebuilt_from_the_frozen_proxy_parts(cluster_request, proxy, endpoint):
    cluster_request["registryProxy"] = proxy
    record, content = ceph_native_registry.route_record(cluster_request)
    assert content == expected(endpoint)
    token = hashlib.sha256((cluster_request["fsid"] + "\x00" + cluster_request["owner"]).encode()).hexdigest()
    assert record == {"version": "ceph-native-route-v1", "name": "90-bootwright-ceph-" + token + ".conf", "sha256": hashlib.sha256(content).hexdigest()}


@pytest.mark.parametrize("proxy", ['http://proxy"x.example.test:3128', "http://proxy~x.example.test:3128", "https://proxy.example.test:3128",
                                   "http://user@proxy.example.test:3128"])
def test_a_proxy_whose_parts_would_not_rebuild_exactly_refuses(cluster_request, proxy):
    cluster_request["registryProxy"] = proxy
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native_registry.route_record(cluster_request)


def test_a_direct_route_keeps_no_record_and_no_file(routed):
    native, directory, unused = routed
    del native.request["registryProxy"]
    assert native.prepare_route() is False
    ceph_native_registry.Registry(native).prove_route()
    assert native.markers.writes == [] and not directory.exists()


def test_the_route_record_is_durable_before_the_file_and_the_publication_repeats_as_a_proof(routed):
    native, directory, unused = routed
    seen = []
    native.markers.before = lambda name, value: seen.append((name, path_of(directory, native).exists()))
    assert native.prepare_route() is True
    assert seen == [(ceph_native_registry.ROUTE_MARKER, False)]
    assert path_of(directory, native).read_bytes() == expected(PROXY)
    assert path_of(directory, native).stat().st_mode & 0o777 == 0o600 and directory.stat().st_mode & 0o777 == 0o755
    assert native.prepare_route() is False and native.markers.writes == [ceph_native_registry.ROUTE_MARKER]
    assert sorted(os.listdir(directory)) == [ceph_native_registry.route_name(native.request)]


def test_the_registry_proof_carries_the_route(routed):
    native, directory, material = routed
    native.prepare_route()
    assert native.prepare_registry(material)
    native.prove_registry(material)
    path_of(directory, native).unlink()
    with pytest.raises(ceph_native.Refusal, match="^registry-ownership$"):
        native.prove_registry(material)


@pytest.mark.parametrize("parent", ["writable", "absent"])
def test_an_unprotected_or_missing_podman_directory_refuses_before_any_record(routed, parent):
    native, directory, unused = routed
    if parent == "writable":
        directory.parent.chmod(0o775)
    else:
        directory.parent.rmdir()
    with pytest.raises(ceph_native.Refusal, match="^registry-ownership$"):
        native.prepare_route()
    assert native.markers.writes == [] and not directory.exists()


@pytest.mark.parametrize("damage", ["bytes", "mode", "link", "directory-mode", "record", "unproxied"])
def test_a_route_this_cluster_does_not_hold_exactly_refuses(routed, damage):
    native, directory, unused = routed
    native.prepare_route()
    path = path_of(directory, native)
    if damage == "bytes":
        path.write_bytes(expected("http://other.example.test:3128"))
    elif damage == "mode":
        path.chmod(0o620)
    elif damage == "link":
        path.unlink()
        os.symlink("/dev/null", path)
    elif damage == "directory-mode":
        directory.chmod(0o775)
    elif damage == "record":
        native.markers.records[ceph_native_registry.ROUTE_MARKER]["sha256"] = "0" * 64
    else:
        del native.request["registryProxy"]
    with pytest.raises(ceph_native.Refusal, match="^registry-ownership$"):
        ceph_native_registry.Registry(native).prove_route()
    with pytest.raises(ceph_native.Refusal, match="^registry-ownership$"):
        native.prepare_route()


def test_a_foreign_file_under_this_clusters_name_is_never_adopted(routed):
    native, directory, unused = routed
    directory.mkdir(mode=0o755)
    path_of(directory, native).write_bytes(expected(PROXY))
    with pytest.raises(ceph_native.Refusal, match="^registry-ownership$"):
        native.prepare_route()
    assert native.markers.writes == [] and path_of(directory, native).read_bytes() == expected(PROXY)


def tiebreaker(native):
    return SimpleNamespace(request=native.request, markers=native.markers)


def test_the_tiebreaker_footprint_names_and_removes_only_its_own_route(routed):
    native, directory, unused = routed
    removal = tiebreaker(native)
    assert ceph_native_stretch.TiebreakerRemoval.route_present(removal) is False
    native.prepare_route()
    assert ceph_native_stretch.TiebreakerRemoval.route_present(removal) is True
    assert ceph_native_stretch.TiebreakerRemoval.route_present(removal, remove=True) is False
    assert not path_of(directory, native).exists() and ceph_native_stretch.TiebreakerRemoval.route_present(removal) is False


def test_the_tiebreaker_footprint_refuses_a_route_no_record_binds(routed):
    native, directory, unused = routed
    directory.mkdir(mode=0o755)
    path_of(directory, native).write_bytes(expected(PROXY))
    with pytest.raises(ceph_native.Refusal, match="^tiebreaker-foreign$"):
        ceph_native_stretch.TiebreakerRemoval.route_present(tiebreaker(native), remove=True)
    assert path_of(directory, native).exists()


def test_an_unclaimed_host_holding_this_clusters_route_is_traced(routed, monkeypatch):
    native, directory, unused = routed
    # The trace walks from /, which the registry fixture roots in the temporary tree, so the route keeps its fixed absolute name.
    monkeypatch.setattr(ceph_native_registry, "ROUTE_DIRECTORY", "/etc/containers/containers.conf.d")
    removal = SimpleNamespace(request=native.request, fsid=native.request["fsid"], keys=lambda value: contextlib.nullcontext((None, None, b"")))
    assert ceph_native_stretch.TiebreakerRemoval.traced(removal) is False
    directory.mkdir(mode=0o755)
    path_of(directory, native).write_bytes(expected(PROXY))
    assert ceph_native_stretch.TiebreakerRemoval.traced(removal) is True


@pytest.mark.parametrize("direct", [False, True])
def test_another_bootwright_podman_route_refuses_before_publication(routed, direct):
    native, directory, unused = routed
    if direct:
        del native.request["registryProxy"]
    directory.mkdir()
    (directory / ("90-bootwright-ceph-" + "0" * 64 + ".conf")).write_text("other cluster owns this")
    with pytest.raises(ceph_native.Refusal, match="^registry-ownership$"):
        native.prepare_route()
    assert native.markers.writes == []
