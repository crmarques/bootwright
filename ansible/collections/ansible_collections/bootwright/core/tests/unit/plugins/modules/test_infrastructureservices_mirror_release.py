"""The release is read back from the mirror's own layers, and the copy's oc is verified before anything is lent to it."""

import hashlib
import io
import json
import os
import stat
import tarfile
from urllib.error import URLError

import pytest

from ansible_collections.bootwright.core.plugins.modules import infrastructureservices_mirror_release as mirror_release

MIRROR = "203.0.113.20:5000"
RELEASE = {"components": mirror_release.COMPONENTS, "repository": mirror_release.RELEASE, "tag": "4.21.15-x86_64", "version": "4.21.15"}
CLI, INSTALLER = "sha256:" + "1" * 64, "sha256:" + "2" * 64
CLIENT = {"checksums": "https://mirror.openshift.com/pub/openshift-v4/clients/ocp/4.21.15/sha256sum.txt",
          "file": "openshift-client-linux-4.21.15.tar.gz",
          "url": "https://mirror.openshift.com/pub/openshift-v4/clients/ocp/4.21.15/openshift-client-linux-4.21.15.tar.gz"}
AUTH = '{"auths":{"quay.io":{"auth":"cXVheTp0b2tlbg=="},"203.0.113.20:5000":{"auth":"bWlycm9yOnMzY3JldA=="}}}'


GZIP = "application/vnd.docker.image.rootfs.diff.tar.gzip"


def archive(members, mode="w:gz", fmt=tarfile.DEFAULT_FORMAT, pax=None):
    data = io.BytesIO()
    with tarfile.open(fileobj=data, mode=mode, format=fmt, pax_headers=pax or {}) as tar:
        for name, content in members.items():
            info = tarfile.TarInfo(name)
            info.size, info.mode = len(content), 0o755
            tar.addfile(info, io.BytesIO(content))
    return data.getvalue()


def blob_digest(data):
    return "sha256:" + hashlib.sha256(data).hexdigest()


def image_references(*components):
    tags = [{"name": name, "from": {"kind": "DockerImage", "name": repository + "@" + digest}} for name, repository, digest in components]
    return json.dumps({"kind": "ImageStream", "spec": {"tags": tags}}).encode()


class Mirror:
    """A registry holding a release whose last layer carries its image references."""

    def __init__(self, references=None, held=(CLI, INSTALLER)):
        references = references or image_references(("cli", mirror_release.COMPONENTS, CLI), ("installer", mirror_release.COMPONENTS, INSTALLER))
        base, manifests = archive({"usr/bin/true": b"x"}), archive({"release-manifests/image-references": references})
        self.blobs = {blob_digest(base): base, blob_digest(manifests): manifests}
        self.layers = [{"digest": blob_digest(base), "mediaType": GZIP}, {"digest": blob_digest(manifests), "mediaType": GZIP}]
        self.publish()
        self.held = set(held)

    def publish(self):
        body = json.dumps({"mediaType": "application/vnd.docker.distribution.manifest.v2+json", "layers": self.layers}).encode()
        self.digest = "sha256:" + hashlib.sha256(body).hexdigest()
        self.manifests = {RELEASE["tag"]: body, self.digest: body}

    def get(self, url, accept, bound):
        prefix = "https://%s/v2/%s/" % (MIRROR, mirror_release.RELEASE)
        assert url.startswith(prefix)
        kind, reference = url[len(prefix):].split("/", 1)
        if kind == "manifests":
            body = self.manifests.get(reference)
            return (200, {}, body) if body is not None else (404, {}, b"")
        return (200, {}, io.BytesIO(self.blobs[reference])) if reference in self.blobs else (404, {}, None)

    def head(self, url):
        digest = url.rsplit("/", 1)[1]
        assert url == "https://%s/v2/%s/manifests/%s" % (MIRROR, mirror_release.COMPONENTS, digest)
        return digest if digest in self.held else None


def test_a_release_held_whole_reports_every_component_and_none_missing():
    mirror = Mirror()
    digest, components, missing = mirror_release.held(mirror.get, mirror.head, MIRROR, RELEASE)
    assert digest == mirror.digest and missing == 0
    assert components == [{"name": "cli", "digest": CLI}, {"name": "installer", "digest": INSTALLER}]
    assert mirror_release.held(mirror.get, mirror.head, MIRROR, dict(RELEASE, digest=mirror.digest))[0] == mirror.digest


def test_a_component_the_mirror_lacks_is_counted_and_an_absent_release_holds_nothing():
    assert mirror_release.held(Mirror(held=(CLI,)).get, Mirror(held=(CLI,)).head, MIRROR, RELEASE)[2] == 1
    empty = Mirror()
    empty.manifests = {}
    assert mirror_release.held(empty.get, empty.head, MIRROR, RELEASE) == ("", [], 0)


def test_a_release_at_another_digest_or_naming_a_foreign_component_refuses():
    mirror = Mirror()
    pinned = "sha256:" + "f" * 64
    assert mirror_release.held(mirror.get, mirror.head, MIRROR, dict(RELEASE, digest=pinned)) == ("", [], 0), "a pin the mirror lacks is not held"
    mirror.manifests[pinned] = mirror.manifests[RELEASE["tag"]]
    with pytest.raises(mirror_release.Refused) as refused:
        mirror_release.held(mirror.get, mirror.head, MIRROR, dict(RELEASE, digest=pinned))
    assert refused.value.reason == "release-digest-mismatch", "a manifest served for a pin it does not hash to"
    foreign = Mirror(references=image_references(("cli", "quay.io/example/other", CLI)))
    with pytest.raises(mirror_release.Refused) as refused:
        mirror_release.held(foreign.get, foreign.head, MIRROR, RELEASE)
    assert refused.value.reason == "release-incomplete"
    bare = Mirror()
    bare.blobs[bare.layers[1]["digest"]] = archive({"release-manifests/other": b"{}"})
    with pytest.raises(mirror_release.Refused) as refused:
        mirror_release.held(bare.get, bare.head, MIRROR, RELEASE)
    assert refused.value.reason == "release-incomplete", "a layer that does not hash to its digest is never read"


def damaged(change):
    mirror = Mirror()
    change(mirror)
    mirror.publish()
    with pytest.raises(mirror_release.Refused) as refused:
        mirror_release.held(mirror.get, mirror.head, MIRROR, RELEASE)
    return refused.value.reason


def test_a_layer_past_its_bounds_or_in_another_form_refuses_before_it_is_read_whole(monkeypatch):
    def replace(mirror, data, media=GZIP):
        mirror.blobs[blob_digest(data)] = data
        mirror.layers[1] = {"digest": blob_digest(data), "mediaType": media}
    long_header = archive({"release-manifests/image-references": b"{}"}, fmt=tarfile.PAX_FORMAT, pax={"comment": "x" * (2 << 20)})
    assert damaged(lambda mirror: replace(mirror, long_header)) == "release-incomplete"
    assert damaged(lambda mirror: replace(mirror, archive({"a": b"b"}), "application/vnd.oci.image.layer.v1.tar+zstd")) == "release-incomplete"
    assert damaged(lambda mirror: mirror.blobs.pop(mirror.layers[1]["digest"])) == "release-incomplete"
    plain = archive({"release-manifests/image-references": image_references(("cli", mirror_release.COMPONENTS, CLI))}, mode="w")
    mirror = Mirror()
    replace(mirror, plain, "application/vnd.oci.image.layer.v1.tar")
    mirror.publish()
    assert mirror_release.held(mirror.get, mirror.head, MIRROR, RELEASE)[1] == [{"name": "cli", "digest": CLI}]
    monkeypatch.setattr(mirror_release, "MAX_EXPANDED", 1024)
    big = archive({"filler": b"0" * 8192, "release-manifests/image-references": b"{}"})
    assert damaged(lambda mirror: replace(mirror, big)) == "release-incomplete"


def test_a_read_past_its_time_bound_names_the_mirror_unreadable():
    mirror = Mirror()
    with pytest.raises(mirror_release.Refused) as refused:
        mirror_release.held(mirror.get, mirror.head, MIRROR, RELEASE, budget=-1)
    assert refused.value.reason == "mirror-unreadable"


def test_a_request_for_another_release_never_reads_the_mirror():
    for release in (dict(RELEASE, repository="quay.io/example/release"), dict(RELEASE, tag="latest"), dict(RELEASE, digest="sha256:short")):
        with pytest.raises(ValueError):
            mirror_release.checked(release, MIRROR)


class Publisher:
    def __init__(self, listed=None, unreachable=False):
        self.archive = archive({"oc": b"#!oc\n", "kubectl": b"#!kubectl\n", "README.md": b"readme"})
        digest = hashlib.sha256(self.archive).hexdigest()
        self.listed = listed if listed is not None else ("%s  %s\n%s  openshift-install-linux-4.21.15.tar.gz\n" % (digest, CLIENT["file"], "0" * 64)).encode()
        self.unreachable, self.reads = unreachable, []

    def fetch(self, url, _bound):
        self.reads.append(url)
        if self.unreachable:
            raise URLError("refused")
        return io.BytesIO(self.listed if url == CLIENT["checksums"] else self.archive)


def private(tmp_path, job="bootwright-scratch-1-1"):
    scratch = tmp_path / "runs" / job / "mirror-release"
    scratch.mkdir(mode=0o700, parents=True)
    scratch.chmod(0o700)
    (tmp_path / "run").mkdir(exist_ok=True)
    return str(scratch)


def prepared(fetch, scratch, tmp_path, now=None):
    return mirror_release.prepare(fetch, CLIENT, scratch, AUTH, "anchor", secrets_base=str(tmp_path / "run"), now=now)


def test_prepare_lends_only_a_verified_oc_the_auth_document_the_anchor_and_a_private_home(tmp_path):
    scratch = private(tmp_path)
    publisher = Publisher()
    lent = prepared(publisher.fetch, scratch, tmp_path)
    assert publisher.reads == [CLIENT["checksums"], CLIENT["url"]]
    assert sorted(os.listdir(scratch)) == ["bin", "cache", "config", "data", "home", "runtime"], "no secret on the persistent disk"
    assert os.path.dirname(lent) == str(tmp_path / "run") and os.path.basename(lent).startswith("bootwright-release-")
    assert sorted(os.listdir(lent)) == ["auth.json", "trust"] and stat.S_IMODE(os.stat(lent).st_mode) == 0o700
    assert os.listdir(os.path.join(scratch, "bin")) == ["oc"], "only oc leaves the archive, which is removed"
    assert stat.S_IMODE(os.stat(os.path.join(scratch, "bin", "oc")).st_mode) == 0o700
    assert stat.S_IMODE(os.stat(os.path.join(lent, "auth.json")).st_mode) == 0o600
    assert json.loads(open(os.path.join(lent, "auth.json")).read())["auths"].keys() == {"quay.io", MIRROR}
    assert open(os.path.join(lent, "trust", "mirror.pem")).read() == "anchor"
    for name in ("home", "config", "cache", "data", "runtime"):
        assert stat.S_IMODE(os.stat(os.path.join(scratch, name)).st_mode) == 0o700


def test_prepare_sweeps_what_a_killed_copy_left_once_no_run_can_still_hold_it(tmp_path):
    scratch = private(tmp_path)
    old, recent = private(tmp_path, "bootwright-scratch-7-1"), private(tmp_path, "bootwright-scratch-8-1")
    other = tmp_path / "runs" / "bootwright-scratch-9-1" / "managedos-install"
    other.mkdir(mode=0o700, parents=True)
    os.utime(str(other), (1, 1))
    for path in (old, recent):
        open(os.path.join(path, "auth.json"), "w").close()
    stale_lent, fresh_lent = tmp_path / "run" / "bootwright-release-old", tmp_path / "run" / "bootwright-release-new"
    for path in (stale_lent, fresh_lent):
        path.mkdir(mode=0o700)
        (path / "auth.json").write_text(AUTH)
    now = 10 * 3600 + 100
    for path in (old, str(stale_lent)):
        os.utime(path, (100, 100))
    for path in (recent, str(fresh_lent)):
        os.utime(path, (now - 60, now - 60))
    prepared(Publisher().fetch, scratch, tmp_path, now=now)
    assert not os.path.exists(old) and not os.path.exists(os.path.dirname(old)), "a stale copy's scratch and its emptied job are gone"
    assert not stale_lent.exists() and fresh_lent.exists(), "only a lent directory older than any run is swept"
    assert os.path.exists(os.path.join(recent, "auth.json")) and other.exists(), "a live run and another role's scratch stay"


def test_an_archive_its_publisher_does_not_list_is_never_extracted_or_lent(tmp_path):
    other, ambiguous = "%s  %s\n" % ("0" * 64, CLIENT["file"]), "%s  %s\n%s  %s\n" % ("0" * 64, CLIENT["file"], "1" * 64, CLIENT["file"])
    for listed in (other.encode(), b"", ambiguous.encode()):
        scratch = private(tmp_path / ("x" + str(len(listed))))
        with pytest.raises(mirror_release.Refused) as refused:
            prepared(Publisher(listed=listed).fetch, scratch, tmp_path / ("x" + str(len(listed))))
        assert refused.value.reason == "client-digest-mismatch"
        assert os.listdir(scratch) == [] and os.listdir(str(tmp_path / ("x" + str(len(listed))) / "run")) == [], "nothing was lent"
    scratch = private(tmp_path / "unreachable")
    with pytest.raises(mirror_release.Refused) as refused:
        prepared(Publisher(unreachable=True).fetch, scratch, tmp_path / "unreachable")
    assert refused.value.reason == "client-unreachable" and os.listdir(scratch) == [] and os.listdir(str(tmp_path / "unreachable" / "run")) == []


def test_prepare_refuses_a_run_directory_another_user_could_read_or_one_already_used(tmp_path):
    shared = tmp_path / "shared"
    shared.mkdir(mode=0o755)
    shared.chmod(0o755)
    used = tmp_path / "used"
    used.mkdir(mode=0o700)
    (used / "auth.json").write_text("{}")
    for scratch in (shared, used):
        with pytest.raises(ValueError):
            mirror_release.prepare(Publisher().fetch, CLIENT, str(scratch), AUTH, "anchor", secrets_base=str(tmp_path))


def test_the_route_reaches_the_mirror_directly_and_the_origins_through_the_frozen_proxy():
    route = mirror_release.route({"httpsProxy": "http://proxy.example.test:3128", "noProxy": [".example.test"]}, "[2001:db8::20]:5000")
    assert route["HTTPS_PROXY"] == route["https_proxy"] == "http://proxy.example.test:3128"
    assert route["no_proxy"].split(",") == [".example.test", "[2001:db8::20]", "2001:db8::20"]


def test_clean_removes_the_lent_directory_and_empties_the_run_directory_without_following_a_link(tmp_path):
    scratch = private(tmp_path)
    lent = prepared(Publisher().fetch, scratch, tmp_path)
    outside = tmp_path / "outside"
    outside.mkdir()
    (outside / "kept").write_text("kept")
    os.symlink(str(outside), os.path.join(scratch, "home", "link"))
    mirror_release.clean(scratch, lent, secrets_base=str(tmp_path / "run"))
    assert os.listdir(scratch) == [] and not os.path.exists(lent) and (outside / "kept").read_text() == "kept"
    with pytest.raises(ValueError):
        mirror_release.clean(scratch, str(outside), secrets_base=str(tmp_path / "run"))
