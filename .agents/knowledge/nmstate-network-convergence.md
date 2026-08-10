# Managed-OS network convergence via nmstate

**Design:** the install-time kickstart brings up only the routed interface
without an authored MTU — a merged bond+VLAN line cannot set the parent bond
MTU, while separate lines are invalid RHEL 9 Kickstart — and after the node is
up and SSH-verified the full desired-state nmstate is applied, realizing every
interface, VLAN, route, and MTU. The renderer injects the `nmstate` package
into install packages whenever a desired network state exists. Runs every
converge and reconciles drift idempotently; requires root plus the
NetworkManager backend (never `--kernel`).

**Rollback guard:** `nmstatectl apply --no-commit --timeout` stages the
state behind a NetworkManager checkpoint owned by the NM daemon — NOT by the
nmstatectl process or its SSH/D-Bus connection (NM has no
rollback-on-disconnect) — so it survives the applying session closing. A
SEPARATE controller-to-node SSH performs the commit; if the reconfigure
severs the control path the commit never lands and the node auto-reverts.
The commit SSH fails fast on `ConnectTimeout` so the play fails loudly
rather than leaving the node unreachable. nmstate also verifies the applied
state (including MTU on every layer) and self-rolls-back on verification
failure, and rejects overlapping checkpoints, so any stale checkpoint from
an interrupted prior run is rolled back before staging. Lives in
`machine_os_install_anaconda/tasks/configure_network.yml`.

**interfaceAddresses family gotcha:** the NMState writer defaults an
`interfaceAddresses` entry's `family` to `ipv4`, so an IPv6 literal with
`family` omitted lands in the ipv4 block and renders an invalid NMState
document that fails only at agent boot. Validation therefore requires the
resolved literal's family to match the declared-or-defaulted family
(`interfaceAddressFamilyLabel` renders an empty family as `ipv4`).

**Physical interfaces only get substrate NICs:** only physical NMState
interfaces (`ethernet`, or empty/untyped which is treated as physical) get a
generated MAC and a fabricated substrate VM NIC. Bond/vlan and other virtual
interfaces are created inside the guest by NMState — materializing one as a
substrate NIC would collide with the guest interface of the same name and
stamp a bogus MAC on it (`internal/render/installer/network_config.go`).

**`SetInterfaceAddress` forces `enabled: true`:** NMState templates commonly
author the opposite family as `{enabled: false}` (e.g. ipv6 disabled), and
injecting a static address into a disabled family yields an inert document that
nmstate ignores — leaving the node addressless while the agent rendezvous IP
still points at the never-configured address. A family carrying a static
address is enabled by definition, so the injector force-sets `enabled: true` on
it. Pinned by `TestSetInterfaceAddressEnablesDisabledFamily`.

**`EffectiveConfig` panics on a merge error, by design:** `mergo.Merge` errors
only when its top-level contract is violated (non-pointer dst or mismatched
dst/src types), and `internal/nmstate` always feeds a `*map[string]any` dst
with a `map[string]any` src, so the error is unreachable for these inputs
(nested conflicts overwrite under `mergo.WithOverride`). `EffectiveConfig`
cannot surface an error through its signature and its callers pre-check via the
validation phase, so panicking loudly beats silently dropping the override merge
the package exists to protect. `nmstate_audit_test.go` pins the no-error
contract so a future mergo change that starts erroring is caught.
