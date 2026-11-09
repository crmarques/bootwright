"""Run a Python process that can import only what the controller runtime holds.

The controller runtime is a venv of exactly the distributions its lock names
(internal/controller/runtimelock/lock.json); the lock leaves out what
ansible-core declares but no controller path imports. The check environment
holds those and the test tools besides, so a child process started with
closure_environment refuses every top-level import that resolves to the check
environment's site-packages outside RUNTIME_CLOSURE, as the runtime would, and
records each refused name. TestTheCollectionTestsNameTheLockedClosure
(internal/controller/runtimelock/statements_test.go) holds RUNTIME_CLOSURE to
the lock.
"""

from __future__ import annotations

import os
import pathlib

RUNTIME_CLOSURE = ("ansible-core", "jinja2", "markupsafe", "packaging", "pyyaml", "urllib3")

SITECUSTOMIZE = '''
import importlib.abc
import importlib.machinery
import importlib.metadata
import os
import re
import sys
import sysconfig

ALLOWED = %r
RECORD = os.environ["BOOTWRIGHT_CLOSURE_RECORD"]
ROOTS = tuple(os.path.realpath(sysconfig.get_paths()[key]) + os.sep for key in ("purelib", "platlib"))
TOPS = frozenset(top for top, dists in importlib.metadata.packages_distributions().items()
                 if any(re.sub(r"[-_.]+", "-", dist).lower() in ALLOWED for dist in dists))


class RuntimeClosure(importlib.abc.MetaPathFinder):
    def find_spec(self, name, path=None, target=None):
        if path is not None or name in TOPS:
            return None
        spec = importlib.machinery.PathFinder.find_spec(name)
        if spec is None:
            return None
        where = spec.origin or next(iter(spec.submodule_search_locations or ()), "")
        if not os.path.realpath(where).startswith(ROOTS):
            return None
        with open(RECORD, "a", encoding="utf-8") as record:
            record.write(name + "\\n")
        raise ModuleNotFoundError("No module named %%r" %% name, name=name)


sys.meta_path.insert(0, RuntimeClosure())
''' % (frozenset(RUNTIME_CLOSURE),)


def closure_environment(directory: pathlib.Path) -> tuple[dict[str, str], pathlib.Path]:
    """Environment entries that confine a child Python to the runtime closure,
    and the file it records each refused import in."""
    hook = directory / "runtime-closure"
    hook.mkdir(parents=True, exist_ok=True)
    (hook / "sitecustomize.py").write_text(SITECUSTOMIZE, encoding="utf-8")
    record = directory / "runtime-closure-refused.txt"
    record.write_text("", encoding="utf-8")
    path = os.pathsep.join(part for part in (str(hook), os.environ.get("PYTHONPATH", "")) if part)
    return {"PYTHONPATH": path, "BOOTWRIGHT_CLOSURE_RECORD": str(record)}, record


def refused(record: pathlib.Path) -> set[str]:
    """The top-level names a confined child refused to import."""
    return set(record.read_text(encoding="utf-8").split())
