# A Ceph node that can start no container: kernel lockdown and the eBPF device filter

**Root cause:** on cgroup v2 there is no `devices` controller, so a container
runtime enforces device access with an eBPF `BPF_PROG_TYPE_CGROUP_DEVICE` program.
crun installs it through systemd's transient-unit properties. When the kernel is
**locked down**, it refuses that BPF operation and crun aborts with

```
Error: OCI runtime error: crun: systemd failed to install eBPF device filter on cgroup `/sys/fs/cgroup/machine.slice/libpod-<id>.scope`
```

Lockdown is switched on automatically by **EFI Secure Boot** — `dmesg` says
`Kernel is locked down from EFI Secure Boot mode`, and
`/sys/kernel/security/lockdown` reads `none [integrity] confidentiality`. RHEL 9's
systemd is additionally built `-BPF_FRAMEWORK`, so that path has no margin to begin
with. A second, independent symptom of the same restriction is
`bpftool cgroup tree` answering
`can't query bpf programs attached to /sys/fs/cgroup: Invalid argument` as root.

**Why it looks like a Ceph problem:** cephadm runs
`podman run --entrypoint stat <image> -c '%u %g' /var/lib/ceph` to resolve the ceph
uid/gid *before every single daemon deployment*
(`cephadmlib/container_types.py::extract_uid_gid`). On a locked-down host that call
returns 126, so **no daemon of any type can ever be placed there** — mon, crash,
ceph-exporter and node-exporter fail identically, and even the `cephadm ceph-volume`
inventory probe fails. The cluster reports `CEPHADM_DAEMON_PLACE_FAIL` and
`CEPHADM_REFRESH_FAILED`, neither of which names the runtime as the cause.

**Discriminating it from everything else:** the failure is specific to the systemd
cgroup manager. Re-run the identical probe with `--cgroup-manager=cgroupfs`; if it
prints `167 167`, the kernel and the image are fine and only the systemd BPF path is
blocked. Check in this order, because each rules out a different candidate:
`stat -fc %T /sys/fs/cgroup` (expect `cgroup2fs`), `mount | grep -w bpf` (bpffs
present), `ausearch -m avc -ts today | grep -i bpf` (no SELinux denial),
`rpm -q podman crun container-selinux selinux-policy systemd`, `uname -r` against
the installed kernels, then `cat /sys/kernel/security/lockdown` and
`mokutil --sb-state`.

**When it bites:** typically one odd host out — a VM among bare metal, or the
stretch **arbiter**, which is often a `provided` machine whose OS Bootwright never
built and whose firmware settings therefore differ from the fleet. Its peers deploy
normally with the same image, which makes the failure read as host-specific rather
than as a Ceph or network fault.

**Fix:** `phases/container_runtime.yml` runs on **every** storage node before
`rebuild.yml` and before bootstrap, so the apply fails closed in seconds naming the
host rather than ten minutes later on a daemon-readiness gate. It proves the runtime
by starting a real container from the pinned image; if that fails it retries under
`--cgroup-manager=cgroupfs`, and when only that succeeds it writes
`/etc/containers/containers.conf.d/20-bootwright-ceph-cgroup-manager.conf` selecting
the cgroupfs manager, re-proves the runtime, and reports what it did. If both probes
fail it asserts with the lockdown mode, both probe stderrs, and the runtime package
versions.

The drop-in is written **only** when the default manager provably fails and cgroupfs
provably works — never blanket — and it is **removed** on the next apply once the
default manager works again, so clearing Secure Boot is self-healing. The gate needs
a resolvable `image.version` pin to have something to run; without one it is skipped.

**Accept the trade deliberately:** under the cgroupfs manager the daemons on that
node are not device-isolated by a cgroup BPF program. That is a real, if narrow,
reduction — least bad on an arbiter, which carries mons only and no data. Clearing
Secure Boot on the node is the cleaner fix wherever policy allows it.

**Unrelated but co-located:** a host-installed `node_exporter` listening on 9100
blocks `node-exporter.<host>` with
`TCP Port(s) '0.0.0.0:9100' required for node-exporter already in use`, which keeps
the cluster in `HEALTH_WARN` indefinitely. Bootwright renders no node-exporter, so
this is cephadm's default monitoring stack colliding with a pre-existing agent —
disable the host unit and let cephadm own the port.
