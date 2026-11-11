"""Each pinned image is copied once by digest into the mirror, read back, and its credential never outlives the run."""

import base64
import json
import os

import pytest

from ansible_collections.bootwright.core.plugins.modules import infrastructureservices_mirror_image as mirror_image

DIGEST = "sha256:" + "a" * 64
OTHER = "sha256:" + "b" * 64
MIRROR = "203.0.113.20:5000"
IMAGES = [
    {"repository": "docker.io/library/registry", "digest": DIGEST, "source": "docker.io/library/registry@" + DIGEST},
    {"repository": "quay.io/metal3-io/sushy-tools", "digest": DIGEST, "source": "quay.io/metal3-io/sushy-tools@" + DIGEST},
]
EGRESS = {"httpProxy": "", "httpsProxy": "http://proxy.example.test:3128", "noProxy": [".example.test"]}
TRUST = "-----BEGIN CERTIFICATE-----\nanchor\n-----END CERTIFICATE-----\n"


class Mirror:
    """A registry that answers HEADs from what it holds, and a skopeo that pushes into it."""

    def __init__(self, held=(), pushed=None, code=0, status=None):
        self.held, self.pushed, self.code, self.status = dict(held), pushed, code, status
        self.reads, self.copies, self.lent = [], [], {}

    def read(self, url):
        self.reads.append(url)
        if self.status is not None:
            return self.status, {}
        prefix = "https://%s/v2/" % MIRROR
        assert url.startswith(prefix)
        repository, reference = url[len(prefix):].split("/manifests/")
        digest = self.held.get((repository, reference))
        return (200, {"docker-content-digest": digest}) if digest else (404, {})

    def run(self, argv, route):
        self.copies.append((argv, route))
        authfile, certificates = argv[argv.index("--dest-authfile") + 1], argv[argv.index("--dest-cert-dir") + 1]
        self.lent = {"auth": json.load(open(authfile)), "auth mode": os.stat(authfile).st_mode & 0o777,
                     "trust": open(os.path.join(certificates, "ca.crt")).read(), "scratch": os.path.dirname(authfile)}
        repository, tag = argv[-1][len("docker://%s/" % MIRROR):].rsplit(":", 1)
        if self.code == 0:
            self.held[(repository, tag)] = self.pushed or tag.replace("-", ":", 1)
            self.held[(repository, tag.replace("-", ":", 1))] = self.pushed or tag.replace("-", ":", 1)
        return self.code, "", "error: unauthorized\n" if self.code else ""


def fill(mirror, observe=False, base=None):
    return mirror_image.fill(mirror.run, mirror.read, IMAGES, MIRROR, EGRESS, "user", "pass", TRUST, observe, base)


def test_a_replay_over_a_mirror_holding_every_digest_copies_nothing():
    mirror = Mirror(held={(image["repository"], DIGEST): DIGEST for image in IMAGES})
    changed, held = fill(mirror)
    assert changed is False and mirror.copies == []
    assert mirror.reads == ["https://%s/v2/%s/manifests/%s" % (MIRROR, image["repository"], DIGEST) for image in IMAGES]
    assert held == [{"repository": image["repository"], "digest": DIGEST, "verified": True} for image in IMAGES]


def test_a_missing_image_is_copied_by_digest_with_every_platform_then_read_back_under_its_tag(tmp_path):
    mirror = Mirror(held={(IMAGES[0]["repository"], DIGEST): DIGEST})
    changed, held = fill(mirror, base=str(tmp_path))
    assert changed is True and len(mirror.copies) == 1
    argv, route = mirror.copies[0]
    assert argv[:4] == ["/usr/bin/skopeo", "copy", "--all", "--preserve-digests"]
    assert "--src-no-creds" in argv and "--src-tls-verify=true" in argv and "--dest-tls-verify=true" in argv
    assert argv[-2:] == ["docker://quay.io/metal3-io/sushy-tools@" + DIGEST, "docker://%s/quay.io/metal3-io/sushy-tools:sha256-%s" % (MIRROR, "a" * 64)]
    assert mirror.reads[-1] == "https://%s/v2/quay.io/metal3-io/sushy-tools/manifests/sha256-%s" % (MIRROR, "a" * 64)
    assert route["https_proxy"] == route["HTTPS_PROXY"] == EGRESS["httpsProxy"]
    assert route["no_proxy"].split(",")[:2] == [".example.test", "203.0.113.20"], "the mirror itself is reached without the proxy"
    assert [entry["verified"] for entry in held] == [True, True]


def test_the_credential_and_anchor_are_lent_to_the_mirror_alone_and_removed_after(tmp_path):
    mirror = Mirror()
    fill(mirror, base=str(tmp_path))
    token = base64.b64encode(b"user:pass").decode("ascii")
    assert mirror.lent["auth"] == {"auths": {MIRROR: {"auth": token}}}
    assert mirror.lent["auth mode"] == 0o600 and mirror.lent["trust"] == TRUST
    assert not os.path.exists(mirror.lent["scratch"]) and list(tmp_path.iterdir()) == []


def test_a_failed_copy_names_the_origin_unreachable_and_still_removes_the_credential(tmp_path):
    mirror = Mirror(code=1)
    with pytest.raises(mirror_image.Refused) as refused:
        fill(mirror, base=str(tmp_path))
    assert refused.value.reason == "image-unreachable" and "pass" not in str(refused.value)
    assert list(tmp_path.iterdir()) == []


def test_a_mirror_holding_another_digest_under_the_tag_fails_as_a_mismatch(tmp_path):
    mirror = Mirror(pushed=OTHER)
    with pytest.raises(mirror_image.Refused) as refused:
        fill(mirror, base=str(tmp_path))
    assert refused.value.reason == "image-digest-mismatch"


def test_a_mirror_that_refuses_the_credential_is_unreadable_and_nothing_is_copied():
    mirror = Mirror(status=401)
    with pytest.raises(mirror_image.Refused) as refused:
        fill(mirror)
    assert refused.value.reason == "mirror-unreadable" and mirror.copies == []


def test_an_observation_copies_nothing_and_reports_only_what_the_mirror_holds():
    mirror = Mirror(held={(IMAGES[1]["repository"], DIGEST): DIGEST})
    changed, held = fill(mirror, observe=True)
    assert changed is False and mirror.copies == []
    assert held == [{"repository": IMAGES[1]["repository"], "digest": DIGEST, "verified": True}]


@pytest.mark.parametrize("images, mirror", [
    ([dict(IMAGES[0], digest="latest")], MIRROR),
    ([dict(IMAGES[0], source="docker.io/library/other@" + DIGEST)], MIRROR),
    ([dict(IMAGES[0], repository="Docker.io/Library/Registry")], MIRROR),
    (IMAGES, "mirror.example.test"),
    ([], MIRROR),
])
def test_a_request_that_names_no_pinned_image_or_no_mirror_port_is_refused(images, mirror):
    with pytest.raises(ValueError):
        mirror_image.fill(Mirror().run, Mirror().read, images, mirror, EGRESS, "user", "pass", TRUST)


def test_a_missing_credential_or_anchor_is_refused_before_any_read():
    probe = Mirror()
    with pytest.raises(ValueError):
        mirror_image.fill(probe.run, probe.read, IMAGES, MIRROR, EGRESS, "user", "", TRUST)
    assert probe.reads == []


def test_a_copy_the_mirror_then_does_not_hold_is_unreadable_not_a_mismatch(tmp_path):
    mirror = Mirror()
    mirror.run = lambda argv, route: (0, "", "")
    with pytest.raises(mirror_image.Refused) as refused:
        fill(mirror, base=str(tmp_path))
    assert refused.value.reason == "mirror-unreadable"
