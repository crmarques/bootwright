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

func TestAHostRebindsNoTrustAnchor(t *testing.T) {
	e, r, _ := hostExecution(t, "node-2")
	r.Cluster.RegistryTrustBundleRef = "registry-ca"
	r.Cluster.Packages.PackageRepositoryTrustBundleRef = "repository-ca"
	r.Cluster.Hosts[0].KnownHostsRef = "node-known-hosts"
	canonical, err := r.canonical()
	if err != nil {
		t.Fatal(err)
	}
	e.Block.Request = canonical
	access, err := NewHost(nil).AccessCredentials(e.Block)
	if err != nil {
		t.Fatal(err)
	}
	references := r.SecretReferences()
	for _, anchor := range []string{"registry-ca", "repository-ca", "node-known-hosts"} {
		if !slices.Contains(references, anchor) {
			t.Fatalf("the host does not reference trust anchor %q: %v", anchor, references)
		}
		if slices.Contains(access, anchor) {
			t.Fatalf("trust anchor %q was read as an access credential: %v", anchor, access)
		}
	}
}
