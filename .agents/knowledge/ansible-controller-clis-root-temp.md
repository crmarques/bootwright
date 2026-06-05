# Ansible Controller CLI Root Temp

**Symptom:** `bootwright bastion setup` fails during
`ocp_clis : Ensure tmp working directory exists` with a permission denied
error under a user state directory, such as:

```text
There was an issue creating /rhome/<user>/.bootwright/state/tmp
```

**Cause:** The controller CLI install playbook runs as root so package install
and `/usr/local/bin` writes work. User home directories can reject root reads
or writes through NFS root-squash or restrictive mount policy, so the sudo-run
Ansible process must not depend on a user-owned state directory for its
playbook bundle or transient CLI downloads.

**Fix:** Keep the managed Ansible venv under `/var/lib/bootwright`, extract the
controller CLI Ansible bundle into a local system temp directory for the
sudo-wrapped playbook, and let the `ocp_clis` role create a per-run temporary
download directory with Ansible's `tempfile` module after it has determined a
CLI install is needed.
