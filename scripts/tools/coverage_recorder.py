"""Record which lines of the collection's plugins a pytest run executes.

A pytest plugin on the standard library's sys.monitoring (CPython 3.12 or
newer), so no coverage distribution enters the locked check environment.
`./scripts/ansible-check --coverage DIR` loads it into the plain unit loop
with ROOT_VARIABLE naming the collection copy under test and OUT_VARIABLE the
directory it writes to. Each line location reports once and then disables
itself, so the run pays for a line only the first time it executes. Code that
runs only in a child process is not seen, so the figures are a floor.
"""

import json
import os
import sys

ROOT_VARIABLE = "BOOTWRIGHT_COVERAGE_ROOT"
OUT_VARIABLE = "BOOTWRIGHT_COVERAGE_OUT"

# The tool identity sys.monitoring reserves for a coverage tool.
TOOL_NAME = "bootwright-coverage"


class Recorder:
    """Collect the executed lines of the files under one plugins directory."""

    def __init__(self, root: str):
        # A code object names its file as it was imported, through any link in
        # the root's path or without one.
        self.roots = tuple(
            {os.path.join(os.path.abspath(root), ""), os.path.join(os.path.realpath(root), "")}
        )
        self.lines: dict[str, set[int]] = {}
        self.tool = None

    def line(self, code, line):
        name = code.co_filename
        for root in self.roots:
            if name.startswith(root):
                self.lines.setdefault(name[len(root) :], set()).add(line)
                break
        return sys.monitoring.DISABLE

    def start(self) -> None:
        """Start recording, refusing when another tool holds the identity."""
        monitoring = sys.monitoring
        tool = monitoring.COVERAGE_ID
        current = monitoring.get_tool(tool)
        if current is not None:
            raise RuntimeError(
                f"the coverage tool identity is held by {current}; nothing is recorded"
            )
        monitoring.use_tool_id(tool, TOOL_NAME)
        monitoring.register_callback(tool, monitoring.events.LINE, self.line)
        monitoring.set_events(tool, monitoring.events.LINE)
        self.tool = tool

    def stop(self) -> None:
        """Stop recording and release the tool identity."""
        if self.tool is None:
            return
        monitoring = sys.monitoring
        monitoring.set_events(self.tool, 0)
        monitoring.register_callback(self.tool, monitoring.events.LINE, None)
        monitoring.free_tool_id(self.tool)
        self.tool = None

    def write(self, directory: str) -> str:
        """Write the lines seen so far to a file of this process; return its path."""
        path = os.path.join(directory, f"lines.{os.getpid()}.json")
        partial = path + ".partial"
        with open(partial, "w", encoding="utf-8") as stream:
            json.dump(
                {name: sorted(found) for name, found in sorted(self.lines.items())},
                stream,
            )
        os.replace(partial, path)
        return path


def started_from(environ) -> "Recorder | None":
    """Start a recorder of the root the environment names, or return None."""
    root, out = environ.get(ROOT_VARIABLE), environ.get(OUT_VARIABLE)
    if not root or not out:
        return None
    recorder = Recorder(os.path.join(root, "plugins"))
    recorder.start()
    return recorder


# pytest imports a plugin named by -p before it loads any conftest.py, which
# may import plugins, so recording starts here rather than in a later hook.
_recorder = started_from(os.environ)


def pytest_configure(config):
    if _recorder is None:
        raise RuntimeError(
            f"the coverage recorder needs {ROOT_VARIABLE} and {OUT_VARIABLE}"
        )


def pytest_unconfigure(config):
    global _recorder
    if _recorder is None:
        return
    _recorder.stop()
    _recorder.write(os.environ[OUT_VARIABLE])
    _recorder = None
