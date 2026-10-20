"""A Ceph session pins exactly the algorithms the managed-OS session's host_key_algorithms filter names for the same key and policy.

The filter needs Ansible's controller, so this check runs in controller context.
"""

from __future__ import annotations

import pytest
from ansible.errors import AnsibleFilterError

from ansible_collections.bootwright.core.plugins.filter.host_key_algorithms import host_key_algorithms as pinned_filter
from ansible_collections.bootwright.core.plugins.module_utils import ceph_native, ceph_ssh_policy, ceph_tunnel, ssh_key_policy
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_ssh_policy as session

HOST = {"name": "node-1", "address": "192.0.2.1", "port": 22}


def test_the_pinned_table_is_the_filters_table():
    assert {name: ",".join(value) for name, value in ceph_tunnel.HOST_KEY_ALGORITHMS.items()} == ssh_key_policy.ALGORITHMS


@pytest.mark.parametrize("listed", [session.DEFAULT, session.FIPS, session.OPENSSH_DEFAULT, "rsa-sha2-256,ecdsa-sha2-nistp521"])
@pytest.mark.parametrize("key_type", sorted(ssh_key_policy.ALGORITHMS))
def test_pinned_algorithms_match_the_host_key_algorithms_filter(key_type, listed):
    line = session.key(key_type)
    try:
        want = pinned_filter(line, listed)
    except AnsibleFilterError:
        want = None
    try:
        got = ceph_ssh_policy.host_key_algorithms({"configuration": "", "hostKeyAlgorithms": listed}, ("192.0.2.1 " + line + "\n").encode(), HOST)
    except ceph_native.Refusal as refused:
        assert str(refused) == "ssh-policy-host-excluded"
        got = None
    assert got == want
