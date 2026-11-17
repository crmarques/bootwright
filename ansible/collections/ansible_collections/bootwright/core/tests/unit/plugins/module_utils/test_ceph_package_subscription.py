"""A subscription-sourced Ceph host takes BaseOS and AppStream from its own Satellite repositories, through the lent CA bundle alone.

The loopback acquisition with a client certificate is in
tests/unit/test_ceph_package_subscription_acquisition.py, which needs cryptography.
"""

from __future__ import annotations

import copy
import hashlib
import io
import ssl
import stat
import urllib.error
from types import SimpleNamespace

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_crypto_policy as policy, ceph_native
from ansible_collections.bootwright.core.plugins.module_utils import ceph_package_sources as sources, native_signatures
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils.test_ceph_package_sources import FILELISTS, PRIMARY
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils.test_ceph_packages import package_request  # noqa: F401 pylint: disable=unused-import

SATELLITE = "https://satellite.example.test/pulp/content"
RELEASE = SATELLITE + "/Default_Organization/Library/content/dist/rhel9/9.8/x86_64"
MIRROR = "https://mirror.example.test/ceph"


def subscribed(request, hosts=None):
    for source in request["packages"]["sources"]:
        if hosts is not None and source["host"] not in hosts:
            continue
        for key in ("media", "mediaSHA256", "mediaSize", "publicationPath", "url"):
            del source[key]
        source["certificateRef"] = "satellite-ca"
        source["subscription"] = {"hostname": "satellite.example.test", "contentBaseURL": SATELLITE}
    return request


def record(kind, **changes):
    value = {"enabled": True, "baseurl": [RELEASE + "/" + kind + "/os"], "sslverify": True,
             "sslclientcert": "/etc/pki/entitlement/1234.pem", "sslclientkey": "/etc/pki/entitlement/1234-key.pem"}
    value.update(changes)
    return value


def owned(path, mode=0o644, uid=0):
    if path == sources.ENTITLEMENT_DIRECTORY:
        return SimpleNamespace(st_mode=stat.S_IFDIR | 0o755, st_uid=0)
    return SimpleNamespace(st_mode=stat.S_IFREG | mode, st_uid=uid)


def test_source_for_admits_the_tree_and_the_subscription_shapes_and_mixed_clusters(package_request):
    tree = sources.source_for(copy.deepcopy(package_request), "node-1")
    assert tree["url"] == "https://192.0.2.10:8443/os/rhel/tree"
    mixed = subscribed(copy.deepcopy(package_request), {"node-2"})
    assert "url" in sources.source_for(mixed, "node-1")
    frozen = {"host": "node-2", "profile": "rhel", "certificateRef": "satellite-ca",
              "subscription": {"hostname": "satellite.example.test", "contentBaseURL": SATELLITE}}
    assert sources.source_for(mixed, "node-2") == frozen
    for hostname in ("192.0.2.20", "2001:db8::20", "satellite"):
        request = subscribed(copy.deepcopy(package_request))
        request["packages"]["sources"][0]["subscription"]["hostname"] = hostname
        assert sources.source_for(request, "node-1")["subscription"]["hostname"] == hostname
    assert policy.source_root(sources.source_for(mixed, "node-2")) == SATELLITE and policy.source_root(tree) == tree["url"]


def without(key):
    return lambda source: source.pop(key)


def subscription(key, value):
    return lambda source: source["subscription"].__setitem__(key, value)


@pytest.mark.parametrize("edit", [
    lambda source: source.__setitem__("media", "rhel-dvd.iso"),
    lambda source: source.__setitem__("url", SATELLITE),
    without("certificateRef"),
    lambda source: source.__setitem__("subscription", {"hostname": "satellite.example.test"}),
    lambda source: source["subscription"].__setitem__("port", 443),
    lambda source: source.__setitem__("subscription", "satellite.example.test"),
    subscription("hostname", ""),
    subscription("hostname", "satellite_one.example.test"),
    subscription("hostname", "-satellite.example.test"),
    subscription("hostname", "satellite.example.test."),
    subscription("hostname", "a" * 254),
    subscription("hostname", "https://satellite.example.test"),
    subscription("contentBaseURL", "http://satellite.example.test/pulp/content"),
    subscription("contentBaseURL", "https://operator@satellite.example.test/pulp/content"),
    subscription("contentBaseURL", "https://satellite.example.test/pulp/content?view=1"),
    subscription("contentBaseURL", "https://satellite.example.test/pulp/content?"),
    subscription("contentBaseURL", "https://satellite.example.test/pulp/content#root"),
    subscription("contentBaseURL", "https://satellite.example.test/pulp/../content"),
    subscription("contentBaseURL", 7),
], ids=["media", "url", "no-certificate", "no-content-root", "extra-member", "not-a-record", "empty-host", "underscore-host", "hyphen-host",
        "trailing-dot", "long-host", "host-url", "http", "user-information", "query", "empty-query", "fragment", "traversal", "not-text"])
def test_source_for_refuses_every_other_subscription_shape(package_request, edit):
    request = subscribed(package_request)
    edit(request["packages"]["sources"][0])
    with pytest.raises(ceph_native.Refusal, match="^(packages|request)$"):
        sources.source_for(request, "node-1")


def test_selection_takes_the_one_baseos_and_appstream_repository_under_the_content_root():
    records = [record("baseos"), record("appstream"), record("baseos", enabled=False, baseurl=[RELEASE.replace("9.8", "9") + "/baseos/os"]),
               record("codeready-builder"), record("baseos", baseurl=[RELEASE + "/baseos/debug"]),
               record("appstream", baseurl=["https://cdn.redhat.com/content/dist/rhel9/9.8/x86_64/appstream/os"])]
    selected = sources.subscription_repositories(records, SATELLITE, owned)
    assert selected == {"baseos": {"url": RELEASE + "/baseos/os", "certificate": "/etc/pki/entitlement/1234.pem", "key": "/etc/pki/entitlement/1234-key.pem"},
                        "appstream": {"url": RELEASE + "/appstream/os", "certificate": "/etc/pki/entitlement/1234.pem",
                                      "key": "/etc/pki/entitlement/1234-key.pem"}}


def other_release(kind):
    return record(kind, baseurl=[RELEASE.replace("dist", "eus") + "/" + kind + "/os"])


@pytest.mark.parametrize("records,inspect", [
    ([record("appstream")], owned),
    ([record("baseos"), other_release("baseos"), record("appstream")], owned),
    ([record("baseos", baseurl=["https://other.example.test/pulp/content/dist/baseos/os"]), record("appstream")], owned),
    ([record("baseos", baseurl=[SATELLITE + "-other/dist/baseos/os"]), record("appstream")], owned),
    ([record("baseos", sslclientcert="/etc/pki/other/1234.pem"), record("appstream")], owned),
    ([record("baseos", sslclientkey="/etc/pki/entitlement/../1234-key.pem"), record("appstream")], owned),
    ([record("baseos", sslclientkey="/etc/pki/entitlement/keys/1234-key.pem"), record("appstream")], owned),
    ([record("baseos", sslclientkey=""), record("appstream")], owned),
    ([record("baseos", sslverify=False), record("appstream")], owned),
    ([record("baseos", enabled=False), record("appstream")], owned),
    ([record("baseos", baseurl=[RELEASE + "/baseos/os", RELEASE + "/baseos/os"]), record("appstream")], owned),
    ([record("baseos"), record("appstream")], lambda path: owned(path, mode=0o664)),
    ([record("baseos"), record("appstream")], lambda path: owned(path, uid=1000)),
    ([record("baseos"), record("appstream")], lambda path: owned(path) if path == sources.ENTITLEMENT_DIRECTORY else
     SimpleNamespace(st_mode=stat.S_IFLNK | 0o777, st_uid=0)),
], ids=["no-baseos", "two-baseos", "outside-content-root", "content-root-prefix", "certificate-outside", "key-traversal", "key-nested", "no-key",
        "verification-off", "disabled", "two-base-urls", "group-writable", "not-root-owned", "symlink"])
def test_selection_refuses_without_exactly_one_qualifying_repository_of_each(records, inspect):
    with pytest.raises(ceph_native.Refusal, match="^packages$"):
        sources.subscription_repositories(records, SATELLITE, inspect)


class Context:
    def __init__(self, bundle=None):
        self.bundle, self.minimum_version, self.chains = bundle, None, []

    def load_cert_chain(self, certificate, key):
        self.chains.append((certificate, key))


def acquisition(request, monkeypatch, reader=None, inspect=owned):
    contexts, openers = [], []
    monkeypatch.setattr(sources.tls_trust, "context", lambda bundle: (contexts.append(Context(bundle)) or contexts[-1], None))
    monkeypatch.setattr(sources.ssl, "create_default_context", lambda **kwargs: Context())
    monkeypatch.setattr(sources.urllib.request, "build_opener", lambda *handlers: openers.append(handlers) or SimpleNamespace(handlers=handlers))
    source = sources.source_for(subscribed(copy.deepcopy(request)), "node-1")
    value = sources.Acquisition(source, "satellite-bundle", repository=MIRROR, repository_key=MIRROR + "/ibm.asc",
                                reader=reader or (lambda: [record("baseos"), record("appstream")]), inspect=inspect)
    return value, contexts, openers


def test_each_satellite_opener_presents_its_entitlement_and_trusts_only_the_bundle(package_request, monkeypatch):
    calls = []
    value, contexts, openers = acquisition(package_request, monkeypatch, reader=lambda: calls.append(1) or [record("baseos"), record("appstream")])
    contexts.clear()
    openers.clear()
    selected = value.satellite_repositories()
    assert value.satellite_repositories() is selected and calls == [1]
    assert [context.bundle for context in contexts] == ["satellite-bundle", "satellite-bundle"]
    assert all(context.minimum_version == ssl.TLSVersion.TLSv1_2 for context in contexts)
    assert [context.chains for context in contexts] == [[("/etc/pki/entitlement/1234.pem", "/etc/pki/entitlement/1234-key.pem")]] * 2
    for handlers, context in zip(openers, contexts):
        proxy, redirect, https = handlers
        assert proxy.proxies == {} and isinstance(redirect, sources.NoRedirect) and https._context is context  # pylint: disable=protected-access
        assert not isinstance(https, sources.RepositoryHTTPS)


def test_an_unreadable_repository_file_or_entitlement_refuses_without_host_text(package_request, monkeypatch):
    def broken():
        raise RuntimeError("Parsing file /etc/yum.repos.d/redhat.repo failed")

    value, _contexts, _openers = acquisition(package_request, monkeypatch, reader=broken)
    with pytest.raises(ceph_native.Refusal, match="^packages$"):
        value.satellite_repositories()
    for resumed in (lambda: value.publisher_key("/nonexistent"), lambda: value.payloads("/nonexistent", {"packages": []})):
        monkeypatch.setattr(value, "stream", lambda *arguments, **options: pytest.fail("a resume downloaded before it read the host's repositories"))
        with pytest.raises(ceph_native.Refusal, match="^packages$"):
            resumed()
    value, contexts, _openers = acquisition(package_request, monkeypatch)

    def mismatched(self, certificate, key):
        raise ssl.SSLError("key values mismatch")

    monkeypatch.setattr(Context, "load_cert_chain", mismatched)
    with pytest.raises(ceph_native.Refusal, match="^packages$"):
        value.satellite_repositories()
    assert contexts


@pytest.mark.parametrize("url", [SATELLITE + "x/dist/baseos/os/repodata/repomd.xml", "https://other.example.test/pulp/content/x.rpm",
                                 "https://192.0.2.10:8443/os/rhel/tree/BaseOS/repodata/repomd.xml", MIRROR + "x/repodata/repomd.xml",
                                 sources.IBM_BASE + "/repodata/repomd.xml"])
def test_stream_refuses_a_url_outside_the_content_root_and_the_ibm_repository_before_reading_the_host(package_request, monkeypatch, url):
    calls = []
    value, _contexts, _openers = acquisition(package_request, monkeypatch, reader=lambda: calls.append(1) or [])
    with pytest.raises(ceph_native.Refusal, match="^packages$"):
        value.stream(url, io.BytesIO(), 7)
    assert calls == []


def test_stream_refuses_a_content_root_url_outside_the_selected_repositories(package_request, monkeypatch):
    value, _contexts, _openers = acquisition(package_request, monkeypatch)
    with pytest.raises(ceph_native.Refusal, match="^packages$"):
        value.stream(RELEASE + "/codeready-builder/os/repodata/repomd.xml", io.BytesIO(), 7)


def test_a_satellite_certificate_the_bundle_does_not_verify_names_the_satellite_and_the_secret(package_request, monkeypatch):
    value, _contexts, _openers = acquisition(package_request, monkeypatch)

    def unverified(request, timeout):
        raise urllib.error.URLError(ssl.SSLCertVerificationError("unable to get local issuer certificate"))

    for repository in value.satellite_repositories().values():
        repository["opener"] = SimpleNamespace(open=unverified)
    with pytest.raises(sources.RepositoryTrustFailure) as failure:
        value.stream(RELEASE + "/baseos/os/repodata/repomd.xml", io.BytesIO(), 7)
    assert str(failure.value) == ("TLS verification of the Satellite satellite.example.test content root " + SATELLITE
                                  + " failed against CA bundle Secret satellite-ca")
    assert failure.value.__cause__ is None and "entitlement" not in str(failure.value)


def repomd(base, served):
    rows = []
    for kind, data in (("primary", PRIMARY), ("filelists", FILELISTS)):
        digest = hashlib.sha256(data).hexdigest()
        href = "repodata/%s-%s.xml" % (digest, kind)
        served[base + "/" + href] = data
        rows.append('<data type="%s"><checksum type="sha256">%s</checksum><location href="%s"/><size>%d</size></data>' % (kind, digest, href, len(data)))
    served[base + "/repodata/repomd.xml"] = ('<?xml version="1.0" encoding="UTF-8"?>\n<repomd xmlns="http://linux.duke.edu/metadata/repo">'
                                             + "".join(rows) + "</repomd>\n").encode()


class Served:
    def __init__(self, name, served, seen):
        self.name, self.served, self.seen = name, served, seen

    def open(self, request, timeout):
        self.seen.append((self.name, request.full_url))
        response = io.BytesIO(self.served[request.full_url])
        response.status, response.headers, response.geturl = 200, {}, lambda: request.full_url
        return response


def test_repositories_return_the_satellite_baseos_appstream_and_the_ibm_repository(package_request, monkeypatch, tmp_path):
    served, seen = {}, []
    for base in (RELEASE + "/baseos/os", RELEASE + "/appstream/os", MIRROR):
        repomd(base, served)
    value, _contexts, _openers = acquisition(package_request, monkeypatch)
    value.repository_opener = Served("ibm", served, seen)
    for kind, repository in value.satellite_repositories().items():
        repository["opener"] = Served(kind, served, seen)
    repositories = value.repositories(str(tmp_path))
    assert [(r["id"], r["baseURL"], r["signer"]) for r in repositories] == [
        ("rhel-baseos", RELEASE + "/baseos/os", native_signatures.RHEL_SIGNER),
        ("rhel-appstream", RELEASE + "/appstream/os", native_signatures.RHEL_SIGNER),
        ("ibm-storage-ceph", MIRROR, native_signatures.IBM_CEPH_SIGNER)]
    assert {name for name, url in seen if url.startswith(RELEASE + "/baseos/os/")} == {"baseos"}
    assert {name for name, url in seen if url.startswith(RELEASE + "/appstream/os/")} == {"appstream"}
    assert {name for name, url in seen if url.startswith(MIRROR + "/")} == {"ibm"} and len(seen) == 9
