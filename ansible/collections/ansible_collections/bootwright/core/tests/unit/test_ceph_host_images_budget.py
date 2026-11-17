"""The Ceph host images budget is the one Go freezes into every host request, read there by each consumer.

Go derives the budget once and the block deadline from it; the role starts the images steps with it, the module and
ceph_core take it as their whole budget and ceiling, and the long launcher runs its remote worker under it. No
collection file spells the value itself, so a change of the Go derivation reaches every consumer.
"""
from __future__ import annotations

from pathlib import Path

import pytest
from ansible.template import Templar

from ansible_collections.bootwright.core.plugins.module_utils import ceph_core
from ansible_collections.bootwright.core.tests.unit.ceph_goldens import IMAGES_SECONDS, REQUESTS
from ansible_collections.bootwright.core.tests.unit.role_tasks import LOADER

COLLECTION = Path(__file__).resolve().parents[2]
HOSTS = {name: request for name, request in REQUESTS.items() if request.get("version") == "ceph-host-v1"}


def test_every_host_request_golden_freezes_the_same_images_budget():
    assert len(HOSTS) >= 4
    assert {request["imagesSeconds"] for request in HOSTS.values()} == {IMAGES_SECONDS}


def test_no_collection_source_spells_the_images_budget():
    spelled = [path.relative_to(COLLECTION).as_posix()
               for directory in ("plugins", "roles", "playbooks") for path in sorted((COLLECTION / directory).rglob("*"))
               if path.suffix in (".py", ".yml") and str(IMAGES_SECONDS) in path.read_text(encoding="utf-8")]
    assert not spelled


def test_the_role_starts_the_images_steps_with_the_frozen_budget():
    tasks = LOADER.load_from_file(str(COLLECTION / "roles/storage_ceph_host/tasks/prepare.yml"), trusted_as_template=True)
    start = next(task for task in tasks if task.get("name") == "Start from no reported step")["ansible.builtin.set_fact"]
    request = dict(HOSTS["host"], imagesSeconds=IMAGES_SECONDS - 1)
    rendered = Templar(loader=LOADER, variables={"bootwright_ceph_host_request": request}).template(start["storage_ceph_host_pull_seconds"])
    assert int(rendered) == IMAGES_SECONDS - 1


class Recorder:
    def __init__(self):
        self.calls = []

    def host_images(self, host, names, seconds, ceiling):
        self.calls.append((seconds, ceiling))
        return {"changed": False}


@pytest.mark.parametrize("frozen", [IMAGES_SECONDS, 600])
def test_an_images_step_takes_its_whole_budget_and_ceiling_from_the_request(monkeypatch, frozen):
    request = dict(HOSTS["host"], imagesSeconds=frozen)
    monkeypatch.setattr(ceph_core, "host_request", lambda value: value)
    monkeypatch.setattr(ceph_core.ceph_native, "supported_transport", lambda cluster: True)
    recorder = Recorder()
    ceph_core.host_step(request, "images", "d" * 64, recorder, pull=ceph_core.CEPH_IMAGE_GROUP, clock=lambda: 0)
    assert recorder.calls == [(frozen, frozen)]
    with pytest.raises(ceph_core.ceph_native.Refusal, match="^image-pull$"):
        ceph_core.host_step(request, "images", "d" * 64, recorder, pull=ceph_core.CEPH_IMAGE_GROUP, seconds=frozen + 1, clock=lambda: 0)
