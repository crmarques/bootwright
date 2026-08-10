# Controller DNS missing: machine access or bootstrap cannot start

**Symptoms:** a machines-phase task fails with `Could not resolve hostname`, or
`openshift-install agent wait-for install-complete` eventually reports a
controller lookup failure such as `lookup api.<cluster>.<domain> ... no such
host`.

**Root cause:** Bootwright connects to name-resolution-wired machines by FQDN,
and `openshift-install` polls the API from the controller. Managed dnsmasq can
already publish those records while the controller still has no route to the
managed zone. The original controller route lived inside the later OpenShift
agent role, so storage-only and early machines-phase work reached DNS too soon.

**Implementation:** controller readiness belongs to the managed
`nameResolution` service.

- Desired validation permits one resolved managed DNS service per context.
  Resolution follows actual machine-service consumers, so unused Environment
  catalog entries do not count and compatible aliases of one `InfraComponent`
  remain one service. Distinct consumed managed services fail before planning.
- The apply planner creates one controller activity for that service. Fabric
  supplies the service-ready capability; machines may establish or reconcile
  the route; every machines task depends on the activity, including managed-OS,
  registration, repository, node-access, substrate, and container-machine work.
  A `deps`-, `base`-, or `add-ons`-only run still gets the activity as a live
  proof before its selected mutations, but that proof cannot alter resolver
  state, run apply-mode preflight, or write convergence state.
- Runtime state is narrowed to that exact DNS service. The task's desired hash
  renders the same service projection from the full unscoped desired state, so
  `--clusters` and `--machines` cannot manufacture drift and another service on
  the host cannot be recorded as converged without running.
- The projection carries concrete service addresses, the machine domain, every
  consuming container/storage zone, exact out-of-zone record names, and a probe
  for every rendered host, subtree, and CNAME answer. A wildcard bind uses the
  selected or sole declared service endpoint. No concrete address is a hard
  refusal when Bootwright must install or reconcile an owned route; an unowned
  operator resolver whose complete exact probe set passes needs no such address.
- An already-correct controller resolver is accepted without mutation only when
  no Bootwright-owned route needs reconciliation and no sibling Bootwright route
  exists. An owned route is always reconciled. systemd-resolved records
  ownership before writing one context-and-service-specific drop-in whose
  filename uses a digest of the exact identity. The controller-global
  shared-service mutation lease serializes the write and restart. Current and
  sibling record/drop-in paths must be regular non-symlink files; ambiguous path
  types fail before mutation or removal.
- The owned drop-in is exclusive owner of global systemd-resolved `DNS=` and
  `Domains=` assignments. Effective static assignments outside it refuse; a new
  unowned route requires empty runtime globals; overlapping per-link route
  domains refuse; and post-write global values must exactly match desired state.
  Disjoint per-link routes may coexist. An unchanged healthy route does not
  restart resolved, while an unchanged owned route with failed initial probes
  does restart before the final exact proof to recover stale daemon/cache state.
  Without systemd-resolved, no mutation is attempted and every desired answer
  must already work.
- Failures that may follow partial controller mutation use a mode-aware exact
  invocation: `create` changes to `reconcile`, `reconcile` and `rebuild` remain
  unchanged, and a machines-only range adds required `fabric` while preserving
  its end. A later-only live-proof failure instead prints a fabric-only
  reconcile for the service's complete consumer closure, followed by the exact
  original invocation. The repair cannot run later-phase work and the resume
  cannot widen it. The ledger preserves the original argv for audit while
  recording these CLI-resolved steps at the active task boundary, so failed,
  stale, and cancelled status output retains the same safe recovery. No role
  hand-builds a retry command.
- The managed DNS destroy graph proves controller ownership before any infra
  mutation, keeps the route through machine teardown, and removes it only after
  every task settles and every task naming selected resource identity reports
  `ok`. A skipped/no-host selected task blocks cleanup and retains the route and
  evidence; a truly empty no-op may skip. Cleanup also requires fresh absence of the
  matching infra-component owner record; an ignored unreachable host therefore
  retains the controller route and both records until the exact destroy retry
  proves remote removal. Resolver removal/restart precedes controller evidence
  removal; a failure retains evidence. A `stale-input` or `unreadable-records`
  authorization for skipped evidence disables record-only/context-wide orphan
  sweeps and this controller bracket, leaving skipped resources standing.
- Managed-service identity replacement is not an in-place operation. Keep the
  old service declared and consumed through its successful owning infra destroy,
  then update desired state and apply the replacement. A direct switch meets the
  old route as sibling evidence and fails closed.

The agent role keeps an endpoint-specific `getent hosts` gate as defense in
depth. Bootwright never edits `/etc/hosts`, and automatic controller mutation is
limited to systemd-resolved.

This lifecycle correction does **not** close B-007. It orders controller routing
after the dnsmasq service task, but it does not remove dnsmasq's
`bind-dynamic`/port-53 listener race on a controller-local service host. That
listener-ordering work remains separate and open.
