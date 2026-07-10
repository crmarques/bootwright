# Redfish boot-flow quirks: retries, discovery, emulated BMC service

Constraints inside `container_cluster_boot_redfish` and the emulated BMC
provider service that are easy to regress. Several are pinned by
`internal/repo/checks/ansible_boot_redfish_test.go`.

**Shared uri defaults:** every `ansible.builtin.uri` request in the role hits
the same BMC with the same credentials/TLS/timeout, supplied once by a
`module_defaults` block in `tasks/main.yml`, so per-task bodies carry only
method/url/headers/body. `no_log` and `status_code` stay per-task — they are
task keywords, not module args.

**Tail-recursive retries; never `until` on a no_log task:** a looped
`include_tasks` enqueues every iteration up front, so a first-attempt success
still prints every inner task as skipped for each spare loop item. And `until`
on a no_log'd task force-fails with a fully censored result on retry
exhaustion. Both the power-state waits (`boot/power_state_wait.yml`) and the
InsertMedia retry (`media/insert_until_attached.yml`) instead seed an attempt
fact and tail-recursively re-include themselves, re-checking the retry
condition only after each attempt completes. Attempt accounting mirrors the old
`range(1, retries + 1)` loop: start at 1, advance after an unsuccessful
attempt, stop once the advanced attempt exceeds the retry budget — exactly
`retries` total attempts, sleeping only between them. A `retries >= 1` guard
preserves the `insert_media_retries: 0` behavior (zero attempts, then the
attach assert fails). Each probe stays a censored uri GET plus a sanitized
capture/debug/assert.

**sushy-tools cold-init lock:** the emulated BMC's VirtualMedia driver is built
lazily on the first GET to `/Systems/<id>/VirtualMedia`; that cold init opens a
shared sqlite state DB under a WAL write lock, so parallel boots race it and
every loser returns HTTP 500 (`database is locked`). The first vmedia probe
runs `throttle: 1`, and backends with that shared cold init project
`redfish.vmediaColdInitRetry=true` so until/retries rides out the residual
lock. Real per-server BMCs share no state, leave the flag unset, and the probe
makes a single best-effort attempt instead of serially burning the retry
window on every host.

**Duplicate VirtualMedia views:** iDRAC-style BMCs expose the same VirtualMedia
resources under both `/Systems/<id>/VirtualMedia` and
`/Managers/<id>/VirtualMedia`; member URLs are `unique`'d on the resolved URL
string so nothing is probed twice. iBMC 404s the system view and is unaffected.

**Multi-system BMCs:** a blade/chassis or some OpenBMC exposes more than one
ComputerSystem; falling through to `Members|first` can target — and boot
destructively — the wrong system. The role warns when multiple systems exist
and none was selected; pin the system by appending `/redfish/v1/Systems/<id>`
to the machine's BMC address (the renderer strips the suffix into the rendered
`boot.redfish.systemId`). First member remains the conventional default.

**Emulated BMC service provisioning (provider_service_bmc_emulated):**

- sushy-tools is installed with the pip module and an explicit `version:`; a
  `command` + `creates:` guard pinned to the sushy-emulator entrypoint never
  reconciled a `bootwright_sushy_tools_version` bump on an already-provisioned
  host. pip reinstalls when the pinned version differs and reports changed.
- `community.libvirt.virt_pool` returns from the `state` branch before it
  reaches autostart handling, so `state: present` + `autostart: true` in one
  call silently drops autostart. Set autostart in its own call (no `state`),
  or the vmedia storage pool comes up inactive after a host reboot and the
  emulated BMC cannot attach virtual media until a re-apply.

**bmcRole=redfish is off-host:** real bare-metal Redfish BMCs are reached
directly from the controller during the install layer; the provider-host
service role for them is an explicit no-op (nothing to install on any provider
host).
