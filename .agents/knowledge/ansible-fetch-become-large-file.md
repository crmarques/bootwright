# Ansible Become Fetch Large File

## Symptom

- Apply appears stuck at `Fetch generated agent ISO to local cluster runtime state`.
- `Create agent ISO` completed quickly.
- `/var/lib/bootwright/contexts/<context>/clusters/<cluster>/runtime/installer/.openshift_install.log`
  has no new lines while the fetch task is active.

## Cause

The OpenShift install play runs with `become: true`. Ansible `fetch` under
become can use a slurp/checksum path before transfer. For generated agent ISOs,
that path is slow, memory-heavy, and silent enough to look hung.

## Fix

Do not fetch large generated artifacts with become. Copy the root-owned artifact
to a private temporary file owned by the SSH connection user, run `fetch` with
`become: false`, and remove the temporary file in an `always` cleanup block.
Keep both the temporary file and the local cluster runtime copy mode `0600`.
