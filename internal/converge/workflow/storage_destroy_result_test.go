package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/ownership"
)

func storageDestroyResult(name string, completed, skipped []string) StorageDestroyResult {
	zero := 0
	nodes := make([]StorageDestroyNodeResult, 0, len(completed)+len(skipped))
	for _, node := range completed {
		nodes = append(nodes, StorageDestroyNodeResult{
			Name: node, Host: "storage__" + name + "__" + node, Outcome: storageDestroyOutcomeCompleted,
			ProofVersion: storageDestroyProof, ScanScope: storageDestroyScanScope, ScanDigest: strings.Repeat("0", 64),
			ScannedRows: &zero, OwnedSurvivors: &zero, LVMScanRC: &zero, CompletionRC: &zero,
		})
	}
	for _, node := range skipped {
		nodes = append(nodes, StorageDestroyNodeResult{
			Name: node, Host: "storage__" + name + "__" + node, Outcome: storageDestroyOutcomeSkipped,
			AbsenceClass: storageDestroyAbsenceSSHUnreachable, Reason: node + ": connection timed out",
		})
	}
	return StorageDestroyResult{
		SchemaVersion: 1,
		Clusters: []StorageDestroyClusterResult{{
			Name:  name,
			Nodes: nodes,
		}},
	}
}

func TestValidateStorageDestroyResultsRequiresExactPositiveCoverage(t *testing.T) {
	expected := map[string][]string{"ceph-a": {"a1", "a2"}}
	valid := storageDestroyResult("ceph-a", []string{"a1"}, []string{"a2"})
	if _, err := ValidateStorageDestroyResults([]StorageDestroyResult{valid}, expected, true); err != nil {
		t.Fatalf("valid result: %v", err)
	}

	tests := []struct {
		name   string
		result StorageDestroyResult
		want   string
	}{
		{name: "empty", result: StorageDestroyResult{}, want: "schemaVersion"},
		{name: "wrong cluster", result: storageDestroyResult("ceph-b", []string{"a1", "a2"}, nil), want: "unexpected cluster"},
		{name: "missing expected node", result: storageDestroyResult("ceph-a", []string{"a1"}, nil), want: "missing: a2"},
		{name: "unknown completed node", result: storageDestroyResult("ceph-a", []string{"a1", "a3"}, []string{"a2"}), want: "unexpected: a3"},
		{name: "duplicate node", result: storageDestroyResult("ceph-a", []string{"a1", "a1"}, []string{"a2"}), want: "duplicate a1"},
		{name: "conflicting outcomes", result: storageDestroyResult("ceph-a", []string{"a1", "a2"}, []string{"a2"}), want: "duplicate a2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ValidateStorageDestroyResults([]StorageDestroyResult{tt.result}, expected, true)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want text %q", err, tt.want)
			}
		})
	}

	incomplete := storageDestroyResult("ceph-a", []string{"a1"}, []string{"a2"})
	incomplete.Clusters[0].Nodes[0].Outcome = "incomplete"
	if _, err := ValidateStorageDestroyResults([]StorageDestroyResult{incomplete}, expected, true); err == nil || !strings.Contains(err.Error(), "is not terminal") {
		t.Fatalf("incomplete result error = %v", err)
	}
	if _, err := ValidateStorageDestroyResults([]StorageDestroyResult{valid}, expected, false); err == nil || !strings.Contains(err.Error(), "unreachable-nodes authorization") {
		t.Fatalf("unauthorized skipped result error = %v", err)
	}
	whitespace := storageDestroyResult("ceph-a", []string{"a1", "a2"}, nil)
	whitespace.Clusters[0].Nodes[0].Name = " a1"
	if _, err := ValidateStorageDestroyResults([]StorageDestroyResult{whitespace}, expected, false); err == nil || !strings.Contains(err.Error(), "not canonical") {
		t.Fatalf("whitespace identity error = %v", err)
	}
}

func TestValidateStorageDestroyResultsBindsProofToEachNode(t *testing.T) {
	expected := map[string][]string{"ceph-a": {"a1", "a2"}}
	tests := []struct {
		name string
		edit func(*StorageDestroyNodeResult)
		want string
	}{
		{name: "wrong host", edit: func(node *StorageDestroyNodeResult) { node.Host = "storage__ceph-a__wrong" }, want: "host ="},
		{name: "wrong proof", edit: func(node *StorageDestroyNodeResult) { node.ProofVersion = "ceph-lvm-quiet-v1" }, want: "proofVersion"},
		{name: "survivor", edit: func(node *StorageDestroyNodeResult) { one := 1; node.OwnedSurvivors = &one }, want: "ownedSurvivors"},
		{name: "missing row count", edit: func(node *StorageDestroyNodeResult) { node.ScannedRows = nil }, want: "scannedRows = <missing>"},
		{name: "missing survivor count", edit: func(node *StorageDestroyNodeResult) { node.OwnedSurvivors = nil }, want: "ownedSurvivors = <missing>"},
		{name: "missing digest", edit: func(node *StorageDestroyNodeResult) { node.ScanDigest = "" }, want: "scanDigest"},
		{name: "missing scan", edit: func(node *StorageDestroyNodeResult) { node.LVMScanRC = nil }, want: "<missing>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := storageDestroyResult("ceph-a", []string{"a1", "a2"}, nil)
			tt.edit(&result.Clusters[0].Nodes[1])
			_, err := ValidateStorageDestroyResults([]StorageDestroyResult{result}, expected, false)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestReadStorageDestroyResultIsStrict(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, StorageDestroyResultFileName)
	if _, found, err := ReadStorageDestroyResult(path); err != nil || found {
		t.Fatalf("missing result found=%t err=%v", found, err)
	}
	for _, tt := range []struct {
		name string
		body string
		want string
	}{
		{name: "malformed", body: "{", want: "decode"},
		{name: "unknown field", body: `{"schemaVersion":1,"clusters":[],"unknown":true}`, want: "unknown field"},
		{name: "trailing value", body: `{"schemaVersion":1,"clusters":[]} {}`, want: "multiple JSON values"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(tt.body), 0o600); err != nil {
				t.Fatal(err)
			}
			_, _, err := ReadStorageDestroyResult(path)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestValidateStorageDestroyResultsRequiresEveryClusterExactlyOnce(t *testing.T) {
	expected := map[string][]string{"ceph-a": {"a1"}, "ceph-b": {"b1"}}
	a := storageDestroyResult("ceph-a", []string{"a1"}, nil)
	if _, err := ValidateStorageDestroyResults([]StorageDestroyResult{a}, expected, false); err == nil || !strings.Contains(err.Error(), "missing cluster ceph-b") {
		t.Fatalf("missing report error = %v", err)
	}
	if _, err := ValidateStorageDestroyResults([]StorageDestroyResult{a, a}, expected, false); err == nil || !strings.Contains(err.Error(), "more than once") {
		t.Fatalf("duplicate report error = %v", err)
	}
}

func TestStorageDestroyExpectedNodesUsesManagedSelectedTopology(t *testing.T) {
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{
		{
			Metadata: v1alpha1.Metadata{Name: "ceph-a"},
			Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{Cephadm: v1alpha1.StorageCephadmSpec{Bootstrap: v1alpha1.StorageCephadmBootstrap{Node: "a2"}}, Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{
				{MachineRef: v1alpha1.LocalObjectReference{Name: "a2"}},
				{MachineRef: v1alpha1.LocalObjectReference{Name: "a1"}},
			}}}},
		},
		{
			Metadata: v1alpha1.Metadata{Name: "ceph-external"},
			Spec:     v1alpha1.StorageClusterSpec{Management: v1alpha1.StorageClusterManagementExternal, Ceph: &v1alpha1.StorageClusterCephSpec{}},
		},
	}}
	got := StorageDestroyExpectedNodes(state, []string{"ceph-a", "ceph-external"})
	if len(got) != 1 || strings.Join(got["ceph-a"], ",") != "a1,a2" {
		t.Fatalf("expected nodes = %v, want ceph-a:[a1 a2]", got)
	}
	seeds := StorageDestroyExpectedSeedHosts(state, []string{"ceph-a", "ceph-external"})
	if seeds["ceph-a"] != "storage__ceph-a__a2" {
		t.Fatalf("expected seed hosts = %v", seeds)
	}
}

func TestStorageDestroyExpectedNodesForLedgerRequiresAPlannedStorageTask(t *testing.T) {
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{{
		Metadata: v1alpha1.Metadata{Name: "ceph-a"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{Cephadm: v1alpha1.StorageCephadmSpec{Bootstrap: v1alpha1.StorageCephadmBootstrap{Node: "a1"}}, Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{{
			MachineRef: v1alpha1.LocalObjectReference{Name: "a1"},
		}}}}},
	}}}
	infraOnly := RunLedger{Tasks: []TaskLedgerEntry{{Kind: DestroyTaskKindMachineInfra, ResourceKeys: []string{"ceph-a"}}}}
	if got := StorageDestroyExpectedNodesForLedger(state, infraOnly); len(got) != 0 {
		t.Fatalf("infra-only ledger expected storage proof for %v", got)
	}
	withStorage := RunLedger{Tasks: []TaskLedgerEntry{{Kind: DestroyTaskKindStorageCluster, ResourceKeys: []string{"ceph-a", DestroyMachineResourceKeyPrefix + "a1"}}}}
	if got := StorageDestroyExpectedNodesForLedger(state, withStorage); strings.Join(got["ceph-a"], ",") != "a1" {
		t.Fatalf("storage ledger expected nodes = %v, want ceph-a:[a1]", got)
	}
	if got := StorageDestroyExpectedSeedHostsForLedger(state, withStorage); got["ceph-a"] != "storage__ceph-a__a1" {
		t.Fatalf("storage ledger expected seed hosts = %v", got)
	}
}

func TestReconcileStorageDestroyOwnershipKeepsClusterLocalPartialEvidence(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ownership")
	for _, name := range []string{"ceph-a", "ceph-b"} {
		seed := map[string]string{"ceph-a": "storage__ceph-a__a1", "ceph-b": "storage__ceph-b__b1"}[name]
		if err := ownership.SaveResource(dir, ownership.ResourceRecord{
			Kind: string(ownership.KindStorageCluster), Name: name, Cluster: name, Host: seed,
			Attributes: map[string]string{"seedHost": seed},
		}); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	results := map[string]StorageDestroyClusterResult{
		"ceph-a": storageDestroyResult("ceph-a", nil, []string{"a1"}).Clusters[0],
		"ceph-b": storageDestroyResult("ceph-b", []string{"b1"}, nil).Clusters[0],
	}
	if err := ReconcileStorageDestroyOwnership(dir, "", results, map[string]string{
		"ceph-a": "storage__ceph-a__a1",
		"ceph-b": "storage__ceph-b__b1",
	}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	records, err := ownership.LoadContext(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Name != "ceph-a" {
		t.Fatalf("records = %+v, want only ceph-a", records)
	}
	if got := records[0].Attributes[storageDestroyStatusAttr]; got != storageDestroyStatusPartial {
		t.Fatalf("destroy status = %q", got)
	}
	if got := records[0].Attributes[storageDestroySkippedNodesAttr]; got != "a1" {
		t.Fatalf("skipped nodes = %q, want a1", got)
	}
}

func TestReconcileStorageDestroyOwnershipTargetsOnlyTheExactOwner(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ownership")
	owner := ownership.ResourceRecord{
		Kind: string(ownership.KindStorageCluster), Name: "ceph-a", Cluster: "ceph-a", Context: "ctx", Host: "storage__ceph-a__a1",
		Attributes: map[string]string{"seedHost": "storage__ceph-a__a1"},
	}
	reference := ownership.ResourceRecord{
		Kind: string(ownership.KindStorageCluster), Name: "ceph-a", Cluster: "ceph-a", Context: "ctx", Role: ownership.RoleReference,
	}
	for _, record := range []ownership.ResourceRecord{owner, reference} {
		if err := ownership.SaveResource(dir, record); err != nil {
			t.Fatal(err)
		}
	}
	result := storageDestroyResult("ceph-a", []string{"a1"}, nil).Clusters[0]
	if err := ReconcileStorageDestroyOwnership(dir, "ctx", map[string]StorageDestroyClusterResult{"ceph-a": result}, map[string]string{"ceph-a": "storage__ceph-a__a1"}); err != nil {
		t.Fatal(err)
	}
	records, err := ownership.LoadContext(dir, "ctx")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || !records[0].IsReference() {
		t.Fatalf("records = %+v, want only the reference", records)
	}
}

func TestReconcileStorageDestroyOwnershipRefusesAContradictoryOwner(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ownership")
	if err := ownership.SaveResource(dir, ownership.ResourceRecord{
		Kind: string(ownership.KindStorageCluster), Name: "ceph-a", Cluster: "ceph-a", Context: "ctx", Owner: "foreign",
	}); err != nil {
		t.Fatal(err)
	}
	result := storageDestroyResult("ceph-a", []string{"a1"}, nil).Clusters[0]
	err := ReconcileStorageDestroyOwnership(dir, "ctx", map[string]StorageDestroyClusterResult{"ceph-a": result}, map[string]string{"ceph-a": "storage__ceph-a__a1"})
	if err == nil || !strings.Contains(err.Error(), "contradicts") {
		t.Fatalf("error = %v", err)
	}
	if records, loadErr := ownership.LoadContext(dir, "ctx"); loadErr != nil || len(records) != 1 {
		t.Fatalf("contradictory owner must be retained, records=%v err=%v", records, loadErr)
	}
}

func TestReconcileStorageDestroyOwnershipRequiresTheSelectedSeed(t *testing.T) {
	for _, test := range []struct {
		name     string
		host     string
		seedHost string
		expected string
	}{
		{name: "missing host", seedHost: "storage__ceph-a__a1", expected: "storage__ceph-a__a1"},
		{name: "wrong host", host: "storage__ceph-a__other", seedHost: "storage__ceph-a__a1", expected: "storage__ceph-a__a1"},
		{name: "missing seed attribute", host: "storage__ceph-a__a1", expected: "storage__ceph-a__a1"},
		{name: "wrong seed attribute", host: "storage__ceph-a__a1", seedHost: "storage__ceph-a__other", expected: "storage__ceph-a__a1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "ownership")
			if err := ownership.SaveResource(dir, ownership.ResourceRecord{
				Kind: string(ownership.KindStorageCluster), Name: "ceph-a", Cluster: "ceph-a", Context: "ctx", Host: test.host,
				Attributes: map[string]string{"seedHost": test.seedHost},
			}); err != nil {
				t.Fatal(err)
			}
			result := storageDestroyResult("ceph-a", []string{"a1"}, nil).Clusters[0]
			err := ReconcileStorageDestroyOwnership(dir, "ctx", map[string]StorageDestroyClusterResult{"ceph-a": result}, map[string]string{"ceph-a": test.expected})
			if err == nil || !strings.Contains(err.Error(), "contradicts") {
				t.Fatalf("error = %v", err)
			}
			if records, loadErr := ownership.LoadContext(dir, "ctx"); loadErr != nil || len(records) != 1 {
				t.Fatalf("contradictory seed owner must be retained, records=%v err=%v", records, loadErr)
			}
		})
	}
}
