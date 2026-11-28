"""The values the managed OS roles once derived are the ones Go now freezes.

Which proofs a completion requires, how long each worker session runs and
where the artifact server's roots are were computed in Jinja from other fields
of the request. Go now derives and freezes them (internal/managedos/
installation) and the roles read them as frozen. Each expression below is the
role's former derivation, kept verbatim, and every request golden Go freezes
must carry exactly what that derivation gave for it, so moving the rule
changed no installation. The goldens travel from Go's testdata as one document
(collection_requests_test.go).

Rendering needs Ansible's controller (DataLoader and Templar), which
ansible-test does not offer to unit tests under tests/unit/plugins.
"""

from __future__ import annotations

import json
import pathlib

import pytest
from ansible.template import Templar, trust_as_template
from ansible_collections.bootwright.core.tests.unit.role_tasks import LOADER

GOLDENS = json.loads((pathlib.Path(__file__).resolve().parent / "goldens" / "managedos_request_goldens.json").read_text(encoding="utf-8"))["requests"]
INSTALLS = {name: request for name, request in GOLDENS.items() if name.startswith("request-")}
INSTALLS.update({name + ".install": request["install"] for name, request in GOLDENS.items() if "install" in request})
TREES = {name: request for name, request in GOLDENS.items() if name.startswith("tree-")}
R = "bootwright_os_install_request"

# The installation role's former derivations, verbatim.
FORMER_INSTALL = {
    "fips": "{{ 'fips=1' in (%s.installerArguments) }}" % R,
    "rootEncryption": "{{ %s.diskEncryption is defined }}" % R,
    "rootWWN": """{{ (%(r)s.target.physical | default(false) | bool)
         and (%(r)s.target.hardware.serialNumber | default('') | length > 0)
         and (%(r)s.target.hardware.rootDevice | default('') | length == 0)
         and %(r)s.target.hardware.rootVolumeRef is not defined }}""" % {"r": R},
    "registration": "{{ %s.registration is defined }}" % R,
    "update": "{{ 'update' in %s }}" % R,
}
FORMER_REGISTRATION = """{{ (750 if 'update' in %(r)s else 630)
         + (60 if 'satellite' in %(r)s.registration else 0)
         + (60 if %(r)s.registration.satellite.contentView | default('') else 0) }}""" % {"r": R}
FORMER_SERVED_ROOT = """{{ (%(r)s.image.path | dirname | dirname | dirname)
     if %(r)s.image is defined
     else (%(r)s.private.path | dirname | dirname | dirname) }}""" % {"r": R}
FORMER_TREE = {
    "area": "{{ bootwright_os_tree_request.publication.path.split('/')[5:7] }}",
    "server": "{{ bootwright_os_tree_request.publication.path.split('/')[:8] | join('/') }}",
    "servedRoot": "{{ bootwright_os_tree_request.publication.path | dirname | dirname | dirname }}",
}


def render(expression, **variables):
    return Templar(loader=LOADER, variables=variables).template(trust_as_template(expression))


def test_every_request_golden_is_read():
    assert len(INSTALLS) == 20 and len(TREES) == 1
    assert any(request["requires"]["registration"] for request in INSTALLS.values())
    assert any(request["requires"]["update"] for request in INSTALLS.values())
    assert any(request["requires"]["fips"] for request in INSTALLS.values())
    assert any(request["requires"]["rootEncryption"] for request in INSTALLS.values())


@pytest.mark.parametrize("name", sorted(INSTALLS))
def test_the_frozen_requirements_are_what_the_role_derived(name):
    request = INSTALLS[name]
    derived = {key: render(expression, **{R: request}) for key, expression in FORMER_INSTALL.items()}
    if "cloudInit" in request:
        derived["cloudInit"] = True
    if request["target"].get("tpm2"):
        derived["tpm2"] = True
    assert request["requires"] == derived


@pytest.mark.parametrize("name", sorted(INSTALLS))
def test_the_frozen_sessions_are_what_the_role_chose(name):
    request = INSTALLS[name]
    registering, updating = "registration" in request, "update" in request
    chosen = {
        "registration": int(render(FORMER_REGISTRATION, **{R: request})) if registering else 0,
        "unregistration": 330 if registering else 0,
        "update": 1800 if updating else 0,
        "updateCompletion": 120 if updating else 0,
    }
    # registration_observe.yml and update_observe.yml each ran 120 s; a request
    # that observes neither omits the session.
    if registering or updating:
        chosen["inspection"] = 120
    assert request["budgets"]["sessions"] == chosen


@pytest.mark.parametrize("name", sorted(INSTALLS))
def test_the_frozen_served_root_is_what_the_role_recovered(name):
    request = INSTALLS[name]
    if "cloudInit" in request:
        assert not request["servedRoot"] and "image" not in request and "private" not in request
    else:
        assert request["servedRoot"] == render(FORMER_SERVED_ROOT, **{R: request})


@pytest.mark.parametrize("name", sorted(TREES))
def test_the_frozen_tree_roots_are_what_the_role_recovered(name):
    request = TREES[name]
    for key, expression in FORMER_TREE.items():
        assert request[key] == render(expression, bootwright_os_tree_request=request), key
