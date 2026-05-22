# Ansible DNF Unavailable Repo

**Symptom:** `bootwright apply infra` fails during
`host_base : Install base host packages` with a package-manager metadata error
like:

```text
Failed to download metadata for repo '<repo-id>': Cannot download repomd.xml
```

**Cause:** DNF refreshes metadata for every enabled repository before package
resolution. A stale or unreachable optional repository, such as a Satellite
client repo, can fail the base package task even when Bootwright's required
packages are available from other repositories.

**Fix:** The shared `host_base` role configures Red Hat family hosts with
`skip_if_unavailable=True` before package installation. Required packages still
fail if no healthy repository provides them, but unrelated unavailable repos do
not block provider host convergence.
