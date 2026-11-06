package cephcluster

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestClusterSSHKeySeparation(t *testing.T) {
	for _, collision := range []string{"none", "fleet", "access", "host", "cluster"} {
		t.Run(collision, func(t *testing.T) {
			catalog := fixtureCatalog()
			cluster := find(t, catalog, api.CephCluster, "storage")
			key := "cluster-key"
			if collision == "fleet" {
				key = "fleet-key"
			}
			cluster = cluster.WithSpec(cluster.Spec().WithPath(api.StringValue(key), "cephadm", "clusterSSH", "keyRef"))
			catalog = replace(catalog, cluster)
			if collision == "access" || collision == "host" {
				machine := find(t, catalog, api.Machine, "machine-1")
				path := []string{"access", "ssh", "auth", "privateKeyRef"}
				if collision == "host" {
					path = []string{"os", "install", "hostKeyRef"}
				}
				catalog = replace(catalog, machine.WithSpec(machine.Spec().WithPath(api.StringValue(key), path...)))
			}
			if collision == "cluster" {
				catalog = replace(catalog, object(api.CephCluster, "other", cluster.Spec()))
			}
			issues := issuesAt(Validate(cluster, catalog), "$.spec.cephadm.clusterSSH.keyRef")
			if collision == "none" {
				if len(issues) != 0 {
					t.Fatal(issues)
				}
				return
			}
			messages := map[string]string{"fleet": "cluster SSH key must differ from the fleet install key", "access": "cluster SSH key must differ from Machine access keys", "host": "cluster SSH key must differ from Machine host keys", "cluster": "each storage cluster requires a distinct cluster SSH key"}
			if len(issues) != 1 || issues[0].Code != "api.invariant" || issues[0].Message != messages[collision] || issues[0].Remediation == "" {
				t.Fatalf("%s: %#v", collision, issues)
			}
		})
	}
}
