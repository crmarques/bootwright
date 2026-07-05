// Package cephdiff compares a desired StorageCluster (and its pools, placement
// policies, filesystems, and gateways) against the live cluster observed by
// package cephstate, producing a structured, per-facet, per-field Report the CLI
// renders as a git-style diff.
//
// It is the Rosetta stone of `bootwright diff --live`: for each facet it derives
// the desired value the way apply would (reusing internal/storage/topology, the
// same resolver the renderer uses — so a pool's effective size or a rule's
// failure domain match exactly) and compares it to the value Ceph reports. The
// desired side is the diff's "---" baseline and the live cluster is "+++": a
// field Ceph does not match shows as "-desired/+real"; an object declared but
// absent on the cluster is desired-only; an object on the cluster but not
// declared is real-only (the additive-only case, and the candidate `--adopt`
// pulls into desired state).
package cephdiff
