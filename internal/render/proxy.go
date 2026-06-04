package render

// Managed-proxy helpers under the new schema. Environment proxy catalog
// entries resolve to InfraComponent proxy services; the renderer derives the
// URL from `(machineRef, port)` via internal/infra/proxy.ManagedProxyURL. The
// cluster-facing URL is
// substituted with the primary network's gateway (via
// internal/infra/proxy.ClusterFacingHostAddress).
//
// This file is intentionally thin; the substantive logic now lives in
// internal/infra/proxy/effective.go (Resolve, ManagedProxyURL) and is
// consumed from installer.go.
