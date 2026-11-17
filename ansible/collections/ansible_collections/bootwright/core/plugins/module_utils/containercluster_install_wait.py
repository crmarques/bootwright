"""One installer wait of an agent installation, bounded by its phase's frozen budget.

The installer gives up on its own compiled deadlines while the cluster keeps
converging, so a give-up it can be resumed after is re-invoked, DELAY seconds
after the last and at most RETRIES times, while the budget lasts. Every attempt
runs under timeout with only what the budget has left when that attempt
starts, so the wait returns within its budget and one kill-after grace, and an
attempt the budget stopped is told apart from the installer's own give-up. The
budget is the request's; nothing here chooses one.
"""

from __future__ import annotations

import os
import pwd
import re
import time

from ansible_collections.bootwright.core.plugins.module_utils import bounded_process

RETRIES = 40
DELAY = 30
# Once the budget is spent, timeout sends the installer SIGTERM, and SIGKILL to
# its process group this many seconds later if it is still running.
GRACE = 30
# How an attempt its budget stopped exits: 124 is timeout's own status once it
# sent SIGTERM, and a kill after the grace reaches timeout's own process group
# too, which a shell reports as 137 (128 + 9) and a process's return code as -9.
SPENT = (124, 137, -9)
MAX_OUTPUT = 64 << 20
# The installer runs with no ambient proxy or kubeconfig, a fixed PATH and
# LANG, and the running account's own home, where it may keep a cache or
# configuration as it did under the command module.
ENVIRONMENT = {"LANG": "C.UTF-8", "PATH": "/usr/bin:/bin"}


class NoHome(ValueError):
    """The account running the wait has no absolute home directory."""


def home():
    """The running account's home directory from the password database, never from the environment."""
    try:
        directory = pwd.getpwuid(os.geteuid()).pw_dir
    except KeyError:
        directory = ""
    if not isinstance(directory, str) or not os.path.isabs(directory):
        raise NoHome("home")
    return directory


MILESTONES = ("bootstrap-complete", "install-complete")
OUTCOMES = ("reached", "host-error", "stalled", "budget-spent", "timeout", "unclassified", "restore-refused")
# The give-ups the installer can be re-invoked after, and the one it cannot: a
# host the assisted service moved into error has stopped installing, so another
# window would watch a state that cannot change.
STALLED = re.compile(r"failed to progress after all hosts available|failed to prepare cluster installation")
TIMED_OUT = re.compile(r"bootstrap process timed out")
RESUMABLE = re.compile(STALLED.pattern + r"|" + TIMED_OUT.pattern
                       + r"|failed to initialize the cluster|timed out waiting for the condition")
HOST_ERROR = re.compile(r"has hosts in error|updated status from [a-z-]+ to error(?![-a-z])")

HOST_ERROR_HINT = (
    "the assisted service moved a declared host into error and stopped installing this cluster, so waiting again "
    "would watch a state that cannot change. A cluster diverted into recovery is never pivoted out of bootstrap, so "
    "an install left short of a declared node does not finish on its own. Repair the host, destroy this context and "
    "perform a fresh apply.")
STALLED_HINT = (
    "the cluster reached ready but the rendezvous node never started installing. It starts only once every declared "
    "node reports known, so a node that never booted the image, whose declared hardware address is not the one it "
    "registers with, or whose install disk was rejected holds the cluster there.")
STOPPED_AFTER_STALL = (
    "The installer was re-invoked after that give-up, and this wait's budget of %s seconds stopped that attempt.")
BUDGET_SPENT_HINT = (
    "this wait's budget of %s seconds was spent while the installer was still watching, so timeout stopped it: the "
    "installer did not give up on its own. The cluster keeps converging after the wait is stopped, so this is no "
    "proof of a failed install; repeat the apply to resume watching under a fresh budget.")
TIMEOUT_HINT = (
    "the installer stopped watching on its own compiled deadline, and this wait had no budget or attempt left to "
    "re-invoke it. The cluster keeps converging across that give-up, so the operator conditions reported here are its "
    "state at the last give-up rather than proof of a failed install; repeat the apply to resume watching where this "
    "attempt stopped.")
UNCLASSIFIED_HINT = (
    "this give-up is not one the installer can be resumed after, so the wait ran once and stopped. Read the installer "
    "output before repeating the apply.")
# Only a stall proves the cluster reached ready: after the installer's own
# timeout or a spent budget the hint names the nodes and nothing more.
UNREGISTERED_HINT = (
    "%s %s never registered with the assisted service on the rendezvous host%s starts installing only once every "
    "declared node reports known. A node that never booted the image, or whose declared hardware address is not the "
    "one it registers with, keeps the cluster from installing.")
UNREGISTERED_STALL = ", so the cluster reached ready but the rendezvous node never started installing: it"
UNREGISTERED_OTHER = ". The rendezvous node"
UNREACHED = "\nThe installer did not reach %s for %s."
RESTORED_NOTE = " This apply restored the installer's kubeconfig from the kept copy before waiting."
RESTORE_REFUSED = (
    "the installer's administrator kubeconfig in the work area of cluster %s no longer parses, and the copy this "
    "installation kept names no identity or another one than this apply recorded before its first effect, so it was "
    "not restored and the installer did not wait for %s. Destroy the cluster's installation first: bootwright "
    "destroy --context %s ejects its media and discards the work area, and the apply after it builds the image afresh")


def now():
    return int(time.time())


def installer(executable, milestone, work):
    """One attempt: the installer's wait for the milestone under timeout with the seconds given, as (rc, stderr).
    The home is resolved here, before any attempt, so an account without one refuses before the installer runs."""
    environment = dict(ENVIRONMENT, HOME=home())

    def attempt(seconds):
        argv = ["/usr/bin/timeout", "--kill-after=%ds" % GRACE, "%ds" % seconds, executable, "agent", "wait-for",
                milestone, "--dir", work, "--log-level", "info"]
        try:
            code, unused, errors = bounded_process.run(argv, b"", seconds + GRACE + 5, MAX_OUTPUT, environment=dict(environment))
        except bounded_process.Failure as failure:
            return (-9, "") if failure.kind == "timeout" else (1, "")
        return code, errors.decode("utf-8", "replace")
    return attempt


def attempts(budget, attempt, restore, clock=now, pause=time.sleep):
    """Run the attempts while the budget lasts and the installer can resume.

    Each attempt first has the installer's kubeconfig restored when it no
    longer parses, and a refused restore ends the wait before the installer
    runs. Whether the installer's own last give-up was a stall is kept apart
    from the last result: an attempt the budget stopped carries no give-up of
    its own, so it keeps the stall the one before it proved.
    """
    deadline = clock() + budget
    record = {"rc": None, "stderr": "", "stalled": False, "restored": False, "refused": False}
    for index in range(RETRIES + 1):
        if index and deadline > clock():
            pause(min(DELAY, deadline - clock()))
        remaining = deadline - clock()
        if remaining <= 0:
            break
        restored = restore()
        if restored == "refused":
            record["refused"] = True
            break
        record["restored"] = record["restored"] or restored == "restored"
        code, stderr = attempt(remaining)
        record["stalled"] = record["stalled"] if code in SPENT else STALLED.search(stderr) is not None
        record.update(rc=code, stderr=stderr)
        if code == 0 or code in SPENT or HOST_ERROR.search(stderr) or not RESUMABLE.search(stderr):
            break
    return record


def classify(record):
    """The outcome of a wait, a host in error first, then a stall, then the budget spent, then the installer's timeout."""
    stderr = record["stderr"]
    if record["refused"]:
        return "restore-refused"
    if record["rc"] == 0:
        return "reached"
    if HOST_ERROR.search(stderr):
        return "host-error"
    if record["stalled"] or STALLED.search(stderr):
        return "stalled"
    if record["rc"] is None or record["rc"] in SPENT:
        return "budget-spent"
    return "timeout" if TIMED_OUT.search(stderr) else "unclassified"


def reads_registration(milestone, outcome):
    """Whether a failed wait reads once which declared nodes registered: a bootstrap stall, timeout or spent budget."""
    return milestone == "bootstrap-complete" and outcome in ("stalled", "budget-spent", "timeout")


def printable(text):
    return "".join(character for character in text if character.isprintable())


def registered(registration, stalled):
    """What the read of the registered hosts names: each declared node never registered, and each in a bad state."""
    if not registration or registration.get("read") is not True:
        return ""
    lead = ""
    missing = registration.get("unregistered") or []
    if missing:
        lead = UNREGISTERED_HINT % ("node" if len(missing) == 1 else "nodes", ", ".join(missing),
                                    UNREGISTERED_STALL if stalled else UNREGISTERED_OTHER) + " "
    for host in registration.get("unready") or []:
        lead += "node %s is %s: %s; failing validations: %s. " % (
            host["name"], host["status"], printable(host["message"]), printable(", ".join(host["validations"])))
    return lead


def message(outcome, record, request, milestone, budget, registration=None, restored=False):
    """Why the milestone was never reached, leading with the classification, since a run tree keeps only a message's beginning."""
    identity = request["identity"]
    if outcome == "restore-refused":
        return RESTORE_REFUSED % (identity["cluster"], milestone, identity["context"])
    hints = {"host-error": HOST_ERROR_HINT, "budget-spent": BUDGET_SPENT_HINT % budget, "timeout": TIMEOUT_HINT,
             "unclassified": UNCLASSIFIED_HINT}
    hint = hints.get(outcome, STALLED_HINT)
    if outcome == "stalled" and (record["rc"] is None or record["rc"] in SPENT):
        hint += " " + STOPPED_AFTER_STALL % budget
    return (registered(registration, outcome == "stalled") + hint + UNREACHED % (milestone, identity["cluster"])
            + (RESTORED_NOTE if restored else ""))


def wait(request, milestone, budget, attempt, restore, read, restored=False, clock=now, pause=time.sleep):
    """One wait's typed outcome: its attempts, then the read a failed bootstrap wait makes, then its message.

    `restored` says whether an earlier wait of the same apply restored the
    installer's kubeconfig; the result carries it on for the next one.
    """
    if milestone not in MILESTONES or not isinstance(budget, int) or isinstance(budget, bool) or budget < 0:
        raise ValueError("installer wait")
    record = attempts(budget, attempt, restore, clock, pause)
    outcome = classify(record)
    registration = read() if reads_registration(milestone, outcome) else None
    result = {"outcome": outcome, "rc": record["rc"], "restored": restored or record["restored"],
              "changed": record["rc"] == 0 or record["restored"]}
    if outcome != "reached":
        result["message"] = message(outcome, record, request, milestone, budget, registration, result["restored"])
    if registration is not None:
        result["registration"] = registration
    return result
