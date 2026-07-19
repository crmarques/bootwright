# cephadm bootstrap contract: config seeding, flags, and service specs

**Constraint:** `public_network` has no `cephadm bootstrap` flag (unlike
`--cluster-network`), and the first monitor binds at bootstrap. Declared public
CIDRs must be in the initial ceph.conf handed to `cephadm bootstrap --config`;
the `set-public-network` operation keeps the value converged on later applies
(`ceph config set` is last-write-wins).

**Constraint:** Only the `global`/`mon`/`osd` config sections (no masks) seed
into the bootstrap ceph.conf. Their daemons exist at bootstrap, so seeding them
means cephadm's own auto-created pools (e.g. `.mgr`) honor the declared defaults
instead of cephadm defaults the post-bootstrap `ceph config set` ops would only
correct afterward. `mgr`/`mds`/`client`/`<type>.<id>` sections have no daemon
yet and are left to the post-bootstrap ops (which run for every section
regardless).

**Constraint:** `--allow-fqdn-hostname` is unconditional, matching IBM's
recommended bootstrap command (`--ssh-user --mon-ip --allow-fqdn-hostname
--registry-json`). cephadm bootstrap refuses a seed whose hostname is an FQDN
without it; the flag is a no-op for short hostnames. The host-spec hostnames
must still equal each node's real hostname.

**Constraint:** `--dashboard-password-noupdate` is unconditional. Bootwright
captures the generated dashboard admin password install-only
(`clusters/<cluster>/secrets/dashboard-password`); the forced first-login
rotation would immediately stale that captured secret and break the documented
recovery workflow. `--single-host-defaults` renders only for a one-host
topology, so a single-node lab reaches `active+clean` (multi-host CRUSH
defaults never would). Its default pool size is two, so validation rejects a
statically countable topology below two OSDs and the readiness gate always
requires at least two in OSDs before creating pools.

**Constraint:** `--image` is a global cephadm option, not a `bootstrap`
subcommand flag. It must sit between the `cephadm` argv[0] and the `bootstrap`
subcommand token (`cephadm --image <ref> bootstrap …`); appended after
`bootstrap` (or any other flag) cephadm exits rc=2 with
`error: unrecognized arguments: --image …`. The image pin flows from
`bootwright_ceph_bootstrap_image`, so its conditional block prepends before the
`bootstrap` list, while every `--mon-ip`/`--config`/`--registry-json`/etc. flag
is a genuine bootstrap subcommand option that follows it.

**Constraint:** `cephadm bootstrap` is a one-time, non-idempotent operation
that refuses to run when `/etc/ceph/ceph.conf` already exists. The role gates
purely on that file — the same marker cephadm itself checks. An
already-bootstrapped node must never be re-bootstrapped (a transiently
unreachable cluster cannot be repaired by re-running bootstrap); a stale conf
from a prior cluster is cleared only by the destroy flow, which owns
`/etc/ceph`.

**Constraint:** cephadm prints the one-time dashboard admin password to stdout
for EVERY provider (not just registry-authed ones), and that stdout is shown on
failure and under `-v`. The bootstrap task is always `no_log`. The `register`
survives `no_log` (which redacts display, not the captured value), so the
password is parsed from the output with
`regex_search('Ceph Dashboard is now available[\s\S]*?Password:\s*(\S+)')`
and persisted controller-side, gated on the bootstrap having actually run —
never re-read or re-synced on later applies.

**Constraint:** IBM Storage Ceph requires time synchronization before
bootstrap: unsynchronized monitor clocks raise clock-skew warnings and can
disrupt quorum. The role waits on chrony (bounded: 6 checks, 10s apart),
non-fatal by design — a node with no reachable NTP source (disconnected site)
still proceeds and cephadm surfaces residual skew as a health warning. Gated on
chronyd actually being a managed service.

**Constraint:** `ceph orch apply` reconciles to the spec and exposes no stable
changed/ok signal, so the declarative service-spec steps set
`changed_when: true` by design. Convergence drift is computed in Go from the
desired hash vs recorded reality, not from Ansible's recap.

**Constraint:** The cephadm common service-spec keys `unmanaged`,
`extra_container_args`, `extra_entrypoint_args`, `networks`, and
`custom_configs` are top-level keys (siblings of `spec`/`placement`), not
entries inside the daemon/drivegroup spec. Rendering them inside `spec` makes
`ceph orch apply -i` reject the document.

**Constraint:** Host identity: authored placement tokens may be machine names;
they are canonicalized to the registered (fully-qualified) hostname so cephadm
matches them against the host spec. The per-host OSD service id stays on the
bare machine name (`data-<machine>`) because it becomes part of daemon/systemd
unit names, where a dotted FQDN is fragile; placement still targets the
registered hostname.

**Constraint:** A management gateway carrying secret material (TLS cert or
oauth2-proxy secrets) is omitted from the static late-services render and
applied by a dedicated step from staged secret files (0600 spec on the seed).
The secret-free keepalive ingress still renders statically, so the generated
native-CLI `apply.sh` prints a `[todo]` warning naming the missing
mgmt-gateway step — the VIP is up but has no backend until the gateway is
applied by `bootwright apply`.
