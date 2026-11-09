"""A published image or private file a role removes lies beneath the context's service directories.

The media and installation roles remove the published path recursively, so a
path under another hosted area of the context, such as a Machine's custody or
BMC directory, refuses at the request check before any effect.
"""

from __future__ import annotations

import pathlib

import pytest
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar

ROLES = pathlib.Path(__file__).resolve().parents[2] / "roles"
LOADER = DataLoader()
HOSTED = "/var/lib/bootwright-hosted/lab"
SERVED = HOSTED + "/services/artifact-server/artifacts/public/os/node/image.iso"
ELSEWHERE = [HOSTED + "/machines/node/erase/op/image.iso", HOSTED + "/machines/node/bmc/image.iso",
             HOSTED + "/machines/node/install/image.iso", HOSTED + "x/services/artifact-server/image.iso",
             "/var/lib/bootwright-hosted/other/services/artifact-server/image.iso", "/var/lib/bootwright-hosted/lab"]


def condition(role, operation, marker):
    tasks = LOADER.load_from_file(str(ROLES / role / "tasks" / (operation + ".yml")), trusted_as_template=True)
    found = [item for item in tasks[0]["ansible.builtin.assert"]["that"] if marker in item]
    assert len(found) == 1
    return found[0]


def media(path):
    return {"bootwright_cluster_media_request": {"deliveryKind": "fetch", "identity": {"context": "lab", "cluster": "sno"},
                                                 "image": {"path": path}, "workRoot": HOSTED + "/clusters/sno"}}


def install(path, field):
    return {"bootwright_os_install_request": {"identity": {"context": "lab", "object": "node"}, field: {"path": path}}}


def holds(expression, variables):
    return Templar(loader=LOADER, variables=variables).evaluate_conditional(expression)


@pytest.mark.parametrize("operation", ["apply", "destroy"])
def test_the_media_roles_take_a_fetched_image_only_from_the_context_service_directories(operation):
    expression = condition("containercluster_media_agent", operation, "image.path.startswith")
    assert holds(expression, media(SERVED))
    for path in ELSEWHERE:
        assert not holds(expression, media(path)), path


@pytest.mark.parametrize("operation", ["apply", "destroy"])
@pytest.mark.parametrize("field", ["image", "private"])
def test_the_installation_roles_take_a_published_path_only_from_the_context_service_directories(operation, field):
    expression = condition("managedos_install_anaconda", operation, "private.path).startswith")
    assert holds(expression, install(SERVED, field))
    for path in ELSEWHERE:
        assert not holds(expression, install(path, field)), path
