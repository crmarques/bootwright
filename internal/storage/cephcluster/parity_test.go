package cephcluster

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestReplicatedProtectionRefusesOneAtAdmission(t *testing.T) {
	catalog := fixtureCatalog()
	for _, kind := range []api.Kind{api.CephPool, api.CephPlacementPolicy} {
		name := "block"
		if kind == api.CephPlacementPolicy {
			name = "policy"
		}
		original := find(t, catalog, kind, name)
		for _, member := range []string{"size", "minSize"} {
			candidate := original.WithSpec(original.Spec().With("replicated", m(member, 1)))
			issues := Validate(candidate, replace(catalog, candidate))
			found := false
			for _, issue := range issues {
				if issue.Code == "api.invariant" && issue.Field == "$.spec.replicated" && strings.Contains(issue.Message, "at least two") {
					found = true
				}
			}
			if !found {
				t.Errorf("%s %s: %+v", kind, member, issues)
			}
		}
	}
}

func TestMDSRefusesMultipleDaemonsPerHostAtAdmission(t *testing.T) {
	catalog := fixtureCatalog()
	cluster := find(t, catalog, api.CephCluster, "storage")
	service := m("service_type", "mds", "service_id", "files", "placement", m("label", "storage", "count_per_host", 2))
	cluster = cluster.WithSpec(cluster.Spec().With("services", api.ListValue(service)))
	issues := ValidatePartial(cluster, catalog)
	for _, issue := range issues {
		if issue.Field == "$.spec.services[0].placement.count_per_host" && issue.Code == "api.invariant" {
			return
		}
	}
	t.Fatalf("MDS multiplicity admitted: %+v", issues)
}

func TestSMBFilesystemWideBackingCannotCrossClusters(t *testing.T) {
	for _, row := range []struct{ ownWide, peerWide bool }{{true, false}, {false, true}, {true, true}} {
		catalog := smbCatalog(t)
		key, name := "subvolumeRef", "docs"
		if row.peerWide {
			key, name = "filesystemRef", "files"
		}
		catalog = smbAdd(catalog, smbClusterObject("smb-other", "other", m("hosts", smbHosts("node-2"), "count", 1)), smbShareObject("smb-other-docs", "other", "smb-other", "docs", key, name))
		change := smbKeep
		if row.ownWide {
			change = smbChain(smbSpec("subvolumeRef", nil), smbSpec("filesystemRef", "files"))
		}
		issues := smbValidate(t, catalog, "smb-docs", change)
		found := false
		for _, issue := range issues {
			if issue.Code == "api.invariant" && strings.Contains(issue.Message, "smb-other-docs") && strings.Contains(issue.Message, "another SMB cluster") {
				found = true
			}
		}
		if !found {
			t.Errorf("%+v admitted overlapping backing: %+v", row, issues)
		}
	}
}

func TestEnrollmentAddressesMatchFleetSSHAtAdmission(t *testing.T) {
	for _, row := range []struct {
		name                       string
		bootstrap, provided, equal bool
	}{{"bootstrap", true, false, false}, {"ordinary", false, false, false}, {"provided", false, true, false}, {"same address", true, false, true}} {
		t.Run(row.name, func(t *testing.T) {
			catalog := fixtureCatalog()
			cluster := find(t, catalog, api.CephCluster, "storage")
			nodes := cluster.Spec().Get("topology", "nodes").Items()
			index := 1
			field := "$.spec.cephadm.addressRef"
			if row.bootstrap {
				index = 0
				field = "$.spec.cephadm.bootstrap.addressRef"
			}
			node := nodes[index]
			machine := find(t, catalog, api.Machine, node.Get("machineRef").Text())
			address := "198.51.100.11"
			if row.equal {
				address = "192.0.2.11"
			}
			machine = machine.WithSpec(machine.Spec().WithPath(api.ListValue(m("name", "ip", "address", "192.0.2.11"), m("name", "storage", "address", address)), "network", "addresses").WithPath(api.StringValue("ip"), "access", "ssh", "addressRef").WithPath(api.StringValue("operator"), "access", "ssh", "user").WithPath(api.BoolValue(row.provided), "os", "provided"))
			spec := cluster.Spec().WithPath(api.StringValue(nodes[0].Get("name").Text()), "cephadm", "bootstrap", "nodeRef")
			if row.bootstrap {
				spec = spec.WithPath(api.StringValue("storage"), "cephadm", "bootstrap", "addressRef")
			} else {
				spec = spec.WithPath(api.StringValue("storage"), "cephadm", "addressRef").WithPath(api.StringValue("ip"), "cephadm", "bootstrap", "addressRef")
			}
			cluster = cluster.WithSpec(spec)
			issues := Validate(cluster, replace(replace(catalog, machine), cluster))
			found := false
			for _, issue := range issues {
				if issue.Field == field && issue.Code == "api.invariant" && strings.Contains(issue.Message, "fleet SSH") {
					found = true
				}
			}
			if found == row.equal {
				t.Fatalf("address identity refusal=%v issues=%+v", found, issues)
			}
		})
	}
}
