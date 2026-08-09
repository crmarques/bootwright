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
`rebuild.yml` and before bootstrap, including a base-only apply whose
`skip_prereqs` execution flag omits the earlier deps work. The role resolves and
requires the provider image outside that flag before entering the gate. The apply
therefore fails closed naming the host rather than ten minutes later on a
daemon-readiness gate. It proves the runtime
by starting a real container from the pinned image; if that fails it retries under
`--cgroup-manager=cgroupfs`, and when only that succeeds it writes
`/etc/containers/containers.conf.d/20-bootwright-ceph-cgroup-manager.conf` selecting
the cgroupfs manager, re-proves the runtime, and reports what it did. If both probes
fail it asserts with the lockdown mode, both probe stderrs, and the runtime package
versions.

**Backstop:** for any path that reaches bootstrap without the gate having run for a
host, the monmap gate (`bootstrap_steps/mon_readiness.yml`) scans the mon service
events it already collects for `Failed to extract uid/gid` and names the runtime
fault outright, instead of offering the public_network, image-pull and mon-port
causes that do not apply. The signature lives in the `ceph orch ls --service_type mon`
events, not in `ceph log last 100 cephadm`, which in the 2026-08-05 failure held
nothing but reconfigure lines.

The drop-in is written **only** when the default manager provably fails and cgroupfs
provably works — never blanket. Removing it again needs more care than it looks, and
the first cut of this gate got it wrong: it keyed the removal on the ordinary probe,
which runs under whatever manager the drop-in has already selected. On the *next*
apply that probe therefore passed **because** of the drop-in, the gate deleted the
remediation, asserted green anyway, and handed cephadm a node that could start no
container at all. That is exactly how ceph-prd-01 node-07 failed a second time on
2026-08-05, five days after the remediation landed — this time not on the runtime
gate but ten minutes later on the monmap gate, with the podman error buried in the
mon service events. The removal is now keyed on a separate probe that pins
`--cgroup-manager=systemd`, so it asks the kernel rather than the drop-in whether the
node still needs it, and it only runs when the drop-in is actually present. Clearing
Secure Boot stays self-healing, and a node that still depends on the drop-in now
reports it on **every** apply instead of only on the one that wrote it.

A remediation that makes its own precondition stop reproducing cannot be re-tested
through the channel it repaired. Any future auto-remediation here needs the same
shape: probe the unremediated path explicitly before withdrawing the remedy.

The gate needs the resolved provider image to have something to run. An empty image
is a pre-mutation refusal rather than a reason to skip the proof.

**Accept the trade deliberately:** under the cgroupfs manager the daemons on that
node are not device-isolated by a cgroup BPF program. That is a real, if narrow,
reduction — least bad on an arbiter, which carries mons only and no data. Clearing
Secure Boot on the node is the cleaner fix wherever policy allows it.

## The gate's own probes are bounded, and rc=124 is not a refusal

Every `podman run` in `phases/container_runtime.yml` runs under
`timeout --kill-after=10`: 900s for the two probes that can pull the image
(the ordinary one and the cgroupfs retry) and 120s for the two that cannot,
since both are gated on an earlier probe having already succeeded with the
image local. Without a bound the first probe is the single longest-lived
unbounded command in the whole apply — it is the first thing on each node to
run the pinned image, so it is the pull, and a registry that black-holes
stalls it with no output at all. Measured 2026-08-07 on ceph-prd-01 node-04:
31 minutes inside `podman run --entrypoint stat
cp.icr.io/cp/ibm-ceph/ceph-9-rhel9:v9.9.1-21278`, ended only by killing the
process by hand.

Read `rc=124` from any of these probes as "never returned", not "refused".
The assert's diagnostic says so, because the two have identical exit paths and
opposite remedies: 126 with the eBPF message is lockdown and the cgroupfs
drop-in answers it; 124 with no stderr is the registry or a podman that cannot
create its cgroup, and no cgroup manager helps. A 124 on the
`--cgroup-manager=systemd` re-prove keeps the drop-in, which is the safe
direction — it withdraws the remedy only on proof the systemd manager works.

**Unrelated but co-located:** a host-installed `node_exporter` listening on 9100
blocks `node-exporter.<host>` with
`TCP Port(s) '0.0.0.0:9100' required for node-exporter already in use`, which keeps
the cluster in `HEALTH_WARN` indefinitely. Bootwright renders no node-exporter, so
this is cephadm's default monitoring stack colliding with a pre-existing agent —
disable the host unit and let cephadm own the port.
