package render

// Managed-proxy helpers under the new schema. Environment proxy catalog
// entries resolve to InfraComponent proxy services; the renderer derives the
// URL from `(hostRef, port)` via internal/proxy.ManagedProxyURL. The
// cluster-facing URL is
// substituted with the primary network's gateway (via
// internal/proxy.ClusterFacingHostAddress), which is substrate-blind
// — it reads whichever connectivity arm the network declares.
//
// This file is intentionally thin; the substantive logic now lives in
// internal/proxy/effective.go (Resolve, ManagedProxyURL) and is
// consumed from installer.go.
