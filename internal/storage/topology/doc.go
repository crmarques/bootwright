// Package topology owns Ceph storage-cluster domain resolution over a
// v1alpha1.State: failure domain, monitor endpoints, node roles, placement,
// node-to-machine address resolution, pool replication/application/CRUSH-rule
// defaults, and management endpoint/VIP shaping for one StorageCluster.
// Generic cross-kind state lookups by name live in internal/state/view, not
// here.
package topology
