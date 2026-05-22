# Managed Ansible venv package install fails with Errno 2

**Symptom:** `bootwright apply bastion` plans only an `install ansible-core`
bootstrap step, then pip fails with `Could not install packages due to an
OSError: [Errno 2] No such file or directory`.

**Root cause:** The root-managed venv under `/var/lib/bootwright/ansible-venv`
can contain enough metadata for version probes to run while the installed
package files or console scripts are stale or partially removed. Incremental
`pip install ansible-core==...` may then fail while replacing the old package.

**Fix:** Treat the managed venv as an owned artifact. When its Python, pip, or
ansible-core pin does not match, rebuild it with `python -m venv --clear` and
install packages with the venv interpreter via `python -m pip`.
