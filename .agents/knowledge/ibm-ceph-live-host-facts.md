# IBM Ceph live-host facts

Observed on 2026-10-13 during the owner's M2b run: three RHEL 9.8 x86_64 guests
(podman 5.8.2, OpenSSH 9.9p1, cephadm and ceph-common 20.2.1-405.el9cp,
crypto-policies 20260224, SELinux enforcing, sudo umask 0022, 4 vCPU and
5.6 GiB each) prepared and bootstrapped by `apply --stage clusters`, plus the
image `cp.icr.io/cp/ibm-ceph/ceph-9-rhel9@sha256:893835…` (id `969d01cffae4`)
run on the Fedora 43 controller's rootless podman 5.8.8. Every unit test of
the native Ceph code fakes these tools, so each fact below reached the
operator as a first-call refusal; the slice that records it is named.

## Files and links on the guest

- `/etc/crypto-policies/back-ends/opensshserver.config` is a root-owned
  symbolic link to `/usr/share/crypto-policies/DEFAULT/opensshserver.txt`,
  which has two hard links. RHEL's `/etc/ssh/sshd_config.d/50-redhat.conf`
  includes it, so the sshd include-closure proof met ELOOP under
  `O_NOFOLLOW` (`native-unknown:OSError@ceph_native.native_source:2029`), and
  would have met the one-name rule next. A public root-only source now
  follows a root-owned link, bounded, and admits a package's hard links
  (X75).
- `/var/run` is a symbolic link to `../run`, and cephadm mounts
  `/var/run/ceph/<fsid>` into every daemon by that spelling; podman records
  the source as given. The mount-source identity walk raised
  `NotADirectoryError` on `run` (X76).
- `/var/lib/ceph` is `ceph:ceph 0750` from ceph-common; `/var/log/ceph` is
  `ceph:ceph 3770`; `/etc/ssh/sshd_config.d/*.conf` are root `0600` regular
  files (`50-redhat.conf`, and the managed OS's `60-bootwright.conf`).
- With SELinux enforcing, every osd and ceph-volume container run makes
  `/var/lib/ceph/<fsid>/selinux` (`os.makedirs(…, 0o755)`, root-owned), and
  the manager runs `ceph-volume inventory` on each host right after host
  add, so the cluster-directory inventory refused it from then on (X76).
- cephadm 20.2.1 writes the bootstrap `ceph.conf` with a plain `open(…, 'w')`
  and no umask, so under sudo's 0022 it is `0644`; only the keyring is
  written `0600`. The capture proof (private, root-only) refused after a
  successful bootstrap, and every re-run refused in `prove_seed` (X76).

## Tool output shapes

- `ceph --format json config-key get <key>` prints a blank line before the
  value: `\nabc\n`, where the plain form prints `abc\n` (X76).
- `ceph --format json osd metadata` lists an OSD the map holds before its
  first boot as `[{"id":0}]`, with no `device_ids`; ceph-volume runs
  `osd new` tens of seconds before the daemon first boots (X76).
- cephadm daemon containers run with `--rm`, so a stopped, activating or
  redeploying daemon has no container and `podman container inspect` exits
  125; the daemon directory also exists before `unit.meta` is written, and
  the systemd instance reports `LoadState=not-found` before the template unit
  is installed (X76).
- `podman ps --all --format json --filter name=^<name>$` prints `[]` with no
  match and a list-valued `Names` with one; checked on 5.8.8.
- podman 5.8 adds `HOSTNAME=<short id>` to a started container's
  environment unless one is set, records a bind as
  `<src>:<dst>:ro,z,rprivate,nosuid,nodev,rbind` and drops `z` once the start
  relabelled it, and reports no `HostConfig.Sysctls` at all; a host-network
  container lists no implicit `/etc/hosts` or `resolv.conf` binds in
  `Mounts` (X73).
- `sshd -T -C user=…,host=…,addr=…,laddr=…,lport=…` on 9.9p1 prints every
  line as `keyword value` (no short lines) with the policy keys spelled
  `allowtcpforwarding`, `permitlisten`, `permitopen`,
  `allowstreamlocalforwarding`, `permittunnel`.

## Timing and health

- The bootstrap manager redeploys itself after the service spec adds
  `extra_container_args` ("container cli args changed"); `tunnel_mount` and
  the dashboard's `orch redeploy mgr` restart it too. In those windows
  `ceph orch …` fails or overruns the 60 s default command timeout; a
  manager-served command is now retried, bounded, after `mgr dump` reports a
  new active gid (X76).
- IBM's bootstrap enables `call_home_agent`, which raises
  `CHA_ERROR_SENDING_REPORT` (HEALTH_ERR) when `esupport.ibm.com` is out of
  reach; readiness tolerates exactly that check (X76). The lab's NAT reached
  it (HTTP 200).
- IBM bootstrap restarts the manager three times (cephadm module, call home,
  license); the phase runs under its own 1800 s budget, not the 640 s
  default (X76). `READINESS_TIMEOUT` stays 900 s; a timeout is recovered by
  a later apply's observation.

## How to check before a run

On a guest, as root: `umask`, `stat -c %N /var/run`, `getenforce`,
`grep -i -E '^(match|include)' /etc/ssh/sshd_config /etc/ssh/sshd_config.d/*.conf`,
`sshd -T -C user=<ssh user>,host=<host>,addr=127.0.0.1,laddr=127.0.0.1,lport=22`,
`sudo -l -U <ssh user>`, `df -P /var/lib/ceph`, `chronyc tracking` and
`curl -sS -m 15 -o /dev/null -w '%{http_code}\n' https://esupport.ibm.com/connect/api/v1`.
On the controller, against the image with `podman run --rm --network none
--entrypoint … 969d01cffae4`: read the strings above out of the `cephadm`
zip's `__main__.py`, `cephadmlib/daemons/ceph.py` and
`cephadmlib/container_types.py`, and start a scratch mon to print
`config-key get` and `osd metadata`.
