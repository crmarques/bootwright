# ADR 0009: The Renderer Owns the Listening Surface and Dispatch

## Status

Accepted

## Context

Bootwright's Go render layer compiles desired state into installer files,
inventories, and per-component Ansible vars; the Ansible roles then act on
those vars. Two recurring questions decide where logic belongs: who owns a
runtime service's listening surface (ports, bind addresses, protocols), and who
decides which role or code path handles a given substrate. Letting roles
default their own ports or branch on substrate discriminators produced
apply-time mismatches (a BMC reachable on a port the `agentIso.fetchUrl` did
not use) and leaked substrate-specific logic across code paths (emulator
staging surfacing on the bare-metal boot path). ADR 0002 already fixed
role-name dispatch in the registry; this ADR states the complementary
rendering-side rules.

## Decision

**Providers declare placement; the Go layer owns the listening surface.** A
provider says *where* and *how* a service runs; normalize and render own default
protocols, bind addresses, and ports (`DefaultBMC*`, `DefaultSquidPort 3128`,
`DefaultMirrorRegistryPort 5000`, the emulated-BMC listen/vmedia ports, …)
unless a cluster-side component explicitly exposes them. Those defaults are
`api/v1alpha1` constants stamped onto state by normalize, so they are visible in
`effective-state.yaml` and counted in the converge hash rather than invented at
render time. Roles never re-derive a default port; a role reading `default(8000)`
is a bug because the value must come from the Go layer, so the projection and the
consumer cannot drift.

**Render-internal spellings are outputs, not schema.** Values such as
`image.mediaType` (`dvd`/`boot`), the `machines` and `bmc` service kinds, and
the derived Entitlement `provider`/`product` strings exist only in rendered
output to preserve a downstream contract; they are never authorable fields.

**One render body, mode seams only.** `renderCore` is the single body for the
on-disk-context, tool-input, and portable modes; the modes differ only at
declared seams (installer-asset layout, inventory/vars emission, installer
secrets). Secret-independent artifacts (`effective-state.yaml`, the lock) are
emitted by `renderCore` itself so every mode produces them byte-identically.

**Byte-stable output.** Vars maps omit a key rather than emit an empty or false
value, because install-marker hashes and golden fixtures derive from them and a
spurious key is a spurious drift signal. `EffectiveState` is an identity
function over already-normalized state — there is no second overlay.

**Fail closed on unresolved names before writing.** Resolvers degrade to empty
strings on unresolvable names; `checkResolvedNames` runs at every render entry
point before any write, turning a validator gap into a hard error instead of an
install-config with an empty VIP or a silently shortened monitor list.

**Substrate-blind boot projection.** `bootRole` names the protocol driven at
install time, not the substrate. Redfish-driven machines (libvirt via
sushy-emulator, bare-metal via a vendor BMC) receive one fully-resolved
`boot.{redfish,agentIso}` shape and `boot_redfish` never branches on `bmcRole`
or looks up provider components at runtime. Post-install media cleanup
dispatches on the renderer-projected `cleanupMediaRole`, so no play enumerates
boot backends.

## Consequences

- Adding a provider, a service default, or a media-bearing boot backend is a
  renderer/registry change plus roles — never a new conditional inside a
  playbook or a per-role port default.
- Guard tests pin the contracts: `TestRenderCoreSharedOutputsStayInStep` (mode
  parity), the boot-vars tests (substrate-blind projection and emulated-BMC
  port), and the unresolved-name check (fail-closed writes). Detailed traps are
  cataloged under `.agents/knowledge/render-*`.
