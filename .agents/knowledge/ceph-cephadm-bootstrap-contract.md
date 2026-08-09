# cephadm bootstrap contract: config seeding, flags, and service specs

**Constraint:** `public_network` has no `cephadm bootstrap` flag (unlike
`--cluster-network`), and the first monitor binds at bootstrap. Declared public
CIDRs must be in the initial ceph.conf handed to `cephadm bootstrap --config`;
the `set-public-network` operation keeps the value converged on later applies
(`ceph config set` is last-write-wins).

**Constraint:** Only the `global`/`mon`/`mgr`/`osd` config sections (no masks)
seed into the bootstrap ceph.conf. Their daemons exist at bootstrap, so seeding
them means cephadm's own auto-created pools (e.g. `.mgr`) honor the declared
defaults instead of cephadm defaults the post-bootstrap `ceph config set` ops
would only correct afterward. `mds`/`client`/`<type>.<id>` sections have no
daemon yet and are left to the post-bootstrap ops (which run for every section
regardless).

`mgr` was excluded on the same "no daemon yet" reasoning, and that reasoning was
wrong: `cephadm bootstrap` creates a mgr and enables the `cephadm` module inside
the same command — which is exactly why `CephadmBootstrapSpec` exists, to race
the in-bootstrap mon/mgr placement. With monitoring enabled (the default),
bootstrap then deploys prometheus/grafana/alertmanager/node-exporter **in
process**, from cephadm's compiled-in upstream defaults, long before the
post-bootstrap `set-config-mgr-*` ops run at step 15 of 20. So a disconnected or
IBM cluster that pinned `mgr/cephadm/container_image_*` per the documented
remedy still burned its first pulls against unreachable upstream registries. The
family is additionally re-applied by `bootstrap_steps/container_image_base.yml`
(step 5) with a guarded `ceph config get`, which bounds the exposure to seconds
if a given cephadm build does not assimilate mgr *module* options from
`--config`.

`--skip-monitoring-stack` is NOT an acceptable substitute for seeding, and must
stay conditional on the rendered `skipMonitoringStack` flag (guarded by a test).
`cephadmMonitoringSpecs` skips services with no role and no config and skips
empty placements, so a zero-config cluster renders no monitoring specs at all;
`ceph-exporter` and `crash` are never rendered by Bootwright at any time. Making
the flag unconditional therefore deletes monitoring outright for those clusters,
and — because bootstrap runs once ever — leaves clusters built before and after
the change permanently divergent.

Consequence of seeding the whole section rather than only
`mgr/cephadm/container_image_*`: an operator-authored `config[mgr]` value now
reaches the bootstrap conf, where cephadm sets `mgr_standby_modules` under
`--single-host-defaults` with `if not cp.has_option(...)`. That key is therefore
rejected in `config[mgr]` alongside the three `[global]` keys the same flag owns.

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

**Constraint:** The Ceph root-filesystem budget belongs to
`internal/storage/topology`, not the renderer: inventory, desired-state
validation, and advisories must consume the same calculation. A virtual
provider profile below the absolute 20 GiB floor fails validation before VM or
OS mutation; a raw disk between the floor and the node's computed role/service
budget produces a non-blocking warning. This does not replace live preflight —
`diskGiB` is raw capacity while the host gate measures free space after the OS
lands. The IBM libvirt lab's mon-only profile is 40 GiB for a 35 GiB computed
budget, leaving room above the 20 GiB live free-space floor.

**Constraint:** `ceph orch apply` reconciles to the spec and exposes no stable
changed/ok signal, so the declarative service-spec steps set
`changed_when: true` by design. Convergence drift is computed in Go from the
desired hash vs recorded reality, not from Ansible's recap.

**Constraint:** The cephadm common service-spec keys `unmanaged`,
`extra_container_args`, `extra_entrypoint_args`, `networks`, and
`custom_configs` are top-level keys (siblings of `spec`/`placement`), not
entries inside the daemon/drivegroup spec. Rendering them inside `spec` makes
`ceph orch apply -i` reject the document.

**Constraint:** `ceph orch apply -i <file>` reads the file with
`yaml.safe_load_all` and hands **each document** to `ServiceSpec.from_json`,
which requires a mapping. A file holding a YAML *sequence* of specs is one
document whose value is a list, so the whole apply dies with
`Error EINVAL: Service Spec is not an (JSON or YAML) object. got "[{…}]"`
(rc 22) — the printed text is Python's repr of the loaded list, with the keys in
the alphabetical order `to_nice_yaml` wrote them, which reads misleadingly like
Ansible stringified the fact. The Go renderer joins documents with `---` in
`writeYAMLDocuments`; the two spec files Ansible assembles at run time
(`management_services.yml`, `rgw_ingress_tls.yml`) must do the same with
`| map('to_nice_yaml') | join('---\n')`, never `| to_nice_yaml` over the list.

Because the file then carries several documents, cephadm can reject one and
accept the rest while exiting zero — and a rejected service is never created, so
the service-readiness gate cannot see it (it only compares `running` against
`size` for services `ceph orch ls` reports). Both steps therefore apply with
`failed_when: false` and refuse on a following assert that reads rc plus
stdout/stderr, as the host/mon/mgr spec apply already did. A retried apply
(`management_services.yml`) needs the `attempts >=` escape in its `until`, or
retry exhaustion fails the task before its refusal can name the document.

**Constraint:** RGW/NFS ingress takes cert and key concatenated into one
`ssl_cert` PEM bundle. That join must not be written as `~ "\n" ~` inside the
folded (`>-`) `set_fact` expression: the escape survives as a literal
backslash-n there ([ansible-folded-scalar-escapes.md](ansible-folded-scalar-escapes.md)),
which cephadm accepts as spec text and haproxy then refuses as a certificate.
The bundle is built in a double-quoted task `vars:` entry
(`[cert | trim, key | trim, ''] | join('\n')`), which also gives the trailing
newline; a guard test refuses an escape in the assembly expression.

**Constraint:** `MgmtGatewaySpec` and `OAuth2ProxySpec` take the certificate as
**`ssl_cert` / `ssl_key`** — two separate fields, but the same short names the
ingress bundle uses. `ssl_certificate` / `ssl_certificate_key` is what the
upstream `oauth2-proxy.rst` example still prints and what the mgmt-gateway spec
was originally merged with; the classes were renamed before the feature shipped,
and the stale doc is the trap. The wrong name is refused at spec construction:

```text
Error EINVAL: ServiceSpec: __init__() got an unexpected keyword argument 'ssl_certificate'
```

The `ServiceSpec` prefix is misleading and does **not** mean cephadm failed to
recognize `service_type` — `ServiceSpec.from_json` carries `@handle_type_error`,
which formats the message with the class the classmethod was *called* on, while
the `TypeError` comes from the resolved subclass's `__init__`. Read the keyword,
not the class name. A guard test refuses `ssl_certificate` in the assembly
expression.

**Constraint:** `mgmt-gateway` and `oauth2-proxy` are **Tentacle (v20) and
later**. No Squid release defines either class (`v19.2.3`'s `service_spec.py`
has neither), so `ServiceSpec._cls` falls through to the base class and the
document is refused — as an unexpected keyword argument for whichever gateway
field it reaches first, which names a field and never mentions the release.
A `spec.ceph.mgmtGateway` block therefore needs Ceph 20+, and the floor is
enforced twice because neither half can cover the other:

- **validate**, exactly, for `oss` only: the authored release *is* the Ceph
  version there, so `cephprovider.UpstreamCephMajor` maps a known codename or
  an `x.y.z` to a major and `validateStorageCephMgmtGatewayRelease` refuses
  below `MgmtGatewayMinimumCephMajor`. An unrecognized (future) codename
  derives nothing and is allowed through — the table must never refuse a
  release it simply has not heard of.
- **apply**, authoritatively, for every distribution: a vendor product version
  (`9.9.1.0`) is not a Ceph version and must not be arithmetically mapped to
  one, so `management_services.yml` reads `ceph versions --format json` and
  gates on the **lowest** major among the live `mgr` daemons before assembling
  the spec. A version it cannot read leaves the gate skipped rather than
  blocking the run; the spec-apply refusal still catches it.

A repo check ties the Ansible floor to the Go constant so the two cannot drift.
The `ingress` document the Go renderer emits with
`backend_service: mgmt-gateway` is a plain `IngressSpec` that any release
accepts and then never satisfies; the apply-time gate refuses ahead of it.

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
