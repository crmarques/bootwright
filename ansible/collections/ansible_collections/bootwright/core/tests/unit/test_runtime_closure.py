"""Every plugin imports with only the controller runtime's distributions.

The runtime lock leaves out what ansible-core declares but no controller path
imports (runtime_closure). These cases import every plugin of the collection,
and the ansible-core modules a runner's playbook run loads, once with
everything the check environment holds and once confined to the runtime
closure, and require the confined run to fail no import the open run passes;
test_playbook_entry_points runs every playbook confined the same way. The
confined run must refuse an omitted distribution, or it proved nothing.
"""

from __future__ import annotations

import json
import os
import pathlib
import subprocess
import sys

import pytest
from ansible_collections.bootwright.core.tests.unit.runtime_closure import closure_environment, refused

COLLECTION = pathlib.Path(__file__).resolve().parents[2]
COLLECTIONS = COLLECTION.parents[2]
CORE = (
    "ansible.cli.playbook",
    "ansible.executor.playbook_executor",
    "ansible.plugins.connection.local",
    "ansible.plugins.connection.ssh",
    "ansible.plugins.loader",
    "ansible.parsing.vault",
    "ansible.module_utils.urls",
)

SWEEP = r"""
import importlib
import json
import sys

from ansible.utils.collection_loader._collection_finder import _AnsibleCollectionFinder

_AnsibleCollectionFinder(paths=[sys.argv[1]])._install()
failures = {}
for name in json.loads(sys.argv[2]):
    try:
        importlib.import_module(name)
    except BaseException as error:
        failures[name] = "%s: %s" % (type(error).__name__, error)
print(json.dumps(failures))
"""


def plugins():
    root = COLLECTION / "plugins"
    names = []
    for path in sorted(root.rglob("*.py")):
        if path.name != "__init__.py":
            names.append(".".join(("ansible_collections", "bootwright", "core", "plugins") + path.relative_to(root).with_suffix("").parts))
    return names


def swept(work, confined):
    environment = dict(os.environ, **confined)
    ran = subprocess.run(
        [sys.executable, "-c", SWEEP, str(COLLECTIONS), json.dumps(list(CORE) + plugins())],
        stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=environment, cwd=str(work),
        check=False)
    assert ran.returncode == 0, ran.stderr.decode()
    return json.loads(ran.stdout.decode())


@pytest.fixture(scope="module")
def sweeps(tmp_path_factory, once_per_run):
    def compute():
        work = tmp_path_factory.mktemp("runtime-closure")
        confined, record = closure_environment(work)
        return {"open": swept(work, {}), "confined": swept(work, confined), "refused": sorted(refused(record))}
    return once_per_run("runtime-closure", compute)


def test_the_sweep_covers_every_plugin():
    assert len(plugins()) > 100


def test_confined_to_the_runtime_closure_every_plugin_imports_as_it_does_unconfined(sweeps):
    added = {name: error for name, error in sweeps["confined"].items() if name not in sweeps["open"]}
    assert added == {}


def test_the_confined_sweep_refuses_an_omitted_distribution(sweeps):
    assert "cryptography" in sweeps["refused"]
