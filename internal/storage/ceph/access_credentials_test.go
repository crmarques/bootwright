package ceph

import (
	"slices"
	"testing"
)

// A Ceph host presents only its cluster's registry login to authenticate; the
// cluster and host keys it is given are effect material, so a rotated registry
// login alone may be rebound.
func TestAHostReadsOnlyTheRegistryLoginAsAnAccessCredential(t *testing.T) {
	e, r, _ := hostExecution(t, "node-2")
	access, err := NewHost(nil).AccessCredentials(e.Block)
	if err != nil || r.Cluster.RegistryRef == "" || !slices.Equal(access, []string{r.Cluster.RegistryRef}) {
		t.Fatalf("access credentials = %v (%v), want only the registry login %q", access, err, r.Cluster.RegistryRef)
	}
	if slices.Contains(access, r.Cluster.ClusterKeyRef) {
		t.Fatalf("the cluster key %q was read as an access credential", r.Cluster.ClusterKeyRef)
	}
}
