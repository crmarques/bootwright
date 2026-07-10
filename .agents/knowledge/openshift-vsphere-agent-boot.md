# vSphere agent boot constraints

**Constraint (full power cycle):** `container_cluster_boot_vsphere/tasks/power.yml`
always forces `state: powered-off` (with `force: true`) and then
`state: powered-on`. A plain `powered-on` is a no-op on a VM that the media
role hot-swapped the CD on while it was still running from a failed or
half-installed attempt — the node never reboots into the freshly
(re-)attached installer ISO and the play hangs in the readiness wait. Powering
off first is idempotent on the freshly created, already-off VM of a first
boot. This mirrors the Redfish driver's ForceOff → wait Off → power On
semantics; do not "optimize" it to a single power-on.

**Constraint (readiness over SSH):** Agent ISOs carry no VMware Tools, so
install progress cannot be observed through vSphere guest info. Readiness
probing is over SSH, exactly like the Redfish driver — the substrate-blind
boot contract.

**Constraint (ISO staging and duplicate upload):** The cluster flow generates
one agent ISO per cluster in the local install work dir and stages it at the
rendered per-provider vmedia path, so the media role and ownership cleanup see
one uniform layout (the managed-OS flow arrives with its install ISO already
staged there). `vsphere_copy` re-uploads the full ISO on every call; the
attachment fact set by the media role is what skips the duplicate upload when
the managed-OS flow already ran the media role before this boot driver.

**Constraint (external LB vs integrated VIPs):** When a cluster's
`api`/`api-int`/ingress endpoints come from an external load balancer (e.g. a
bastion haproxy InfraComponent), the installer render must keep the vSphere
integrated load balancer out of `platform.vsphere` `apiVIPs`/`ingressVIPs` —
otherwise keepalived inside the cluster fights the external LB for the same
address. See `vipsFromEndpoints` in
`internal/render/installer/installer_platform.go`.

**Constraint (manual MAC range):** vCenter accepts manually-assigned VM NIC
MACs only inside `00:50:56:00:00:00`–`00:50:56:3f:ff:ff`. Bootwright's
deterministic MAC derivation (`internal/render/installer/mac.go`) masks the
first hashed byte with `& 0x3f` to stay in that range. Any change to MAC
generation must preserve the mask or vCenter rejects the NIC.

**Constraint (physical NICs only):** Only physical NICs get a generated MAC
and a fabricated VM NIC on the substrate. A bond/VLAN (or other virtual
NMState interface type) is created *inside* the guest by NMState —
materialising it as a substrate NIC would collide with the guest interface of
the same name and stamp a bogus MAC on it. An empty NMState interface type is
treated as physical (ethernet).
