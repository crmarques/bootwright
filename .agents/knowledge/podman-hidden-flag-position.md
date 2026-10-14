# Podman's hidden `--signature-policy` is local to `pull`

Observed 2026-10-13 with podman 5.8.4 on Fedora 43, checked without network
access through `--help` parsing.

## Symptom

Every image pull exits 125 with `Error: unknown flag: --signature-policy`, the
bounded attempts run out and the host block refuses `image-pull`, although the
registry, credential and policy file are correct. A fake engine that looks a
flag up anywhere in argv passes the same argv.

## Cause

`--signature-policy` is a hidden flag of the `pull` subcommand, not a global
flag: `podman --signature-policy P pull ...` is refused, `podman pull
--signature-policy P ...` is accepted. `--hooks-dir` and `--default-mounts-file`
are accepted on `podman create` (and also before the subcommand), so their
placement in a contained-probe argv does not carry the same trap.

## Rule

Place `--signature-policy` after `pull`, as `ceph_host_runtime.HostRuntime.pull`
does, and make test engines match the subcommand position (`argv[:2] ==
["/usr/bin/podman", "pull"]`) rather than search argv for a flag. The flaw came
over from the retired fixture profile (`da492f162`'s `ceph_fixture_profile.py`).

Evidence:
[ceph_host_runtime.py](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_host_runtime.py),
[test_ceph_host_runtime.py](../../ansible/collections/ansible_collections/bootwright/core/tests/unit/plugins/module_utils/test_ceph_host_runtime.py).
