"""Run one shipped entry point through ansible-core as the runner does.

The shared harness of the role-family integration targets. It writes an
isolated inventory of one local host, hands the frozen request as extra
variables, puts a directory of stub executables first on PATH, and drives the
playbook over the real runner protocol: the result channel on descriptor 3
and the authorization channel on descriptor 4, acknowledging every record. It
returns the records and the run's recap, so a target asserts the published
`completed` record and that a replay changes nothing. Every path a run writes
sits under the target's temporary root; the run needs no privilege and no
network, and the stubs use the standard library alone.
"""

import json
import os
from pathlib import Path
import re
import subprocess
import sys

INTERPRETER = os.environ.get("ANSIBLE_TEST_PYTHON_INTERPRETER", sys.executable)
RECAP = re.compile(
    r"^(?P<host>\S+)\s+: ok=\d+\s+changed=(?P<changed>\d+)\s+unreachable=(?P<unreachable>\d+)"
    r"\s+failed=(?P<failed>\d+)",
    re.M,
)


def collection():
    """The installed collection the lane runs, or this checkout's."""
    candidates = [
        Path(value) / "ansible_collections/bootwright/core"
        for value in os.environ.get("ANSIBLE_COLLECTIONS_PATH", "").split(os.pathsep)
        if value
    ]
    candidates.append(Path(__file__).resolve().parents[4])
    return next(path for path in candidates if (path / "playbooks").is_dir())


def stub(directory, name, body):
    """Write one executable stub, run by the lane's interpreter in isolated mode."""
    Path(directory).mkdir(mode=0o700, parents=True, exist_ok=True)
    path = Path(directory, name)
    path.write_text("#!" + INTERPRETER + " -I\n" + body, encoding="utf-8")
    path.chmod(0o700)
    return path


class Failure(SystemExit):
    """A target's failed expectation, carrying the run output that explains it."""


class EntryPoint:
    """One shipped playbook run against a temporary root."""

    def __init__(self, root, playbook, group="bootwright_target", host="target"):
        self.root = Path(root)
        self.stubs = self.root / "stubs"
        self.playbook = collection() / "playbooks" / playbook
        self.group, self.host = group, host
        self.runs = 0

    def run(self, variables):
        """Run the playbook once; return its records and the host's changed count."""
        self.runs += 1
        area = self.root / ("run-%d" % self.runs)
        area.mkdir(mode=0o700)
        for name in ("local", "remote"):
            (area / name).mkdir(mode=0o700)
        inventory = {
            "all": {
                "children": {
                    self.group: {
                        "hosts": {
                            self.host: {
                                "ansible_connection": "local",
                                "ansible_host": "localhost",
                                "ansible_python_interpreter": INTERPRETER,
                            }
                        }
                    }
                }
            }
        }
        (area / "inventory.json").write_text(json.dumps(inventory), encoding="utf-8")
        (area / "variables.json").write_text(json.dumps(variables), encoding="utf-8")
        output = area / "output.log"
        messages = self._drive(area, output)
        text = output.read_text(encoding="utf-8")
        sys.stdout.write(text)
        recap = {match["host"]: match for match in RECAP.finditer(text)}
        if self.host not in recap or recap[self.host]["failed"] != "0" or recap[self.host]["unreachable"] != "0":
            raise Failure("%s did not finish on %s" % (self.playbook.name, self.host))
        return messages, int(recap[self.host]["changed"])

    def _drive(self, area, output):
        result_read, result_write = os.pipe()
        authorization_read, authorization_write = os.pipe()

        def wire():
            os.dup2(result_write, 3)
            os.dup2(authorization_read, 4)

        with open(output, "wb") as log:
            # The close pass runs before preexec_fn, so the destination descriptors
            # must be kept explicitly or the remap lands on closed numbers.
            child = subprocess.Popen(  # pylint: disable=consider-using-with
                [
                    INTERPRETER,
                    "-I",
                    "-m",
                    "ansible.cli.playbook",
                    "-i",
                    str(area / "inventory.json"),
                    "--extra-vars",
                    "@" + str(area / "variables.json"),
                    str(self.playbook),
                ],
                cwd=area,
                stdin=subprocess.DEVNULL,
                stdout=log,
                stderr=subprocess.STDOUT,
                env={
                    "PATH": str(self.stubs) + ":/usr/bin:/usr/sbin",
                    "HOME": str(area),
                    "LANG": "C.UTF-8",
                    "LC_ALL": "C.UTF-8",
                    "TMPDIR": str(area),
                    "ANSIBLE_COLLECTIONS_PATH": os.environ.get("ANSIBLE_COLLECTIONS_PATH", ""),
                    "ANSIBLE_NOCOLOR": "1",
                    "ANSIBLE_LOCAL_TEMP": str(area / "local"),
                    "ANSIBLE_REMOTE_TEMP": str(area / "remote"),
                },
                preexec_fn=wire,  # pylint: disable=subprocess-popen-preexec-fn
                pass_fds=(3, 4, result_write, authorization_read),
            )
        os.close(result_write)
        os.close(authorization_read)
        messages = []
        with os.fdopen(result_read, "rb") as stream:
            for line in stream:
                messages.append(json.loads(line))
                # A record that waits reads one acknowledgement; one that does not
                # leaves it queued for the next, which proceeds as the runner would.
                try:
                    os.write(authorization_write, b"proceed\n")
                except BrokenPipeError:
                    pass
        os.close(authorization_write)
        child.wait()
        return messages


def completed(messages, outcome=None):
    """The one terminal completed record of a run, after its loaded handoff."""
    phases = [message.get("phase") for message in messages]
    if not phases or phases[0] != "loaded" or phases[-1] != "completed" or phases.count("completed") != 1:
        raise Failure("unexpected protocol sequence: %s" % phases)
    if "refused" in phases:
        raise Failure("the run published a refusal: %s" % phases)
    record = messages[-1]
    if outcome is not None and record.get("outcome") != outcome:
        raise Failure("the run reported %s, want %s" % (record.get("outcome"), outcome))
    if record.get("evidence", {}).get("postcondition") is not True:
        raise Failure("the completed evidence proves no postcondition")
    return record


def replay(entry, variables, check, first_outcome="changed", recorded=None):
    """Run an entry point twice: a first execution, then a replay that changes nothing.

    `check` returns the part of a record both runs must publish alike.
    `recorded` turns the first completed record into the variables of the
    replay, for a capability whose runner hands its recorded evidence back.
    Returns the replay's completed record.
    """
    messages, ignored_changed = entry.run(variables)
    record = completed(messages, first_outcome)
    first = check(record)
    messages, changed = entry.run(recorded(record) if recorded else variables)
    record = completed(messages, "unchanged")
    second = check(record)
    if changed:
        raise Failure("the replay reported %d changed tasks" % changed)
    if first != second:
        raise Failure("the replay published different evidence:\n%s\n%s" % (first, second))
    return record
