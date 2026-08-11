# Apply hangs at BMC wait-tasks: stale systemd units holding ports

**Symptom:** `bootwright apply --stage infra` or full `bootwright apply` hangs indefinitely at provider BMC/wait-tasks. Preflight reports a port already in use.

**Root cause:** `bootwright-sushy-<name>`, `bootwright-vmedia-<name>`, and
`bootwright-boot-artifacts-<name>` systemd units from a **prior provider name**
(renamed or removed from the state file) are still running and holding the
BMC/vmedia/boot-artifacts ports. A same-named provider in another Bootwright
context is a different case: its service and vmedia pool are provider-global
resources and must never be adopted or redefined by the new context.

**Fix:** First identify the live owner with:
```
systemctl list-units 'bootwright-*' --all
```
Inspect the unit files, `/var/lib/bootwright/shared-services/bmc-emulator/<provider>/claim.json`,
and `virsh pool-dumpxml bootwright-<provider>-vmedia`. Destroy a current-context
orphan with an unscoped `bootwright destroy`; destroy a foreign service from its
owning context. Stop/disable or remove evidence manually only after proving the
recorded run is inactive and every live identity belongs to the context being
retired.

**Preflight gate:** `bmcPortChecks` (internal/cli/preflight.go) probes these three ports and passes when the port is held by the *expected* unit for the current provider (idempotent re-apply). It fails when an unrelated process holds the port.

**Mutation gate:** apply and destroy include every selected emulated-BMC
consequence in the controller-global shared-service lease and digest-bearing
per-host manifest. The host acquires one unique command operation guard before
base, substrate, provider, or infra shared-service mutation and retains it until
the command-wide finalizer. Before publishing new BMC authority it securely
scans BMC and infra full claims/transitions plus the atomic endpoint registry.
Apply first publishes its reconstructible full BMC claim/transition, then
reserves the complete active-plus-pending endpoint set and acquires sorted
endpoint slots before its first
service-specific package, config, or runtime side effect. The full BMC claim
binds context/provider/host, URI/pool, ports, units, paths, bind/auth
configuration, and the firewall consequence.
Both verbs then require regular non-symlink unit files, exact loaded-systemd
identity, mount-free BMC roots, and exact live libvirt metadata/path, or
positive absence for each member. Interrupted recovery uses the claim's old
composite even when desired state changed. Failed probes, stale/tampered record
fields, foreign or cross-family claims, endpoint collisions, unexpected mounts,
and contradictory members refuse with the exact Bootwright retry; no
authorization token adopts them. A changed-port retry retains both active and
pending endpoints until the old listener, firewall, pool, and paths are proved
clean. If a desired Redfish/vMedia port equals the opposite active listener,
the exact old unit is authority-rechecked and stopped before normal convergence,
so swaps and one-way cross-port moves do not enter an unretryable bind loop.
An older context-only marker without `claim.json` is not enough to recover live
or partial BMC state because it cannot prove the old URI, ports, or firewall
consequence; restore the full claim/owner record from trusted evidence or
inspect and retire that state out of band before retrying.

**Ports probed per provider:**
- Redfish: `bindAddress:port` (default `0.0.0.0:8000`)
- vmedia HTTP: `127.0.0.1:port+1`
- boot-artifacts HTTP: `0.0.0.0:<artifact publisher http.port>`, default
  `8443`
