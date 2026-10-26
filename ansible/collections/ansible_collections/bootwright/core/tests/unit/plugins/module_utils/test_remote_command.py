"""The one quoting of a remote command joins an argv the remote shell splits back exactly."""

from __future__ import annotations

import shlex

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import remote_command


# The remote shell splits the joined string back into exactly the argv, whatever a token holds.
def test_a_joined_command_splits_back_into_its_argv():
    argv = ["/usr/bin/sudo", "-n", "/usr/bin/python3", "-I", "-c", "print('a b'); x = \"$HOME\" `id` ; rm -rf /\n", "640"]
    assert shlex.split(remote_command.remote_command(argv)) == argv
    assert remote_command.remote_command(("receipt",)) == "receipt"


def test_a_fixed_worker_runs_its_source_as_root_through_the_isolated_interpreter():
    joined = remote_command.fixed_worker("print('fixed')", "4285")
    assert shlex.split(joined) == ["/usr/bin/sudo", "-n", "/usr/bin/python3", "-I", "-c", "print('fixed')", "4285"]
    assert remote_command.fixed_worker_argv("print('fixed')", "4285") == tuple(shlex.split(joined))


@pytest.mark.parametrize("argv", [[], (), "receipt", [""], ["a", None], ["a", 1], ["a\0b"], None])
def test_anything_but_an_argv_of_non_empty_strings_is_refused(argv):
    with pytest.raises(ValueError):
        remote_command.remote_command(argv)
