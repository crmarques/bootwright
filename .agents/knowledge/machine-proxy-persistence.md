# machine_proxy persistence rules and stale-state cleanup

The `machine_proxy` role consumes the render-projected `bootwright_proxy`
fact (the resolved effective proxy; Squid credentials arrive as
`bootwright_proxy_credentials_ref`, a controller-side 0600 path under the
secrets dir, slurped from localhost). These rules govern how it persists —
and refuses to persist — proxy state on managed hosts.

**Never write `proxy=` into dnf.conf/yum.conf:** those files have no
`no_proxy` directive, so a config-file proxy would force `noProxy` hosts
through the proxy too. dnf/yum take `http(s)_proxy`/`no_proxy` from the task
environment (`bootwright_proxy_env`, applied play-wide in the storage
apply/destroy playbooks; libcurl honors `no_proxy`). Guard: any `lineinfile`
touching `/etc/dnf/dnf.conf` or `/etc/yum.conf` may only STRIP a `proxy=`
line (`state: absent`, never `line:`), and the strip runs whenever the file
exists so a `proxy=` line written by an older bootwright is removed on the
next apply. pip config and the proxy TCP reachability probe stay gated on an
actual proxy URL (`bootwright_proxy_has_url`).

**Strip bare proxy URLs from /etc/environment:** uncredentialed proxy URLs
left there by older runs take precedence over `/root/.pip/pip.conf`, which
made pip skip sending `Proxy-Authorization` and fail with `407` against an
authenticated proxy.

**No containers.conf.d proxy drop-in (general managed nodes):** podman reads
containers.conf for rootless users too, so a `0600` drop-in breaks non-root
`podman` (even `podman info`) while `0644` leaks proxy credentials. Containers
receive `HTTP_PROXY` via the play-level `bootwright_proxy_env` instead.
persist.yml also restores `0644` on containers.conf because older bootwright
versions locked it to `0600`.

**Exception — dedicated Ceph storage nodes DO get a drop-in:**
`storage_cluster_cephadm/tasks/phases/repository.yml` writes
`/etc/containers/containers.conf.d/10-bootwright-ceph-proxy.conf` (`[engine]
env` with `HTTP(S)_PROXY`/`NO_PROXY`, mode `0600` when the proxy URL is
credentialed else `0644`; removed when no proxy URL). Required because the
cephadm mgr pulls OSD/daemon images by SSHing to each host and running `podman`
as **root, non-interactively** — that path does NOT inherit the play-level
`bootwright_proxy_env` (only the seed's Ansible-invoked `cephadm bootstrap`
does), so without a node-persistent source the per-host pulls dial the registry
directly and time out (0 OSDs). Safe here because a dedicated storage node runs
podman only as root: `0600` stays root-only (no rootless `podman` to break, no
credential leak). This is the one place the "no drop-in" rule above is
deliberately reversed.

**Probe proxy TCP reachability before dnf:** dnf's `--setopt=timeout` does
not cover the TCP connect phase, so a silently dropped SYN toward a dead
proxy hangs the first dnf task indefinitely. machine_proxy probes TCP
reachability explicitly (fail fast) before any dnf work.

**Disabled proxy must actively clear stale state:** (1) facts.yml blanks the
proxy env vars explicitly (empty strings, not `{}`) to override stale
`HTTP(S)_PROXY` exports inherited from sshd/systemd of a prior enabled run;
(2) persist.yml strips the systemd `DefaultEnvironment` artifacts — otherwise
sshd sessions keep injecting `HTTPS_PROXY` and later Ansible runs hit the
now-absent proxy and fail with `407`.
