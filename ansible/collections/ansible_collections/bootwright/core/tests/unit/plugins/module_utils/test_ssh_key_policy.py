"""Policy rejection happens without SSH sessions, credentials or installer effects."""

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import bounded_process, ssh_key_policy as policy

HOST = "rsa-sha2-512,ecdsa-sha2-nistp256"
FLEET = "rsa-sha2-512,rsa-sha2-256,ecdsa-sha2-nistp256"


def answer(code, text):
    return bounded_process.Result(code, text.encode(), b"")


def native(argv, data, timeout, limit, environment, **options):
    assert argv == ["/usr/bin/ssh", "-G", "-F", "/dev/stdin", "-o", "CanonicalizeHostname=no", "localhost"]
    assert timeout == 10 and limit == 256 * 1024 and options == {"capture_stderr": False}
    assert environment == {"PATH": "/usr/bin:/bin", "LC_ALL": "C"}
    assert b"PRIVATE" not in data
    return answer(0, "hostkeyalgorithms %s\npubkeyacceptedalgorithms %s\n" % (HOST, FLEET))


@pytest.mark.parametrize("usage", ["host", "fleet"])
@pytest.mark.parametrize("key_type", ["ssh-ed25519", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521"])
def test_excluded_host_and_fleet_types_name_their_secret_before_installation(usage, key_type):
    result = policy.effective_policy("HostKeyAlgorithms " + HOST + "\nPubkeyAcceptedAlgorithms " + FLEET, native)
    with pytest.raises(policy.PolicyError, match="Secret/test-key key type " + key_type + ".*excluded") as refused:
        policy.check_keys(result, [{"secret": "test-key", "type": key_type, "usage": usage}])
    assert refused.value.reason == "ssh-policy-%s-excluded" % usage


# The host key a guest-agent installation generates is checked against the host
# key list, even where the fleet list would admit the type.
def test_check_keys_names_the_machine_for_a_generated_key():
    result = {"hostKeyAlgorithms": HOST, "fleetKeyAlgorithms": "ssh-ed25519," + FLEET}
    with pytest.raises(policy.PolicyError) as refused:
        policy.check_keys(result, [{"machine": "rhel-01", "type": "ssh-ed25519", "usage": "generated"}])
    assert refused.value.reason == "ssh-policy-generated-excluded"
    assert "Machine/rhel-01" in str(refused.value) and "ssh-ed25519" in str(refused.value)
    policy.check_keys(result, [{"machine": "rhel-01", "type": "ecdsa-sha2-nistp256", "usage": "generated"},
                               {"secret": "fleet", "type": "ssh-ed25519", "usage": "fleet"}])
    for machine in (None, "", "Rhel-01", "rhel-01-", "rhel_01"):
        with pytest.raises(policy.PolicyError) as refused:
            policy.check_keys(result, [{"machine": machine, "type": "ecdsa-sha2-nistp256", "usage": "generated"}])
        assert refused.value.reason == "ssh-policy-unavailable"
    with pytest.raises(policy.PolicyError) as refused:
        policy.check_keys(result, [{"machine": "rhel-01", "type": "ssh-dss", "usage": "generated"}])
    assert refused.value.reason == "ssh-policy-unavailable"


def test_native_default_and_modifier_evaluation_is_used_without_overriding_policy():
    calls = []

    def run(argv, data, *args, **kwargs):
        calls.append(data.decode())
        return native(argv, data, *args, **kwargs)

    for data in ("", "# native policy\nHostKeyAlgorithms -ssh-ed25519\nPubkeyAcceptedAlgorithms=^rsa-sha2-512\n"):
        result = policy.effective_policy(data, run)
        policy.check_keys(result, [{"secret": "fleet", "type": "ssh-rsa", "usage": "fleet"}])
        assert result["hostKeyAlgorithms"] == HOST
    assert calls == ["", "HostKeyAlgorithms -ssh-ed25519\nPubkeyAcceptedAlgorithms=^rsa-sha2-512\n"]


@pytest.mark.parametrize("data", [
    "Match exec touch /tmp/effect", "Include /tmp/config", "ProxyCommand command",
    "IdentityFile /tmp/key", "Host localhost", "UnknownPolicy yes", "Ciphers $(command)",
    "HostKeyAlgorithms \"ssh-ed25519\"", "x" * (policy.MAX_POLICY + 1),
])
def test_unsupported_policy_never_starts_a_native_process(data):
    def forbidden(*_args, **_kwargs):
        pytest.fail("unsupported policy reached native process")

    with pytest.raises(policy.PolicyError):
        policy.effective_policy(data, forbidden)


@pytest.mark.parametrize("result", [
    answer(1, "PRIVATE DIAGNOSTIC"),
    answer(0, "hostkeyalgorithms ssh-ed25519\n"),
])
def test_unavailable_policy_refuses_without_disclosing_native_output(result):
    with pytest.raises(policy.PolicyError) as refused:
        policy.effective_policy("", lambda *_args, **_kwargs: result)
    assert "PRIVATE" not in str(refused.value)


@pytest.mark.parametrize("kind", bounded_process.KINDS)
def test_native_failure_is_bounded_and_sanitized(kind):
    def failed(*_args, **_kwargs):
        raise bounded_process.Failure(kind)

    with pytest.raises(policy.PolicyError, match="cannot evaluate") as refused:
        policy.effective_policy("", failed)
    assert "PRIVATE" not in str(refused.value)


def test_reads_only_the_bounded_system_backend_and_allows_its_managed_symlink(tmp_path, monkeypatch):
    path = tmp_path / "backend"
    monkeypatch.setattr(policy, "POLICY_PATH", str(path))
    assert policy.read_policy() == ""
    target = tmp_path / "selected-policy"
    target.write_text("HostKeyAlgorithms " + HOST)
    path.symlink_to(target)
    assert policy.read_policy() == target.read_text()
    target.write_bytes(b"x" * (policy.MAX_POLICY + 1))
    with pytest.raises(policy.PolicyError, match="byte limit"):
        policy.read_policy()
    target.unlink()
    with pytest.raises(policy.PolicyError, match="target is missing"):
        policy.read_policy()


def test_nonregular_backend_refuses(tmp_path, monkeypatch):
    monkeypatch.setattr(policy, "POLICY_PATH", str(tmp_path))
    with pytest.raises(policy.PolicyError, match="regular file"):
        policy.read_policy()
