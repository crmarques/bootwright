"""Reserve the local gate-to-fast-forward window; never certify a gate pass."""

from __future__ import annotations

import argparse
import contextlib
from datetime import datetime, timezone
import fcntl
import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import sys

BUSY = 75
REBASE = 3
GIT = shutil.which("git")


def git(root: Path, *args: str, check: bool = True) -> subprocess.CompletedProcess:
    """Keep the inspected checkout independent of inherited Git overrides."""
    environment = {k: v for k, v in os.environ.items() if not k.startswith("GIT_")}
    return subprocess.run(
        [GIT, "-C", str(root), *args], env=environment,
        stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=check,
    )


def value(root: Path, *args: str) -> str:
    return os.fsdecode(git(root, *args).stdout).strip()


def destination_checkout(root: Path, reference: str) -> Path | None:
    # -z preserves paths containing spaces or newlines. Never print unrelated
    # worktrees or branches: only the selected destination enters the record.
    records = git(root, "worktree", "list", "--porcelain", "-z").stdout.split(b"\0\0")
    for record in records:
        fields = dict(field.split(b" ", 1) for field in record.split(b"\0") if b" " in field)
        if fields.get(b"branch") == os.fsencode(reference):
            return Path(os.fsdecode(fields[b"worktree"]))
    return None


def preflight(root: Path, destination: str) -> tuple[dict, int]:
    reference = "refs/heads/" + destination
    git(root, "check-ref-format", reference)
    head = value(root, "rev-parse", "HEAD")
    tip = value(root, "rev-parse", "--verify", reference)
    branch = value(root, "symbolic-ref", "--quiet", "--short", "HEAD")
    checkout = destination_checkout(root, reference)
    ancestry = git(root, "merge-base", "--is-ancestor", tip, head, check=False)
    if ancestry.returncode not in (0, 1):
        ancestry.check_returncode()
    result = {
        "destination": destination, "destination_head": tip,
        "task_branch": branch, "task_head": head,
        "destination_worktree": str(checkout) if checkout else None,
        "rebase_needed": ancestry.returncode == 1,
    }
    if branch == destination or checkout is None:
        result["reason"] = "use a task worktree and a checked-out destination"
        return result, 1
    if git(checkout, "status", "--porcelain", "-z").stdout:
        result["reason"] = "destination has uncommitted or untracked changes; preserve them"
        return result, 1
    if result["rebase_needed"]:
        result["reason"] = "rebase onto the destination, then run focus before reserving again"
        return result, REBASE
    result["reason"] = "ready; recheck under the reservation before long checks"
    return result, 0


def report(state: dict) -> None:
    print(json.dumps(state, sort_keys=True), flush=True)


def owner(record) -> dict | None:
    record.seek(0)
    try:
        return json.loads(record.read(16384))
    except (ValueError, UnicodeError):
        return None


class Interrupted(Exception):
    def __init__(self, signum: int):
        self.signum = signum


def interrupted(signum, _frame):
    raise Interrupted(signum)


def stop_child(process: subprocess.Popen) -> None:
    # Kill the whole foreground command group before releasing its reservation.
    # Do not let a stuck child's ignored TERM hold the guard forever.
    with contextlib.suppress(ProcessLookupError):
        os.killpg(process.pid, signal.SIGTERM)
    try:
        process.wait(timeout=5)
    except subprocess.TimeoutExpired:
        pass
    with contextlib.suppress(ProcessLookupError):
        os.killpg(process.pid, signal.SIGKILL)
    process.wait()


def execute(root: Path, command: list[str], record, state: dict) -> int:
    executable = shutil.which(command[0])
    if executable is None:
        raise ValueError("integration command executable is unavailable")
    environment = {k: v for k, v in os.environ.items() if not k.startswith("GIT_")}
    environment.update({
        "BOOTWRIGHT_INTEGRATION_BASE": state["destination_head"],
        "BOOTWRIGHT_INTEGRATION_DESTINATION": state["destination"],
        "BOOTWRIGHT_INTEGRATION_WORKTREE": state["destination_worktree"],
    })
    handlers = {sig: signal.signal(sig, interrupted) for sig in (signal.SIGINT, signal.SIGTERM)}
    process = None
    try:
        process = subprocess.Popen(
            [os.path.abspath(executable), *command[1:]], cwd=root, env=environment,
            start_new_session=True, pass_fds=(record.fileno(),),
        )
        status = process.wait()
        return status if status >= 0 else 128 - status
    except Interrupted as error:
        return 128 + error.signum
    finally:
        # Ignore a second interruption until the child is stopped and reaped.
        for sig in handlers:
            signal.signal(sig, signal.SIG_IGN)
        if process is not None:
            stop_child(process)
        for sig, handler in handlers.items():
            signal.signal(sig, handler)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("status", "run"))
    parser.add_argument("--destination", default="main")
    parser.add_argument("command", nargs=argparse.REMAINDER)
    # Options belong before the action; remainder is an argv, never shell text.
    args = parser.parse_args()
    command = args.command
    if command[:1] == ["--"]:
        command = command[1:]
    if (args.action == "run" and not command) or (args.action == "status" and command):
        parser.error("use [--destination BRANCH] status, or run -- COMMAND [ARG...]")
    root = Path(value(Path.cwd(), "rev-parse", "--show-toplevel"))
    common = Path(value(root, "rev-parse", "--path-format=absolute", "--git-common-dir"))
    # A stable inode in the common Git directory coordinates different worktrees
    # and cache overrides. The kernel lock, never a PID or timeout, owns it.
    descriptor = os.open(common / "bootwright-integration.lock", os.O_RDWR | os.O_CREAT, 0o600)
    with os.fdopen(descriptor, "r+", encoding="utf-8") as record:
        try:
            fcntl.flock(record, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            report({"reservation": "busy", "owner": owner(record)})
            return BUSY
        state, status = preflight(root, args.destination)
        report({"reservation": "available", **state})
        if args.action == "status" or status:
            return status
        record.seek(0)
        record.truncate()
        json.dump({
            **state, "pid": os.getpid(),
            "started": datetime.now(timezone.utc).isoformat(),
        }, record)
        record.flush()
        # Keep the lock through the complete command, including its final FF.
        # The child inherits it so killing just this supervisor cannot admit a
        # second foreground command while the first is still running.
        return execute(root, command, record, state)


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        print(f"integration-guard: {error}", file=sys.stderr)
        sys.exit(1)
