# Ansible Sudo Requiretty

**Symptom:** `bootwright bastion setup` fails during
`ocp_clis : Install controller OS packages` with:

```text
sudo: sorry, you must have a tty to run sudo
```

**Controller-local cause:** The controller CLI install playbook runs against `localhost` with
Ansible's local connection. Some sudoers policies require a controlling TTY
even for passwordless sudo. Ansible local `become` may allocate a PTY for
stdin, but the sudo subprocess still lacks `/dev/tty`, so `requiretty` fails
before Ansible sees the become success marker.

**Controller-local fix:** Run the controller CLI playbook itself through direct sudo, and do not
use Ansible local `become` in the `ocp_clis` role. Non-root
`bastion setup` keeps the mutating-workflow default and allows a sudo prompt.
Passing `--ask-become-pass=false` keeps the automation path on `sudo -n` under
Bootwright's controlling PTY, so passwordless sudo still works with
`requiretty`.

**Historical scoped workflow symptom:** Older bundles could fail at
`install_agent : Add controller hosts entries for cluster endpoints` with the
same sudo error when controller DNS misses caused Bootwright to seed
`/etc/hosts`.

**Current scoped workflow fix:** `install_agent` validates controller DNS
without mutating `/etc/hosts`, so the cluster install DNS preflight no longer
uses controller-local `become`.

**Remote-host symptom:** `bootwright apply --stage infra` fails during
`PLAY [Apply provider services]` / `TASK [Gathering Facts]` on a provider host
with the same sudo error.

**Remote-host cause:** SSH pipelining is incompatible with provider hosts that
set sudoers `requiretty`; fact gathering runs through `become` before role
tasks start, and sudo rejects the module invocation before Ansible receives
module JSON.

**Remote-host fix:** Keep shipped Ansible config at
`[ssh_connection] pipelining = False` so remote `become` tasks can use the SSH
TTY behavior Ansible applies for sudo.

**Storage node scope:** the Ceph orchestration-account path prints the identical
error string and is **not** covered here — `storage_node_access` hand-builds its
own `ssh` argv, so no connection plugin and no `pipelining` setting is in its
path. See
[ceph-node-access-privileged-channel.md](ceph-node-access-privileged-channel.md).
