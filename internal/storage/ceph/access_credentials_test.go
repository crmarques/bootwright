package ceph

import (
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/secrets"
)

// A Ceph host presents only its cluster's registry login to authenticate; the
// fleet key it dials with, the cluster key and the host keys it is given are
// effect material, so a rotated registry login alone may be rebound.
func TestAHostReadsOnlyTheRegistryLoginAsAnAccessCredential(t *testing.T) {
	e, r, _ := hostExecution(t, "node-2")
	classes, err := NewHost(nil).SecretClasses(e.Block)
	if err != nil || r.Cluster.RegistryRef == "" || classes.Of(r.Cluster.RegistryRef) != secrets.AccessCredential {
		t.Fatalf("classes = %v (%v), want the registry login %q as an access credential", classes, err, r.Cluster.RegistryRef)
	}
	if access := classes.Named(secrets.AccessCredential); !slices.Equal(access, []string{r.Cluster.RegistryRef}) {
		t.Fatalf("access credentials = %v, want only the registry login %q", access, r.Cluster.RegistryRef)
	}
	if classes.Of(r.Cluster.ClusterKeyRef) != secrets.EffectMaterial {
		t.Fatalf("the cluster key %q is %q, want effect material", r.Cluster.ClusterKeyRef, classes.Of(r.Cluster.ClusterKeyRef))
	}
	for _, host := range r.Cluster.Hosts {
		if classes.Of(host.SSHKeyRef) != secrets.EffectMaterial || host.HostKeyRef != "" && classes.Of(host.HostKeyRef) != secrets.EffectMaterial {
			t.Fatalf("host %s: SSH key %q is %q and host key %q is %q", host.Name, host.SSHKeyRef, classes.Of(host.SSHKeyRef), host.HostKeyRef, classes.Of(host.HostKeyRef))
		}
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
	classes, err := NewHost(nil).SecretClasses(e.Block)
	if err != nil {
		t.Fatal(err)
	}
	references := r.SecretReferences()
	if classes.Of("node-known-hosts") != secrets.EffectMaterial {
		t.Fatalf("a host's known_hosts pin, written into the cluster's tunnel, is read as %q", classes.Of("node-known-hosts"))
	}
	for _, anchor := range []string{"registry-ca", "repository-ca"} {
		if !slices.Contains(references, anchor) {
			t.Fatalf("the host does not reference trust anchor %q: %v", anchor, references)
		}
		if classes.Of(anchor) != secrets.TrustAnchor {
			t.Fatalf("trust anchor %q is read as %q", anchor, classes.Of(anchor))
		}
	}
}
