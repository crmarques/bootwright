// Package cephstate is the read-only observation model for a live managed Ceph
// cluster. The discover_storage_state playbook writes one JSON blob per `ceph
// ... --format json` read into a controller-side per-cluster directory; this
// package loads those blobs into a Discovery and decodes the facets (services,
// hosts, devices, OSD tree, pools, CRUSH rules, config, mgr modules, health)
// into typed Go structs whose field names mirror Ceph's own JSON.
//
// It is the "real state" side of `bootwright diff --live`: the desired side is
// rendered by internal/render/ceph, and internal/storage/cephdiff compares the
// two. This package is a pure leaf — it parses bytes and never contacts a
// cluster (internal/converge owns running the playbook) — so it is unit-tested
// against captured JSON without any live Ceph.
package cephstate
