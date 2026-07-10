# Hand-mirrored logic: BMC address normalization and image pin lookups

**Duplicate-BMC guard mirrors the renderer by hand:** `normalizeBMCAddressKey`
in `internal/state/desired/validate.go` hand-mirrors `normalizeRedfishURL`/
`normalizeRedfishTransport` from `internal/render/inventory/vars_boot.go`
because the leaf desired package cannot import render. It folds the transport
scheme (`redfish+https` / `redfish-virtualmedia+https` / `redfish`), a
trailing `/redfish/v1/Systems/<id>` suffix, and trailing slashes into one
comparison key, and deliberately does NOT normalize ports, matching the
renderer. If the renderer's normalization changes, this duplicate-BMC guard
must be updated by hand — otherwise two equivalent spellings of one endpoint
bypass the wrong-host disk-wipe guard.

**componentImages override must not re-enter the pin table:** when reflecting
a `componentImages` override into the bill of materials
(`internal/render/inventory/components.go`), resolve the override directly —
`managedServiceImage`'s version lookup re-enters `ComponentPins`, causing
infinite recursion. The override is reflected so a disconnected operator
auditing the lock file sees the reference actually pulled, not the superseded
upstream default.

**imageRefTag parsing:** `imageRefTag` splits on the final path segment
before looking for `:` so a registry `host:port` is never mistaken for an
image tag.
