"""The actual supervisor waits for a detached descendant with closed result FDs."""

import os
from pathlib import Path
import subprocess
import sys
import tempfile

candidates = [
    Path(value) / "ansible_collections/bootwright/core"
    for value in os.environ.get("ANSIBLE_COLLECTIONS_PATH", "").split(os.pathsep)
    if value
]
candidates.append(Path(__file__).resolve().parents[4])
collection = next(
    path
    for path in candidates
    if (path / "plugins/module_utils/controller_supervisor.py").is_file()
)
supervisor = collection / "plugins/module_utils/controller_supervisor.py"

with tempfile.TemporaryDirectory(prefix="bootwright-supervisor-") as directory:
    marker = str(Path(directory, "descendant-finished"))
    program = r"""
import importlib.util, os, sys, time, types
marker, source = sys.argv[1:]
module = types.ModuleType('ansible.cli.playbook')
def playbook():
    parent = os.fork()
    if parent == 0:
        child = os.fork()
        if child == 0:
            os.setsid()
            os.closerange(3, 1024)
            time.sleep(0.2)
            with open(marker, 'w') as stream:
                stream.write('complete')
            os._exit(0)
        os._exit(0)
    os._exit(0)
module.main = playbook
sys.modules['ansible.cli.playbook'] = module
spec = importlib.util.spec_from_file_location('controller_supervisor_test', source)
implementation = importlib.util.module_from_spec(spec)
spec.loader.exec_module(implementation)
implementation.main()
"""
    result = subprocess.run(
        [sys.executable, "-I", "-B", "-S", "-c", program, marker, str(supervisor)],
        timeout=10,
        check=False,
    )
    if result.returncode != 0 or Path(marker).read_text() != "complete":
        raise RuntimeError("supervisor returned before detached descendant completed")
print(
    "PASS supervisor retained invocation lifetime through detached descriptor-closing descendant"
)
