# ansible not found under sudo (pip --user install)

**Symptom:** `bootwright apply <target>` auto-escalates and the root child fails with `ansible-playbook: command not found` or Python `ModuleNotFoundError` for ansible modules, even though ansible works for the non-root user.

**Root cause:** The user installed ansible via `pip install --user`. That places it under `~/.local/lib/python*/site-packages/`, which is not in root's `sys.path`. When bootwright runs ansible-playbook under sudo, the interpreter launched by the subprocess is root's python3, which cannot see the user's site-packages.

**Fix:** during Bootwright's internal auto-sudo path, `sudoUserSitePackages` in `internal/ansible/runner.go` resolves the original caller's pip `--user` site directory by running `python3 -m site --user-site` as that caller with the caller's `HOME` and `PATH`, then prepends the result to `PYTHONPATH` in the subprocess environment.

**Invariant:** this only fires for Bootwright's internal root child with captured caller identity, and only when the resolved site directory exists under the caller home. Explicit `sudo bootwright ...` is treated as a root invocation and must use root-visible Ansible.
