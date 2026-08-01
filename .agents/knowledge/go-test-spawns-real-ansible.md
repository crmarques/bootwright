# A Go Test Spawning the Real `ansible-playbook`

## Symptom

A Go test package takes minutes instead of seconds, and gets slower on a loaded
machine rather than just contended. `ps` during the run shows real
`ansible-playbook` processes with inventory paths under a `Test*` temp
directory, and `ssh` processes dialling fixture hostnames such as
`bastion.bootwright.test`. The package's wall time tracks machine load rather
than its own work, because the cost is timeouts waiting, not CPU.

## Root cause

`workspace.ResolveAnsiblePlaybook()` (`internal/workspace/paths.go`) returns the
managed venv binary when one exists and otherwise falls back to the bare string
`"ansible-playbook"`. `converge/ansible.CommandRunner` passes that to `exec`,
which resolves it against the process `PATH` at exec time. A test that calls
`Run(...)` with a converge verb — `apply` or `destroy` without `--dry-run` —
therefore executes whatever Ansible is installed on the developer's machine or
the CI image.

The play then tries to reach hosts that only exist in the fixture. Each one
burns the SSH `ConnectTimeout` (30s in the generated inventory) before failing.
Nothing about that is coverage: the tests in question assert on the CLI's
authorization output, which is produced *before* the first Ansible invocation.

Measured on `internal/cli`, idle host, paired runs:
`TestApplyDestroySafetyMatrix` took 231.6s with the real binary and 45.3s with a
stub; the whole package went from 408.9s to 159.8s. Take such numbers only from
runs made back to back on an idle machine — under load the same package varies by
more than 2x, because the cost is timeouts waiting rather than CPU.

## What to do

`internal/cli`'s `TestMain` installs a stub `ansible-playbook` on `PATH` for the
whole package. Any new test package that drives a converge path must do the
same, or pass an explicit stub executable.

Do not "fix" this by asserting the Ansible run fails — that keeps the timeout.
Do not rely on Ansible being absent; it is present in CI and on every
development machine that can run `make build`.

Playbook validity is covered by `make ansible-syntax-check`, which is what that
stage exists for. Losing the accidental execution loses no verification.

## Related

- [ADR 0041](../../specs/adr/0041-the-gate-runs-what-the-change-can-break.md) —
  the tiered gate this discovery motivated.
- [`ansible-ssh-liveness-timeouts.md`](ansible-ssh-liveness-timeouts.md) — where
  the 30s connect timeout comes from.
