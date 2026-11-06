"""A push the controller lost midway is continued: the next media insert replaces the upload DataVolume no earlier attempt finished."""

from __future__ import annotations

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import kubevirt_api as kapi
from ansible_collections.bootwright.core.plugins.module_utils import kubevirt_machine as arm
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils.test_kubevirt_machine import (
    CLAIMS, MEDIA_PATH, VM, FakeAPI, context, host, pinned, request)


def uploaded(identity, phase):
    """A machine with an upload DataVolume this attempt's predecessor left in one phase under one media identity."""
    api = FakeAPI()
    arm.apply(host(api), request(), context())
    assert arm.upload_volume(pinned(api), VM, identity, 1)[0] is False
    api.objects[MEDIA_PATH]["status"]["phase"] = phase
    return api


def writes(api):
    return [(method, path) for method, path in api.requests if method != "GET"]


@pytest.mark.parametrize("phase", ["UploadReady", "UploadInProgress", "Pending"])
def test_an_upload_left_unfinished_under_this_machines_identity_is_ejected_and_recreated(phase):
    api = uploaded("digest-1", phase)
    left = api.objects[MEDIA_PATH]["metadata"]["uid"]
    api.requests.clear()
    reuse, ignored_live = arm.upload_volume(pinned(api), VM, "digest-1", 1)
    assert not reuse and api.objects[MEDIA_PATH]["metadata"]["uid"] != left
    assert api.objects[MEDIA_PATH]["metadata"]["annotations"][arm.MEDIA] == "digest-1" and api.objects[MEDIA_PATH]["status"]["phase"] == "UploadReady"
    assert [m for m, p in writes(api)] == ["DELETE", "POST"] and writes(api)[0][1] == MEDIA_PATH and CLAIMS + VM + "-media" in api.objects
    assert api.objects[CLAIMS + VM + "-media"]["metadata"]["ownerReferences"][0]["uid"] == api.objects[MEDIA_PATH]["metadata"]["uid"]


def test_a_finished_upload_of_the_same_identity_is_reused_and_one_of_another_identity_is_replaced():
    api = uploaded("digest-1", "Succeeded")
    api.requests.clear()
    assert arm.upload_volume(pinned(api), VM, "digest-1", 1)[0] and not writes(api)
    left = api.objects[MEDIA_PATH]["metadata"]["uid"]
    reuse, ignored_live = arm.upload_volume(pinned(api), VM, "digest-2", 1)
    assert not reuse and api.objects[MEDIA_PATH]["metadata"]["uid"] != left
    assert api.objects[MEDIA_PATH]["metadata"]["annotations"][arm.MEDIA] == "digest-2" and [m for m, p in writes(api)] == ["DELETE", "POST"]
    interrupted = uploaded("digest-1", "UploadInProgress")
    interrupted.requests.clear()
    assert not arm.upload_volume(pinned(interrupted), VM, "digest-2", 1)[0]
    assert interrupted.objects[MEDIA_PATH]["metadata"]["annotations"][arm.MEDIA] == "digest-2" and [m for m, p in writes(interrupted)] == ["DELETE", "POST"]


@pytest.mark.parametrize("foreign", ["unlabelled", "owned by another machine"])
def test_an_upload_volume_that_is_not_this_machines_refuses_and_is_left_untouched(foreign):
    api = uploaded("digest-1", "UploadInProgress")
    held = api.objects[MEDIA_PATH]
    if foreign == "unlabelled":
        held["metadata"].pop("labels")
    else:
        held["metadata"]["ownerReferences"] = [{"controller": True, "uid": "another-machine"}]
    before = dict(held["metadata"])
    api.requests.clear()
    with pytest.raises(kapi.Refused, match="DataVolume guests/%s-media is not this context's, so it was not touched" % VM):
        arm.upload_volume(pinned(api), VM, "digest-1", 1)
    assert ("GET", MEDIA_PATH) in api.requests and not writes(api) and api.objects[MEDIA_PATH]["metadata"] == before
