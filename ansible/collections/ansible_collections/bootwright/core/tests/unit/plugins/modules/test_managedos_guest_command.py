"""The guest command module runs the one builder's argv, bounded in time and output, with the caller's argv quoted once."""

from __future__ import annotations

import shlex

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import bounded_process, managedos_guest_ssh
from ansible_collections.bootwright.core.plugins.modules import managedos_guest_command as module


class FakeRunner:
    """A recording bounded runner that answers with fixed bytes or fails with a kind."""

    def __init__(self, stdout=b"", stderr=b"", code=0, failure=None):
        self.calls = []
        self.stdout, self.stderr, self.code, self.failure = stdout, stderr, code, failure

    def __call__(self, argv, data, timeout, limit, environment, **options):
        self.calls.append({"argv": list(argv), "stdin": data, "timeout": timeout, "limit": limit, "env": environment, **options})
        if self.failure:
            raise bounded_process.Failure(self.failure)
        return bounded_process.Result(self.code, self.stdout, self.stderr)


def params(tmp_path, **values):
    paths = {}
    for name in ("ssh_config", "known_hosts", "identity"):
        path = tmp_path / name
        path.write_bytes(b"")
        paths[name] = str(path)
    value = {"address": "192.0.2.10", "user": "fleet", "identity": paths["identity"], "ssh_config": paths["ssh_config"],
             "known_hosts": paths["known_hosts"], "host_key_algorithms": "ssh-ed25519",
             "policy_host_key_algorithms": "ssh-ed25519,rsa-sha2-512", "argv": ["/usr/bin/cat", "/proc/sys/kernel/random/boot_id"],
             "stdin": None, "timeout": 120}
    value.update(values)
    return value


def builder_argv(value):
    return managedos_guest_ssh.guest_argv(
        ssh_config=value["ssh_config"], known_hosts=value["known_hosts"], host_key_algorithms=value["host_key_algorithms"],
        policy_host_key_algorithms=value["policy_host_key_algorithms"], identity=value["identity"], user=value["user"],
        address=value["address"], remote_argv=value["argv"])


def test_the_argv_is_the_builders_and_the_run_is_bounded_and_clean(tmp_path):
    runner = FakeRunner(stdout=b"0f1e\n", stderr=b"warning\r\n")
    value = params(tmp_path)
    outcome = module.execute(value, runner)
    assert outcome == {"changed": False, "rc": 0, "stdout": "0f1e", "stderr": "warning"}
    call = runner.calls[0]
    assert call["argv"] == builder_argv(value)
    assert call["timeout"] == 120 and call["cwd"] == "/"
    assert call["env"] == {"PATH": "/usr/bin:/usr/sbin", "LC_ALL": "C"}
    assert call["limit"] == managedos_guest_ssh.MAX_COMMAND_OUTPUT + managedos_guest_ssh.MAX_DIAGNOSTIC
    assert call["stdin"] == b""
    options = [call["argv"][index + 1] for index, word in enumerate(call["argv"]) if word == "-o"]
    assert options[0] == "UserKnownHostsFile=" + value["known_hosts"]
    assert [option for option in options if not option.startswith(("UserKnownHostsFile=", "HostKeyAlgorithms="))] == list(managedos_guest_ssh.OPTIONS)


def test_the_remote_words_are_quoted_once_by_remote_command(tmp_path):
    script = "set -u\nprintf 'id=%s\\n' \"$(value ID)\"; rm -rf / # $HOME `id`"
    runner = FakeRunner()
    module.execute(params(tmp_path, argv=["/usr/bin/sh", "-c", script, "bootwright-x", "a b", "$(id)"]), runner)
    remote = runner.calls[0]["argv"][-1]
    assert shlex.split(remote) == ["/usr/bin/sh", "-c", script, "bootwright-x", "a b", "$(id)"]
    assert remote == shlex.join(["/usr/bin/sh", "-c", script, "bootwright-x", "a b", "$(id)"])


def test_standard_input_is_the_only_data_and_is_bounded(tmp_path):
    runner = FakeRunner()
    module.execute(params(tmp_path, stdin='{"bootId":"x"}'), runner)
    assert runner.calls[0]["stdin"] == b'{"bootId":"x"}'
    outcome = module.execute(params(tmp_path, stdin="x" * (module.MAX_STDIN + 1)), runner)
    assert outcome["failure"] == "session" and "rc" not in outcome and len(runner.calls) == 1


def test_a_hung_command_names_its_timeout_and_answers_no_exit_status(tmp_path):
    outcome = module.execute(params(tmp_path, timeout=45), FakeRunner(failure="timeout"))
    assert outcome == {"changed": False, "stdout": "", "failure": "timeout",
                       "stderr": "the guest command did not finish within 45 seconds"}


@pytest.mark.parametrize("kind", ["output", "command"])
def test_a_run_that_did_not_end_answers_no_exit_status(tmp_path, kind):
    outcome = module.execute(params(tmp_path), FakeRunner(failure=kind))
    assert "rc" not in outcome and outcome["failure"] == kind and outcome["stdout"] == ""


@pytest.mark.parametrize("stdout, stderr", [(b"x" * (managedos_guest_ssh.MAX_COMMAND_OUTPUT + 1), b""),
                                            (b"", b"e" * (managedos_guest_ssh.MAX_DIAGNOSTIC + 1))])
def test_output_past_its_bound_is_never_returned(tmp_path, stdout, stderr):
    outcome = module.execute(params(tmp_path), FakeRunner(stdout=stdout, stderr=stderr))
    assert outcome == {"changed": False, "stdout": "", "failure": "output",
                       "stderr": "the guest command wrote more than its output bound"}


def test_output_within_its_bound_is_returned_whole(tmp_path):
    document = b"{" + b" " * (managedos_guest_ssh.MAX_COMMAND_OUTPUT - 2) + b"}"
    outcome = module.execute(params(tmp_path), FakeRunner(stdout=document))
    assert outcome["rc"] == 0 and len(outcome["stdout"]) == managedos_guest_ssh.MAX_COMMAND_OUTPUT


def test_a_dropped_connection_returns_ssh_s_own_status(tmp_path):
    outcome = module.execute(params(tmp_path), FakeRunner(code=255, stderr=b"Connection closed by 192.0.2.10\n"))
    assert outcome["rc"] == 255 and outcome["stderr"] == "Connection closed by 192.0.2.10"


@pytest.mark.parametrize("values", [
    {"timeout": 0}, {"timeout": 1801}, {"timeout": True}, {"argv": []}, {"argv": ["/usr/bin/true", ""]},
    {"address": "-oProxyCommand=id"}, {"user": "root;id"}, {"host_key_algorithms": "ssh-dss"},
    {"host_key_algorithms": "ecdsa-sha2-nistp256"}, {"ssh_config": "/dev/null"}, {"known_hosts": "relative"},
    {"argv": ["/usr/bin/cat", "x" * (managedos_guest_ssh.MAX_REMOTE + 1)]},
], ids=["no time", "too long", "boolean time", "no argv", "empty word", "option address", "bad user", "unknown algorithm",
        "unpermitted algorithm", "no configuration", "relative known_hosts", "oversized command"])
def test_a_refused_session_never_starts(tmp_path, values):
    runner = FakeRunner()
    outcome = module.execute(params(tmp_path, **values), runner)
    assert outcome["failure"] == "session" and "rc" not in outcome and outcome["stderr"]
    assert runner.calls == []


def test_main_runs_the_session_as_a_task_would(run_module, monkeypatch, tmp_path):
    runner = FakeRunner(stdout=b"boot\n")
    monkeypatch.setattr(managedos_guest_ssh.bounded_process, "run", runner)
    value = params(tmp_path)
    del value["stdin"]
    result = run_module(module, value)
    assert result["rc"] == 0 and result["stdout"] == "boot" and not result.get("failed")
    assert runner.calls[0]["argv"] == builder_argv(value)
