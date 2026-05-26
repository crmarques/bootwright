# Ansible DNF Unavailable Repo

**Symptom:** `bootwright apply infra` fails during
`host_base : Install base host packages`, or `bootwright apply bastion` fails
while installing controller Python, with a package-manager metadata/repository
error like:

```text
Failed to download metadata for repo '<repo-id>': Cannot download repomd.xml
There are no enabled repositories
```

**Cause:** DNF refreshes metadata for every enabled repository before package
resolution. A stale or unreachable optional repository, such as a Satellite
client repo, can fail the base package task even when Bootwright's required
packages are available from other repositories.

**Fix:** The shared `host_base` role configures Red Hat family hosts with
`skip_if_unavailable=True` before package installation. Required packages still
fail if no healthy repository provides them, but unrelated unavailable repos do
not block provider host convergence.

For controller bootstrap, Bootwright first discovers Python 3.12 using the
original caller's PATH inside internal sudo re-exec. It should only fall back
to `dnf install -y python3.12` when no suitable caller/root-visible Python is
available.
