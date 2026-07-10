# Managed-OS kickstart network stanza and nmstate rendering rules

Rendering rules behind the managed-OS network split (kickstart minimal, full
nmstate post-install). The convergence flow itself — routed-interface-only
kickstart, `nmstatectl apply --no-commit` checkpoint/commit rollback guard —
is in nmstate-network-convergence.md; this file records what gets RENDERED
into each side.

**Kickstart stanza shape:** the install-time kickstart carries exactly one
merged bond+VLAN stanza for the routed interface: `--bondslaves` builds the
bond and its port connections (no separate per-slave stanzas), `--device`
names the parent bond, and the static IP settings apply to the VLAN device.
The merged line cannot set the bond MTU, so MTU and every other interface
never appear in the kickstart — they are configured post-install from
`osInstall.network.desiredState`, which carries every VLAN (including
secondary/cluster VLANs that never reach the kickstart), MTU on every layer,
and the cluster-network IPs.

**MAC identity on bond members only:** each rendered ethernet port carries the
machine's authored (permanent) MAC and `identifier: mac-address`. Applied to an
already-running OS, every bond member's RUNNING MAC is the shared bond MAC, so
verifying an authored permanent MAC by kernel name always fails and rolls the
apply back; mac-address identity makes nmstate match on the permanent MAC and
verify the in-config value instead. The bond and VLAN layers own no MAC and
must NOT be marked — `identifier: mac-address` requires a mac-address match
key. Pinned by TestManagedOSInstallVarsFromCephBaremetalFixture
(internal/render/inventory/managed_os_baremetal_test.go), which is also the
multi-VLAN + jumbo golden for this flow.

**Inventory shape for bare metal:** bare-metal managed-OS nodes carry no
substrate provider host; the install is driven from the controller
(`ansible_connection: local`) with `provider_host_name` equal to the
component's `machineRef`. They must still land in the cluster's managed-OS
inventory group — an empty group means the planned machines-phase install task
is silently skipped at runtime with "no hosts to target".
