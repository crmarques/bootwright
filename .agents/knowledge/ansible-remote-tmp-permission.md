# Ansible Remote Tmp Permission

**Symptom:** `bootwright apply infra` fails during
`PLAY [Apply provider services]` / `TASK [Gathering Facts]` with:

```text
/usr/bin/python3.12: can't open file '<home-dir>/.ansible/tmp/.../AnsiballZ_setup.py': [Errno 13] Permission denied
```

**Cause:** Provider and infra plays gather facts with `become: true`, so the
SSH user stages the setup module and the become user reads it before any role
task can run. Some enterprise home directories are root-squashed or protected
by ACLs that prevent root from reading files under the SSH user's home.

**Fix:** Keep shipped Ansible config at `[defaults] local_tmp = /tmp` and
`remote_tmp = /tmp` so controller and remote modules are staged under a
system temp directory. Bootwright's Ansible runners should also export the
matching `ANSIBLE_LOCAL_TEMP`, `ANSIBLE_REMOTE_TEMP`, and `ANSIBLE_REMOTE_TMP`
values so ambient operator environment variables cannot override the shipped
config. This must happen before Ansible starts, not in a role pre-task, because
fact gathering fails before pre-tasks or roles execute.
