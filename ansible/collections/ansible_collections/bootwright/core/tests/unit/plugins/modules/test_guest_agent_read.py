"""A refused read is unknown, but it reports what refused it.

The identity operation retries an unknown answer for its whole budget, so an
agent that will never answer has to be distinguishable from one that has not
answered yet by reading the result rather than by waiting it out.
"""

from __future__ import annotations

import pytest

from ansible_collections.bootwright.core.plugins.modules.guest_agent_read import (
    ALLOWED,
    Unanswered,
    diagnosis,
    read_file,
)


# The allow list is the channel's only boundary, so widening it must never be
# incidental. Both entries are files the installation itself wrote: sshd's own
# key directory is unreadable to a confined agent, and the policy that would
# open it also reaches the private halves.
def test_the_channel_reads_only_what_the_installation_published():
    assert ALLOWED == (
        "/etc/bootwright/install-marker.json",
        "/etc/bootwright/host-key.pub",
    )
    for outside in ("/etc/ssh/ssh_host_ed25519_key.pub", "/etc/ssh/ssh_host_ed25519_key", "/etc/shadow"):
        assert outside not in ALLOWED


MARKER = "/etc/bootwright/install-marker.json"

REFUSAL = (
    "error: guest agent command failed: unable to execute QEMU agent command "
    "'guest-file-open': Command guest-file-open has been disabled: "
    "the command is not allowed\n"
)


def runner_for(code, out, err):
    """A runner that answers every argument vector the same way."""

    def run(argv, check_rc=False, environ_update=None):
        del argv, check_rc, environ_update
        return code, out, err

    return run


def test_a_blocked_command_is_unknown_and_names_its_own_refusal():
    with pytest.raises(Unanswered) as refused:
        read_file(runner_for(1, "", REFUSAL), "qemu:///system", "guest", MARKER, 4096)
    assert "has been disabled" in refused.value.reason


def test_an_agent_that_says_nothing_still_reports_something():
    with pytest.raises(Unanswered) as refused:
        read_file(runner_for(1, "", ""), "qemu:///system", "guest", MARKER, 4096)
    assert refused.value.reason == "the guest agent returned nothing"


def test_an_agent_that_opens_no_handle_is_unknown():
    with pytest.raises(Unanswered) as refused:
        read_file(runner_for(0, '{"return":{}}', ""), "qemu:///system", "guest", MARKER, 4096)
    assert refused.value.reason == "the guest agent opened no handle"


def test_a_reason_is_one_bounded_line():
    assert diagnosis("\n\n" + "x" * 500) == "x" * 200
    assert diagnosis("") == "the guest agent returned nothing"
