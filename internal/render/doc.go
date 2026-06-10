// Package render projects validated desired state into deterministic installer
// inputs, Ansible inventory and vars, manifests, runtime locks, and effective
// state views.
//
// The emission families live in the installer, inventory, and ceph
// subpackages; this root package owns every filesystem write and re-exports
// the externally consumed surface (see facade.go).
//
// Managed-proxy helpers under the new schema: environment proxy catalog
// entries resolve to InfraComponent proxy services; the renderer derives the
// URL from `(machineRef, port)` via internal/infra/proxy.ManagedProxyURL. The
// cluster-facing URL is substituted with the primary network's gateway (via
// internal/infra/proxy.ClusterFacingHostAddress). The substantive logic lives
// in internal/infra/proxy/effective.go (Resolve, ManagedProxyURL) and is
// consumed from the installer subpackage.
package render
