package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/ownership"
)

const storageDestroyTestFSIDA = "11111111-1111-1111-1111-111111111111"

const storageDestroyTestFSIDB = "22222222-2222-2222-2222-222222222222"

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
			Name: name,
			FSID: map[string]string{
				"ceph-a": storageDestroyTestFSIDA,
				"ceph-b": storageDestroyTestFSIDB,
			}[name],
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
	allSkipped := storageDestroyResult("ceph-a", nil, []string{"a1", "a2"})
	allSkipped.Clusters[0].FSID = ""
	if _, err := ValidateStorageDestroyResults([]StorageDestroyResult{allSkipped}, expected, true); err != nil {
		t.Fatalf("an authorized all-unreachable result does not need a destructive fsid: %v", err)
	}
	completedWithoutFSID := storageDestroyResult("ceph-a", []string{"a1", "a2"}, nil)
	completedWithoutFSID.Clusters[0].FSID = ""
	if _, err := ValidateStorageDestroyResults([]StorageDestroyResult{completedWithoutFSID}, expected, false); err != nil {
		t.Fatalf("a complete clean-node proof may represent a never-provisioned no-op: %v", err)
	}
	invalidFSID := storageDestroyResult("ceph-a", []string{"a1", "a2"}, nil)
	invalidFSID.Clusters[0].FSID = "not-a-uuid"
	if _, err := ValidateStorageDestroyResults([]StorageDestroyResult{invalidFSID}, expected, false); err == nil || !strings.Contains(err.Error(), "invalid fsid") {
		t.Fatalf("invalid fsid error = %v", err)
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
		fsid := map[string]string{"ceph-a": storageDestroyTestFSIDA, "ceph-b": storageDestroyTestFSIDB}[name]
		if err := ownership.SaveResource(dir, ownership.ResourceRecord{
			Kind: string(ownership.KindStorageCluster), Name: name, Cluster: name, Host: seed,
			Attributes: map[string]string{"seedHost": seed, "fsid": fsid},
		}); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	results := map[string]StorageDestroyClusterResult{
		"ceph-a": storageDestroyResult("ceph-a", nil, []string{"a1"}).Clusters[0],
		"ceph-b": storageDestroyResult("ceph-b", []string{"b1"}, nil).Clusters[0],
	}
	seeds := map[string]string{
		"ceph-a": "storage__ceph-a__a1",
		"ceph-b": "storage__ceph-b__b1",
	}
	manifest, err := PrepareStorageDestroyOwnershipRelease(dir, "", results, seeds)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if len(manifest.Clusters) != 1 || manifest.Clusters["ceph-b"].FSID != storageDestroyTestFSIDB {
		t.Fatalf("release manifest = %+v, want only ceph-b", manifest)
	}
	if err := MarkStorageDestroyOwnershipReleased(dir, "", results, seeds, manifest); err != nil {
		t.Fatalf("mark released: %v", err)
	}
	if err := ReconcileStorageDestroyOwnership(dir, "", results, seeds); err != nil {
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

func TestStorageDestroyReleaseStateReplaysAfterInterruption(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ownership")
	seed := "storage__ceph-a__a1"
	if err := ownership.SaveResource(dir, ownership.ResourceRecord{
		Kind: string(ownership.KindStorageCluster), Name: "ceph-a", Cluster: "ceph-a", Host: seed,
		Attributes: map[string]string{"seedHost": seed, "fsid": storageDestroyTestFSIDA},
	}); err != nil {
		t.Fatal(err)
	}
	result := storageDestroyResult("ceph-a", []string{"a1"}, nil).Clusters[0]
	results := map[string]StorageDestroyClusterResult{"ceph-a": result}
	expected := map[string][]string{"ceph-a": {"a1"}}
	seeds := map[string]string{"ceph-a": seed}
	manifest, err := PrepareStorageDestroyOwnershipRelease(dir, "", results, seeds)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := RecoverStorageDestroyResults(dir, "", expected, seeds)
	if err != nil || len(recovered) != 1 {
		t.Fatalf("recover release-pending proof = %v, err = %v", recovered, err)
	}
	if err := ReconcileStorageDestroyOwnership(dir, "", recovered, seeds); err == nil || !strings.Contains(err.Error(), "not fully released") {
		t.Fatalf("pre-release reconciliation error = %v", err)
	}
	if err := MarkStorageDestroyOwnershipReleased(dir, "", recovered, seeds, manifest); err != nil {
		t.Fatal(err)
	}
	pendingReconcile, err := PrepareStorageDestroyOwnershipRelease(dir, "", recovered, seeds)
	if err != nil || len(pendingReconcile.Clusters) != 0 {
		t.Fatalf("evidence-released owner replay manifest=%+v err=%v, want controller-only reconciliation", pendingReconcile, err)
	}
	if err := ReconcileStorageDestroyOwnership(dir, "", recovered, seeds); err != nil {
		t.Fatal(err)
	}
	if records, err := ownership.LoadContext(dir, ""); err != nil || len(records) != 0 {
		t.Fatalf("completed owner cleanup records=%v err=%v", records, err)
	}
	recovered, err = RecoverStorageDestroyResults(dir, "", expected, seeds)
	if err != nil || len(recovered) != 1 {
		t.Fatalf("recover receipt after owner cleanup = %v, err = %v", recovered, err)
	}
	replayed, err := PrepareStorageDestroyOwnershipRelease(dir, "", recovered, seeds)
	if err != nil {
		t.Fatal(err)
	}
	if len(replayed.Clusters) != 0 {
		t.Fatalf("receipt retry manifest = %+v, want controller-only completion replay", replayed)
	}
	if err := ReconcileStorageDestroyOwnership(dir, "", recovered, seeds); err != nil {
		t.Fatalf("reconcile ownerless receipt replay: %v", err)
	}
}

func TestStorageDestroyReceiptPromotesProofValidatedCrashState(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ownership")
	seed := "storage__ceph-a__a1"
	seeds := map[string]string{"ceph-a": seed}
	expected := map[string][]string{"ceph-a": {"a1"}}
	result := storageDestroyResult("ceph-a", []string{"a1"}, nil).Clusters[0]
	results := map[string]StorageDestroyClusterResult{"ceph-a": result}
	if err := ownership.SaveResource(dir, ownership.ResourceRecord{
		Kind: string(ownership.KindStorageCluster), Name: "ceph-a", Cluster: "ceph-a", Host: seed,
		Attributes: map[string]string{"seedHost": seed, "fsid": result.FSID},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareStorageDestroyOwnershipRelease(dir, "", results, seeds); err != nil {
		t.Fatal(err)
	}
	if err := writeStorageDestroyCompletionReceipt(dir, StorageDestroyCompletionReceipt{
		APIVersion: storageDestroyCompletionAPIVersion,
		State:      storageDestroyCompletionStateResetPending,
		Cluster:    "ceph-a",
		SeedHost:   seed,
		Result:     result,
	}); err != nil {
		t.Fatal(err)
	}
	recovered, err := RecoverStorageDestroyResults(dir, "", expected, seeds)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := PrepareStorageDestroyOwnershipRelease(dir, "", recovered, seeds)
	if err != nil || len(manifest.Clusters) != 0 {
		t.Fatalf("receipt-backed crash replay manifest=%+v err=%v", manifest, err)
	}
	records, err := ownership.LoadContext(dir, "")
	if err != nil || len(records) != 1 || records[0].Attributes[storageDestroyStatusAttr] != storageDestroyStatusEvidenceReleased {
		t.Fatalf("recovered owner records=%v err=%v", records, err)
	}
	if err := ReconcileStorageDestroyOwnership(dir, "", recovered, seeds); err != nil {
		t.Fatal(err)
	}
	if records, err := ownership.LoadContext(dir, ""); err != nil || len(records) != 0 {
		t.Fatalf("final owner records=%v err=%v", records, err)
	}
	receipt, found, err := readStorageDestroyCompletionReceipt(dir, "ceph-a")
	if err != nil || !found || receipt.State != storageDestroyCompletionStateCompleted {
		t.Fatalf("final crash receipt=%+v found=%t err=%v", receipt, found, err)
	}
}

func TestStorageApplyLifecycleRefusesPendingCompletedOwner(t *testing.T) {
	for _, status := range []string{storageDestroyStatusProofValidated, storageDestroyStatusEvidenceReleased} {
		t.Run(status, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "ownership")
			seed := "storage__ceph-a__a1"
			seeds := map[string]string{"ceph-a": seed}
			result := storageDestroyResult("ceph-a", []string{"a1"}, nil).Clusters[0]
			results := map[string]StorageDestroyClusterResult{"ceph-a": result}
			if err := ownership.SaveResource(dir, ownership.ResourceRecord{
				Kind: string(ownership.KindStorageCluster), Name: "ceph-a", Cluster: "ceph-a", Host: seed,
				Attributes: map[string]string{"seedHost": seed, "fsid": result.FSID},
			}); err != nil {
				t.Fatal(err)
			}
			manifest, err := PrepareStorageDestroyOwnershipRelease(dir, "", results, seeds)
			if err != nil {
				t.Fatal(err)
			}
			expectedReceiptState := storageDestroyCompletionStateCompleted
			if status == storageDestroyStatusEvidenceReleased {
				if err := MarkStorageDestroyOwnershipReleased(dir, "", results, seeds, manifest); err != nil {
					t.Fatal(err)
				}
				expectedReceiptState = storageDestroyCompletionStateResetPending
			} else if err := writeStorageDestroyCompletionReceipt(dir, StorageDestroyCompletionReceipt{
				APIVersion: storageDestroyCompletionAPIVersion,
				State:      storageDestroyCompletionStateCompleted,
				Cluster:    "ceph-a",
				SeedHost:   seed,
				Result:     result,
			}); err != nil {
				t.Fatal(err)
			}
			if err := BeginStorageApplyLifecycle(dir, "", "ceph-a"); err == nil || !strings.Contains(err.Error(), "storage topology before apply") {
				t.Fatalf("pending completion apply error = %v", err)
			}
			receipt, found, err := readStorageDestroyCompletionReceipt(dir, "ceph-a")
			if err != nil || !found || receipt.State != expectedReceiptState {
				t.Fatalf("apply lifecycle receipt=%+v found=%t err=%v", receipt, found, err)
			}
			if records, err := ownership.LoadContext(dir, ""); err != nil || len(records) != 1 || records[0].Attributes[storageDestroyStatusAttr] != status {
				t.Fatalf("pending owner must be retained before reset, records=%v err=%v", records, err)
			}
		})
	}
}

func TestStorageApplyLifecycleRefusesResetPendingBeforeOwnerMutation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ownership")
	seed := "storage__ceph-a__a1"
	receiptResult := storageDestroyResult("ceph-a", []string{"a1"}, nil).Clusters[0]
	if err := writeStorageDestroyCompletionReceipt(dir, StorageDestroyCompletionReceipt{
		APIVersion: storageDestroyCompletionAPIVersion,
		State:      storageDestroyCompletionStateResetPending,
		Cluster:    "ceph-a",
		SeedHost:   seed,
		Result:     receiptResult,
	}); err != nil {
		t.Fatal(err)
	}
	ownerResult := receiptResult
	ownerResult.FSID = storageDestroyTestFSIDB
	proof, err := encodeStorageDestroyClusterProof(ownerResult)
	if err != nil {
		t.Fatal(err)
	}
	if err := ownership.SaveResource(dir, ownership.ResourceRecord{
		Kind: string(ownership.KindStorageCluster), Name: "ceph-a", Cluster: "ceph-a", Host: seed,
		Attributes: map[string]string{
			"seedHost": seed, "fsid": ownerResult.FSID,
			storageDestroyStatusAttr: storageDestroyStatusProofValidated,
			storageDestroyProofAttr:  proof,
		},
	}); err != nil {
		t.Fatal(err)
	}
	records, err := ownership.LoadContext(dir, "")
	if err != nil || len(records) != 1 {
		t.Fatalf("load owner before apply: records=%v err=%v", records, err)
	}
	path, err := ownership.ResourcePath(dir, records[0])
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := BeginStorageApplyLifecycle(dir, "", "ceph-a"); err == nil || !strings.Contains(err.Error(), "pending release or controller reset") {
		t.Fatalf("reset-pending apply error = %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("reset-pending refusal mutated the contradictory owner\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestStorageApplyLifecycleTransitionsOwnerlessCompletedReceipt(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ownership")
	seed := "storage__ceph-a__a1"
	seeds := map[string]string{"ceph-a": seed}
	result := storageDestroyResult("ceph-a", []string{"a1"}, nil).Clusters[0]
	results := map[string]StorageDestroyClusterResult{"ceph-a": result}
	manifest := StorageDestroyReleaseManifest{SchemaVersion: 1, Clusters: map[string]StorageDestroyReleaseCluster{
		"ceph-a": {FSID: result.FSID, Nodes: map[string]string{"a1": seed}},
	}}
	if err := writeStorageDestroyCompletionReceipt(dir, StorageDestroyCompletionReceipt{
		APIVersion: storageDestroyCompletionAPIVersion,
		State:      storageDestroyCompletionStateCompleted,
		Cluster:    "ceph-a",
		SeedHost:   seed,
		Result:     result,
	}); err != nil {
		t.Fatal(err)
	}
	if err := BeginStorageApplyLifecycle(dir, "", "ceph-a"); err != nil {
		t.Fatal(err)
	}
	receipt, found, err := readStorageDestroyCompletionReceipt(dir, "ceph-a")
	if err != nil || !found || receipt.State != storageDestroyCompletionStateApplyStarted {
		t.Fatalf("apply lifecycle receipt=%+v found=%t err=%v", receipt, found, err)
	}
	if err := BeginStorageApplyLifecycle(dir, "", "ceph-a"); err != nil {
		t.Fatalf("apply lifecycle retry: %v", err)
	}
	recovered, err := RecoverStorageDestroyResults(
		dir,
		"",
		map[string][]string{"ceph-a": {"new-node"}},
		map[string]string{"ceph-a": "storage__ceph-a__new-node"},
	)
	if err != nil || len(recovered) != 0 {
		t.Fatalf("apply-started receipt must force fresh destroy proof after a topology change: recovered=%v err=%v", recovered, err)
	}
	if _, err := PrepareStorageDestroyOwnershipRelease(dir, "", results, seeds); err == nil || !strings.Contains(err.Error(), "cannot authorize destroy completion") {
		t.Fatalf("prepare accepted apply-started receipt: %v", err)
	}
	if err := MarkStorageDestroyOwnershipReleased(dir, "", results, seeds, manifest); err == nil || !strings.Contains(err.Error(), "cannot authorize destroy completion") {
		t.Fatalf("mark accepted apply-started receipt: %v", err)
	}
}

func TestStorageApplyLifecycleRefusesContradictoryReceipt(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ownership")
	result := storageDestroyResult("ceph-a", []string{"a1"}, nil).Clusters[0]
	receipt := StorageDestroyCompletionReceipt{
		APIVersion: storageDestroyCompletionAPIVersion,
		State:      storageDestroyCompletionStateCompleted,
		Context:    "other",
		Cluster:    "ceph-a",
		SeedHost:   "storage__ceph-a__a1",
		Result:     result,
	}
	if err := writeStorageDestroyCompletionReceipt(dir, receipt); err != nil {
		t.Fatal(err)
	}
	if err := BeginStorageApplyLifecycle(dir, "ctx", "ceph-a"); err == nil || !strings.Contains(err.Error(), "contradicts") {
		t.Fatalf("context contradiction error = %v", err)
	}
	receipt.Context = "ctx"
	receipt.State = "unknown"
	if err := writeStorageDestroyCompletionReceipt(dir, receipt); err != nil {
		t.Fatal(err)
	}
	if err := BeginStorageApplyLifecycle(dir, "ctx", "ceph-a"); err == nil || !strings.Contains(err.Error(), "invalid state") {
		t.Fatalf("unknown state error = %v", err)
	}
}

func TestStorageApplyLifecycleInvalidatesStaleProofBeforeMutation(t *testing.T) {
	for _, receiptState := range []string{"", storageDestroyCompletionStateApplyStarted} {
		t.Run(receiptState, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "ownership")
			seed := "storage__ceph-a__a1"
			result := storageDestroyResult("ceph-a", []string{"a1"}, nil).Clusters[0]
			if receiptState != "" {
				if err := writeStorageDestroyCompletionReceipt(dir, StorageDestroyCompletionReceipt{
					APIVersion: storageDestroyCompletionAPIVersion,
					State:      receiptState,
					Cluster:    "ceph-a",
					SeedHost:   seed,
					Result:     result,
				}); err != nil {
					t.Fatal(err)
				}
			}
			proof, err := encodeStorageDestroyClusterProof(result)
			if err != nil {
				t.Fatal(err)
			}
			if err := ownership.SaveResource(dir, ownership.ResourceRecord{
				Kind: string(ownership.KindStorageCluster), Name: "ceph-a", Cluster: "ceph-a", Host: seed,
				Attributes: map[string]string{
					"seedHost": seed, "fsid": result.FSID,
					storageDestroyStatusAttr: storageDestroyStatusProofValidated,
					storageDestroyProofAttr:  proof,
				},
			}); err != nil {
				t.Fatal(err)
			}
			if err := BeginStorageApplyLifecycle(dir, "", "ceph-a"); err != nil {
				t.Fatal(err)
			}
			records, err := ownership.LoadContext(dir, "")
			if err != nil || len(records) != 1 {
				t.Fatalf("stale proof owner records=%v err=%v", records, err)
			}
			if records[0].Attributes[storageDestroyStatusAttr] != "" || records[0].Attributes[storageDestroyProofAttr] != "" {
				t.Fatalf("stale proof survived apply boundary: %+v", records[0].Attributes)
			}
		})
	}
}

func TestStorageDestroyCompletionReceiptIsSupersededByANewOwnerLifecycle(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ownership")
	seed := "storage__ceph-a__a1"
	seeds := map[string]string{"ceph-a": seed}
	expected := map[string][]string{"ceph-a": {"a1"}}
	oldResult := storageDestroyResult("ceph-a", []string{"a1"}, nil).Clusters[0]
	if err := ownership.SaveResource(dir, ownership.ResourceRecord{
		Kind: string(ownership.KindStorageCluster), Name: "ceph-a", Cluster: "ceph-a", Host: seed,
		Attributes: map[string]string{"seedHost": seed, "fsid": oldResult.FSID},
	}); err != nil {
		t.Fatal(err)
	}
	oldResults := map[string]StorageDestroyClusterResult{"ceph-a": oldResult}
	oldManifest, err := PrepareStorageDestroyOwnershipRelease(dir, "", oldResults, seeds)
	if err != nil {
		t.Fatal(err)
	}
	if err := MarkStorageDestroyOwnershipReleased(dir, "", oldResults, seeds, oldManifest); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileStorageDestroyOwnership(dir, "", oldResults, seeds); err != nil {
		t.Fatal(err)
	}
	if err := BeginStorageApplyLifecycle(dir, "", "ceph-a"); err != nil {
		t.Fatal(err)
	}
	newResult := oldResult
	newResult.FSID = storageDestroyTestFSIDB
	if err := ownership.SaveResource(dir, ownership.ResourceRecord{
		Kind: string(ownership.KindStorageCluster), Name: "ceph-a", Cluster: "ceph-a", Host: seed,
		Attributes: map[string]string{"seedHost": seed, "fsid": newResult.FSID},
	}); err != nil {
		t.Fatal(err)
	}
	if recovered, err := RecoverStorageDestroyResults(dir, "", expected, seeds); err != nil || len(recovered) != 0 {
		t.Fatalf("a normal new owner must supersede the old receipt before destructive proof: recovered=%v err=%v", recovered, err)
	}
	newResults := map[string]StorageDestroyClusterResult{"ceph-a": newResult}
	newManifest, err := PrepareStorageDestroyOwnershipRelease(dir, "", newResults, seeds)
	if err != nil {
		t.Fatal(err)
	}
	if newManifest.Clusters["ceph-a"].FSID != storageDestroyTestFSIDB {
		t.Fatalf("new lifecycle manifest = %+v", newManifest)
	}
	if err := MarkStorageDestroyOwnershipReleased(dir, "", newResults, seeds, newManifest); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileStorageDestroyOwnership(dir, "", newResults, seeds); err != nil {
		t.Fatal(err)
	}
	recovered, err := RecoverStorageDestroyResults(dir, "", expected, seeds)
	if err != nil || recovered["ceph-a"].FSID != storageDestroyTestFSIDB {
		t.Fatalf("new lifecycle receipt = %+v, err=%v", recovered, err)
	}
}

func TestPrepareStorageDestroyReleaseBindsCleanRetryToExactOwner(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ownership")
	seed := "storage__ceph-a__a1"
	seeds := map[string]string{"ceph-a": seed}
	result := storageDestroyResult("ceph-a", []string{"a1"}, nil).Clusters[0]
	result.FSID = ""
	results := map[string]StorageDestroyClusterResult{"ceph-a": result}
	if err := ownership.SaveResource(dir, ownership.ResourceRecord{
		Kind: string(ownership.KindStorageCluster), Name: "ceph-a", Cluster: "ceph-a", Host: seed,
		Attributes: map[string]string{"seedHost": seed, "fsid": storageDestroyTestFSIDA},
	}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), StorageDestroyResultFileName)
	if err := writeStorageDestroyResult(path, results); err != nil {
		t.Fatal(err)
	}
	manifest, err := PrepareStorageDestroyOwnershipRelease(dir, "", results, seeds)
	if err != nil {
		t.Fatalf("prepare clean retry: %v", err)
	}
	if err := writeStorageDestroyResult(path, results); err != nil {
		t.Fatalf("rewrite clean retry result: %v", err)
	}
	if results["ceph-a"].FSID != storageDestroyTestFSIDA || manifest.Clusters["ceph-a"].FSID != storageDestroyTestFSIDA {
		t.Fatalf("bound results=%+v manifest=%+v", results, manifest)
	}
	records, err := ownership.LoadContext(dir, "")
	if err != nil || len(records) != 1 || records[0].Attributes[storageDestroyStatusAttr] != storageDestroyStatusProofValidated {
		t.Fatalf("bound proof owner records=%v err=%v", records, err)
	}
	if _, found, err := readStorageDestroyCompletionReceipt(dir, "ceph-a"); err != nil || found {
		t.Fatalf("owner-bound proof wrote ownerless receipt found=%t err=%v", found, err)
	}
	report, found, err := ReadStorageDestroyResult(path)
	if err != nil || !found || report.Clusters[0].FSID != storageDestroyTestFSIDA {
		t.Fatalf("bound final artifact=%+v found=%t err=%v", report, found, err)
	}
	if err := MarkStorageDestroyOwnershipReleased(dir, "", results, seeds, manifest); err != nil {
		t.Fatalf("mark bound evidence released: %v", err)
	}
	if err := ReconcileStorageDestroyOwnership(dir, "", results, seeds); err != nil {
		t.Fatalf("reconcile bound clean retry: %v", err)
	}
	if records, err := ownership.LoadContext(dir, ""); err != nil || len(records) != 0 {
		t.Fatalf("bound clean retry owner records=%v err=%v", records, err)
	}
	receipt, found, err := readStorageDestroyCompletionReceipt(dir, "ceph-a")
	if err != nil || !found || receipt.State != storageDestroyCompletionStateCompleted || receipt.Result.FSID != storageDestroyTestFSIDA {
		t.Fatalf("bound completion receipt=%+v found=%t err=%v", receipt, found, err)
	}
}

func TestPrepareStorageDestroyReleaseDoesNotBindPartialProof(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ownership")
	seed := "storage__ceph-a__a1"
	if err := ownership.SaveResource(dir, ownership.ResourceRecord{
		Kind: string(ownership.KindStorageCluster), Name: "ceph-a", Cluster: "ceph-a", Host: seed,
		Attributes: map[string]string{"seedHost": seed, "fsid": storageDestroyTestFSIDA},
	}); err != nil {
		t.Fatal(err)
	}
	result := storageDestroyResult("ceph-a", []string{"a1"}, []string{"a2"}).Clusters[0]
	result.FSID = ""
	results := map[string]StorageDestroyClusterResult{"ceph-a": result}
	manifest, err := PrepareStorageDestroyOwnershipRelease(dir, "", results, map[string]string{"ceph-a": seed})
	if err != nil {
		t.Fatal(err)
	}
	if results["ceph-a"].FSID != "" || len(manifest.Clusters) != 0 {
		t.Fatalf("partial proof was bound or released results=%+v manifest=%+v", results, manifest)
	}
	records, err := ownership.LoadContext(dir, "")
	if err != nil || len(records) != 1 || records[0].Attributes[storageDestroyStatusAttr] != storageDestroyStatusPartial {
		t.Fatalf("partial owner records=%v err=%v", records, err)
	}
}

func TestPrepareStorageDestroyReleaseRefusesCleanBindingOverActiveReceipt(t *testing.T) {
	for _, state := range []string{
		storageDestroyCompletionStateReleasePending,
		storageDestroyCompletionStateResetPending,
		storageDestroyCompletionStateCompleted,
	} {
		t.Run(state, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "ownership")
			seed := "storage__ceph-a__a1"
			oldResult := storageDestroyResult("ceph-a", []string{"a1"}, nil).Clusters[0]
			oldResult.FSID = storageDestroyTestFSIDB
			if err := writeStorageDestroyCompletionReceipt(dir, StorageDestroyCompletionReceipt{
				APIVersion: storageDestroyCompletionAPIVersion,
				State:      state,
				Cluster:    "ceph-a",
				SeedHost:   seed,
				Result:     oldResult,
			}); err != nil {
				t.Fatal(err)
			}
			if err := ownership.SaveResource(dir, ownership.ResourceRecord{
				Kind: string(ownership.KindStorageCluster), Name: "ceph-a", Cluster: "ceph-a", Host: seed,
				Attributes: map[string]string{"seedHost": seed, "fsid": storageDestroyTestFSIDA},
			}); err != nil {
				t.Fatal(err)
			}
			result := storageDestroyResult("ceph-a", []string{"a1"}, nil).Clusters[0]
			result.FSID = ""
			results := map[string]StorageDestroyClusterResult{"ceph-a": result}
			_, err := PrepareStorageDestroyOwnershipRelease(dir, "", results, map[string]string{"ceph-a": seed})
			if err == nil || !strings.Contains(err.Error(), "cannot be superseded") {
				t.Fatalf("active receipt binding error=%v", err)
			}
			if results["ceph-a"].FSID != "" {
				t.Fatalf("active receipt bound proof=%+v", results["ceph-a"])
			}
			records, loadErr := ownership.LoadContext(dir, "")
			if loadErr != nil || len(records) != 1 || records[0].Attributes[storageDestroyStatusAttr] != "" {
				t.Fatalf("active receipt mutated owner records=%v err=%v", records, loadErr)
			}
		})
	}
}

func TestPrepareStorageDestroyReleaseDistinguishesNoOpOwnerAndReceipt(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ownership")
	seed := "storage__ceph-a__a1"
	seeds := map[string]string{"ceph-a": seed}
	result := storageDestroyResult("ceph-a", []string{"a1"}, nil).Clusters[0]
	priorResult := result
	if err := writeStorageDestroyCompletionReceipt(dir, StorageDestroyCompletionReceipt{
		APIVersion: storageDestroyCompletionAPIVersion,
		State:      storageDestroyCompletionStateApplyStarted,
		Cluster:    "ceph-a",
		SeedHost:   seed,
		Result:     priorResult,
	}); err != nil {
		t.Fatal(err)
	}
	result.FSID = ""
	results := map[string]StorageDestroyClusterResult{"ceph-a": result}
	manifest, err := PrepareStorageDestroyOwnershipRelease(dir, "", results, seeds)
	if err != nil || len(manifest.Clusters) != 1 || manifest.Clusters["ceph-a"].FSID != "" {
		t.Fatalf("clean no-owner no-op release manifest=%+v err=%v", manifest, err)
	}
	receipt, found, err := readStorageDestroyCompletionReceipt(dir, "ceph-a")
	if err != nil || !found || receipt.State != storageDestroyCompletionStateReleasePending || receipt.Result.FSID != "" {
		t.Fatalf("clean no-op release-pending receipt=%+v found=%t err=%v", receipt, found, err)
	}
	if err := BeginStorageApplyLifecycle(dir, "", "ceph-a"); err == nil || !strings.Contains(err.Error(), "pending release or controller reset") {
		t.Fatalf("apply accepted clean no-op before evidence release: %v", err)
	}
	recovered, err := RecoverStorageDestroyResults(dir, "", map[string][]string{"ceph-a": {"a1"}}, seeds)
	if err != nil || recovered["ceph-a"].FSID != "" {
		t.Fatalf("recover clean no-op before evidence release=%+v err=%v", recovered, err)
	}
	manifest, err = PrepareStorageDestroyOwnershipRelease(dir, "", recovered, seeds)
	if err != nil || len(manifest.Clusters) != 1 || manifest.Clusters["ceph-a"].FSID != "" {
		t.Fatalf("replay clean no-op release manifest=%+v err=%v", manifest, err)
	}
	if err := MarkStorageDestroyOwnershipReleased(dir, "", results, seeds, manifest); err != nil {
		t.Fatalf("mark clean no-op evidence released: %v", err)
	}
	receipt, found, err = readStorageDestroyCompletionReceipt(dir, "ceph-a")
	if err != nil || !found || receipt.State != storageDestroyCompletionStateResetPending || receipt.Result.FSID != "" {
		t.Fatalf("clean no-op reset-pending receipt=%+v found=%t err=%v", receipt, found, err)
	}
	if err := BeginStorageApplyLifecycle(dir, "", "ceph-a"); err == nil || !strings.Contains(err.Error(), "pending release or controller reset") {
		t.Fatalf("apply accepted clean no-op before controller reset: %v", err)
	}
	if err := ReconcileStorageDestroyOwnership(dir, "", results, seeds); err != nil {
		t.Fatalf("reconcile clean no-op: %v", err)
	}
	receipt, found, err = readStorageDestroyCompletionReceipt(dir, "ceph-a")
	if err != nil || !found || receipt.State != storageDestroyCompletionStateCompleted {
		t.Fatalf("clean no-op finalized receipt=%+v found=%t err=%v", receipt, found, err)
	}
	recovered, err = RecoverStorageDestroyResults(dir, "", map[string][]string{"ceph-a": {"a1"}}, seeds)
	if err != nil || recovered["ceph-a"].FSID != "" {
		t.Fatalf("recover clean no-op receipt=%+v err=%v", recovered, err)
	}
	if err := ownership.SaveResource(dir, ownership.ResourceRecord{
		Kind: string(ownership.KindStorageCluster), Name: "ceph-a", Cluster: "ceph-a", Host: seed,
		Attributes: map[string]string{"seedHost": seed, "fsid": storageDestroyTestFSIDA},
	}); err != nil {
		t.Fatal(err)
	}
	_, err = PrepareStorageDestroyOwnershipRelease(dir, "", results, seeds)
	if err == nil || !strings.Contains(err.Error(), "cannot be superseded") {
		t.Fatalf("completed no-op receipt with new owner error=%v", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	result.FSID = storageDestroyTestFSIDA
	results["ceph-a"] = result
	if _, err := PrepareStorageDestroyOwnershipRelease(dir, "", results, seeds); err == nil || !strings.Contains(err.Error(), "no exact controller owner or completion receipt") {
		t.Fatalf("fsid-bound proof without owner or receipt error = %v", err)
	}
}

func TestReconcileStorageDestroyNoOpRequiresCompletedReceipt(t *testing.T) {
	seed := "storage__ceph-a__a1"
	seeds := map[string]string{"ceph-a": seed}
	result := storageDestroyResult("ceph-a", []string{"a1"}, nil).Clusters[0]
	result.FSID = ""
	results := map[string]StorageDestroyClusterResult{"ceph-a": result}
	for _, state := range []string{"", storageDestroyCompletionStateApplyStarted, storageDestroyCompletionStateReleasePending} {
		t.Run(state, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "ownership")
			if state != "" {
				if err := writeStorageDestroyCompletionReceipt(dir, StorageDestroyCompletionReceipt{
					APIVersion: storageDestroyCompletionAPIVersion,
					State:      state,
					Cluster:    "ceph-a",
					SeedHost:   seed,
					Result:     result,
				}); err != nil {
					t.Fatal(err)
				}
			}
			if err := ReconcileStorageDestroyOwnership(dir, "", results, seeds); err == nil {
				t.Fatal("clean no-op reconciled without a completed receipt")
			}
		})
	}
	dir := filepath.Join(t.TempDir(), "ownership")
	if err := writeStorageDestroyCompletionReceipt(dir, StorageDestroyCompletionReceipt{
		APIVersion: storageDestroyCompletionAPIVersion,
		State:      storageDestroyCompletionStateResetPending,
		Cluster:    "ceph-a",
		SeedHost:   seed,
		Result:     result,
	}); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileStorageDestroyOwnership(dir, "", results, seeds); err != nil {
		t.Fatalf("reset-pending clean no-op reconciliation: %v", err)
	}
	receipt, found, err := readStorageDestroyCompletionReceipt(dir, "ceph-a")
	if err != nil || !found || receipt.State != storageDestroyCompletionStateCompleted {
		t.Fatalf("final clean no-op receipt=%+v found=%t err=%v", receipt, found, err)
	}
}

func TestRecoverStorageDestroyRefusesUnknownOwnerStatus(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ownership")
	seed := "storage__ceph-a__a1"
	if err := ownership.SaveResource(dir, ownership.ResourceRecord{
		Kind: string(ownership.KindStorageCluster), Name: "ceph-a", Cluster: "ceph-a", Host: seed,
		Attributes: map[string]string{
			"seedHost":               seed,
			"fsid":                   storageDestroyTestFSIDA,
			storageDestroyStatusAttr: "unknown",
		},
	}); err != nil {
		t.Fatal(err)
	}
	_, err := RecoverStorageDestroyResults(
		dir,
		"",
		map[string][]string{"ceph-a": {"a1"}},
		map[string]string{"ceph-a": seed},
	)
	if err == nil || !strings.Contains(err.Error(), "unknown destroy status") {
		t.Fatalf("unknown owner status error = %v", err)
	}
}

func TestResetStorageDestroyProofMakesValidationFailureDestructiveOnRetry(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ownership")
	seed := "storage__ceph-a__a1"
	seeds := map[string]string{"ceph-a": seed}
	expected := map[string][]string{"ceph-a": {"a1"}}
	if err := ownership.SaveResource(dir, ownership.ResourceRecord{
		Kind: string(ownership.KindStorageCluster), Name: "ceph-a", Cluster: "ceph-a", Host: seed,
		Attributes: map[string]string{"seedHost": seed, "fsid": storageDestroyTestFSIDA},
	}); err != nil {
		t.Fatal(err)
	}
	result := storageDestroyResult("ceph-a", []string{"a1"}, nil).Clusters[0]
	results := map[string]StorageDestroyClusterResult{"ceph-a": result}
	manifest, err := PrepareStorageDestroyOwnershipRelease(dir, "", results, seeds)
	if err != nil {
		t.Fatal(err)
	}
	if err := ResetStorageDestroyOwnershipProof(dir, "", results, seeds, manifest); err != nil {
		t.Fatal(err)
	}
	if recovered, err := RecoverStorageDestroyResults(dir, "", expected, seeds); err != nil || len(recovered) != 0 {
		t.Fatalf("release validation failure must rerun destructive proof: recovered=%v err=%v", recovered, err)
	}
	records, err := ownership.LoadContext(dir, "")
	if err != nil || len(records) != 1 {
		t.Fatalf("owner records=%v err=%v", records, err)
	}
	if records[0].Attributes[storageDestroyStatusAttr] != "" || records[0].Attributes[storageDestroyProofAttr] != "" {
		t.Fatalf("reset owner attributes = %+v", records[0].Attributes)
	}
}

func TestReconcileStorageDestroyOwnershipPrevalidatesEveryClusterBeforeRemoval(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ownership")
	seeds := map[string]string{
		"ceph-a": "storage__ceph-a__a1",
		"ceph-b": "storage__ceph-b__b1",
	}
	results := map[string]StorageDestroyClusterResult{
		"ceph-a": storageDestroyResult("ceph-a", []string{"a1"}, nil).Clusters[0],
		"ceph-b": storageDestroyResult("ceph-b", []string{"b1"}, nil).Clusters[0],
	}
	for _, name := range []string{"ceph-a", "ceph-b"} {
		if err := ownership.SaveResource(dir, ownership.ResourceRecord{
			Kind: string(ownership.KindStorageCluster), Name: name, Cluster: name, Host: seeds[name],
			Attributes: map[string]string{"seedHost": seeds[name], "fsid": results[name].FSID},
		}); err != nil {
			t.Fatal(err)
		}
	}
	manifest, err := PrepareStorageDestroyOwnershipRelease(dir, "", results, seeds)
	if err != nil {
		t.Fatal(err)
	}
	if err := MarkStorageDestroyOwnershipReleased(dir, "", results, seeds, StorageDestroyReleaseManifest{
		SchemaVersion: 1,
		Clusters:      map[string]StorageDestroyReleaseCluster{"ceph-a": manifest.Clusters["ceph-a"]},
	}); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileStorageDestroyOwnership(dir, "", results, seeds); err == nil || !strings.Contains(err.Error(), "ceph-b") {
		t.Fatalf("mixed release reconciliation error = %v", err)
	}
	if records, err := ownership.LoadContext(dir, ""); err != nil || len(records) != 2 {
		t.Fatalf("prevalidation must retain both owners, records=%v err=%v", records, err)
	}
}

func TestReconcileStorageDestroyOwnershipTargetsOnlyTheExactOwner(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ownership")
	owner := ownership.ResourceRecord{
		Kind: string(ownership.KindStorageCluster), Name: "ceph-a", Cluster: "ceph-a", Context: "ctx", Host: "storage__ceph-a__a1",
		Attributes: map[string]string{"seedHost": "storage__ceph-a__a1", "fsid": storageDestroyTestFSIDA},
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
	results := map[string]StorageDestroyClusterResult{"ceph-a": result}
	seeds := map[string]string{"ceph-a": "storage__ceph-a__a1"}
	manifest, err := PrepareStorageDestroyOwnershipRelease(dir, "ctx", results, seeds)
	if err != nil {
		t.Fatal(err)
	}
	if err := MarkStorageDestroyOwnershipReleased(dir, "ctx", results, seeds, manifest); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileStorageDestroyOwnership(dir, "ctx", results, seeds); err != nil {
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

func TestStorageDestroyOwnerContractProblemsNameEveryMismatch(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*ownership.ResourceRecord)
		want   string
	}{
		{name: "manager", mutate: func(record *ownership.ResourceRecord) { record.Owner = "foreign" }, want: `owner is "foreign", want "bootwright"`},
		{name: "api version", mutate: func(record *ownership.ResourceRecord) { record.APIVersion = "other/v1" }, want: `apiVersion is "other/v1"`},
		{name: "context", mutate: func(record *ownership.ResourceRecord) { record.Context = "other" }, want: `context is "other", want "ctx"`},
		{name: "cluster", mutate: func(record *ownership.ResourceRecord) { record.Cluster = "ceph-b" }, want: `cluster is "ceph-b", want record name "ceph-a"`},
		{name: "host", mutate: func(record *ownership.ResourceRecord) { record.Host = "storage__ceph-a__other" }, want: `host is "storage__ceph-a__other", want selected seed "storage__ceph-a__a1"`},
		{name: "seed host", mutate: func(record *ownership.ResourceRecord) { record.Attributes["seedHost"] = "storage__ceph-a__other" }, want: `attributes.seedHost is "storage__ceph-a__other", want selected seed "storage__ceph-a__a1"`},
		{name: "fsid", mutate: func(record *ownership.ResourceRecord) { record.Attributes["fsid"] = "not-a-uuid" }, want: `attributes.fsid "not-a-uuid" is not a canonical UUID`},
	} {
		t.Run(test.name, func(t *testing.T) {
			record := ownership.ResourceRecord{
				APIVersion: "bootwright.io/ownership/v1alpha1",
				Kind:       string(ownership.KindStorageCluster),
				Name:       "ceph-a",
				Owner:      ownership.Owner,
				Role:       ownership.RoleOwner,
				Context:    "ctx",
				Cluster:    "ceph-a",
				Host:       "storage__ceph-a__a1",
				Attributes: map[string]string{"seedHost": "storage__ceph-a__a1", "fsid": storageDestroyTestFSIDA},
			}
			test.mutate(&record)
			problems := storageDestroyOwnerContractProblems(record, "ctx", "storage__ceph-a__a1")
			if len(problems) != 1 || !strings.Contains(problems[0], test.want) {
				t.Fatalf("problems = %v, want only %q", problems, test.want)
			}
		})
	}
}

func TestReconcileStorageDestroyOwnershipRefusesAContradictoryOwner(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ownership")
	if err := ownership.SaveResource(dir, ownership.ResourceRecord{
		APIVersion: "other/v1",
		Kind:       string(ownership.KindStorageCluster),
		Name:       "ceph-a",
		Owner:      "foreign",
		Role:       ownership.RoleOwner,
		Context:    "other",
		Cluster:    "ceph-b",
		Host:       "storage__ceph-a__other",
		Attributes: map[string]string{"seedHost": "storage__ceph-a__other", "fsid": "not-a-uuid"},
	}); err != nil {
		t.Fatal(err)
	}
	result := storageDestroyResult("ceph-a", []string{"a1"}, nil).Clusters[0]
	err := ReconcileStorageDestroyOwnership(dir, "ctx", map[string]StorageDestroyClusterResult{"ceph-a": result}, map[string]string{"ceph-a": "storage__ceph-a__a1"})
	expectedPath := filepath.Join(dir, ownership.ResourceDirName, string(ownership.KindStorageCluster), "ceph-a.json")
	for _, want := range []string{
		"contradicts",
		expectedPath,
		`owner is "foreign", want "bootwright"`,
		`apiVersion is "other/v1"`,
		`context is "other", want "ctx"`,
		`cluster is "ceph-b", want record name "ceph-a"`,
		`host is "storage__ceph-a__other", want selected seed "storage__ceph-a__a1"`,
		`attributes.seedHost is "storage__ceph-a__other", want selected seed "storage__ceph-a__a1"`,
		`attributes.fsid "not-a-uuid" is not a canonical UUID`,
		"the record was retained",
	} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want %q", err, want)
		}
	}
	records, loadErr := ownership.LoadResources(dir)
	if loadErr != nil || len(records) != 1 || records[0].Context != "other" {
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
				Attributes: map[string]string{"seedHost": test.seedHost, "fsid": storageDestroyTestFSIDA},
			}); err != nil {
				t.Fatal(err)
			}
			result := storageDestroyResult("ceph-a", []string{"a1"}, nil).Clusters[0]
			err := ReconcileStorageDestroyOwnership(dir, "ctx", map[string]StorageDestroyClusterResult{"ceph-a": result}, map[string]string{"ceph-a": test.expected})
			if err == nil || !strings.Contains(err.Error(), "contradicts") || !strings.Contains(err.Error(), "selected seed") {
				t.Fatalf("error = %v", err)
			}
			if records, loadErr := ownership.LoadContext(dir, "ctx"); loadErr != nil || len(records) != 1 {
				t.Fatalf("contradictory seed owner must be retained, records=%v err=%v", records, loadErr)
			}
		})
	}
}
