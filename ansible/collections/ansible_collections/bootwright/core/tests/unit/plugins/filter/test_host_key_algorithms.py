"""The host_key_algorithms filter names exactly the algorithms a pinned key's type is accepted under."""

from __future__ import annotations

import pytest
from ansible.errors import AnsibleFilterError

from ansible_collections.bootwright.core.plugins.filter.host_key_algorithms import host_key_algorithms

BLOB = "AAAAC3NzaC1lZDI1NTE5AAAAIBootwrightExampleBlob"

ACCEPTED = {
    "ssh-ed25519": "ssh-ed25519",
    "ssh-rsa": "rsa-sha2-512,rsa-sha2-256",
    "ecdsa-sha2-nistp256": "ecdsa-sha2-nistp256",
    "ecdsa-sha2-nistp384": "ecdsa-sha2-nistp384",
    "ecdsa-sha2-nistp521": "ecdsa-sha2-nistp521",
}


@pytest.mark.parametrize("key_type", list(ACCEPTED), ids=list(ACCEPTED))
def test_a_pinned_key_is_accepted_under_its_own_type(key_type):
    assert host_key_algorithms("%s %s metal-01" % (key_type, BLOB)) == ACCEPTED[key_type]
    assert host_key_algorithms("  %s\t%s\n" % (key_type, BLOB)) == ACCEPTED[key_type]


@pytest.mark.parametrize("line", ["ssh-dss " + BLOB, "", "garbage", BLOB + " ssh-ed25519", None],
                         ids=["ssh-dss", "empty", "garbage", "blob first", "not text"])
def test_any_other_type_refuses_without_repeating_the_key(line):
    with pytest.raises(AnsibleFilterError) as refused:
        host_key_algorithms(line)
    message = str(refused.value)
    assert "is not one an installation pins" in message
    if line and not line.startswith(BLOB):
        assert BLOB not in message


def test_the_named_type_is_printable_and_bounded():
    with pytest.raises(AnsibleFilterError) as refused:
        host_key_algorithms("\x1b[2J" + "x" * 200 + " " + BLOB)
    message = str(refused.value)
    assert "\x1b" not in message and "x" * 65 not in message and BLOB not in message
