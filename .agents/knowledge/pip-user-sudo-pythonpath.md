# ansible not found under sudo (pip --user install)

**Symptom:** `sudo bootwright apply <target>` fails with `ansible-playbook: command not found` or Python `ModuleNotFoundError` for ansible modules, even though ansible works for the non-root user.

**Root cause:** The user installed ansible via `pip install --user`. That places it under `~/.local/lib/python*/site-packages/`, which is not in root's `sys.path`. When bootwright runs ansible-playbook under sudo, the interpreter launched by the subprocess is root's python3, which cannot see the user's site-packages.

**Fix:** `sudoUserSitePackages` in `internal/ansible/runner.go` detects the `SUDO_USER` environment variable, resolves that user's pip `--user` site directory by running `python3 -m site --user-site` with `HOME` set to the original user's home, and prepends the result to `PYTHONPATH` in the subprocess environment.

**Invariant:** This only fires when `SUDO_USER` is set and not `root`, and only when the resolved site directory exists on disk.
