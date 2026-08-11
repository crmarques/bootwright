package converge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/render"
)

const cephRecoveryTestFSID = "2088ddee-875b-11f1-9b98-303ea72d7724"

func TestParseDestroyCephOwnershipRecovery(t *testing.T) {
	confirmed, err := ParseDestroyCephOwnershipRecovery("ceph-b=2088DDEE-875B-11F1-9B98-303EA72D7724,ceph-a=1088ddee-875b-11f1-9b98-303ea72d7724")
	if err != nil {
		t.Fatalf("ParseDestroyCephOwnershipRecovery: %v", err)
	}
	if confirmed["ceph-b"] != cephRecoveryTestFSID || confirmed["ceph-a"] != "1088ddee-875b-11f1-9b98-303ea72d7724" {
		t.Fatalf("confirmed fsids = %v", confirmed)
	}

	for _, test := range []struct {
		name  string
		value string
		want  string
	}{
		{name: "missing separator", value: "ceph-a", want: "mapping"},
		{name: "invalid fsid", value: "ceph-a=not-a-uuid", want: "must be a UUID"},
		{name: "duplicate cluster", value: "ceph-a=" + cephRecoveryTestFSID + ",ceph-a=" + cephRecoveryTestFSID, want: "repeats"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseDestroyCephOwnershipRecovery(test.value)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestValidateDestroyCephOwnershipRecoveryAllowsMissingRecordAndRejectsConflict(t *testing.T) {
	cluster := cephRecoveryTestCluster()
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}}
	seedHost := render.StorageSeedHostName(cluster)
	valid := ownership.ResourceRecord{
		APIVersion: "bootwright.io/ownership/v1alpha1",
		Kind:       string(ownership.KindStorageCluster),
		Name:       cluster.Metadata.Name,
		Owner:      ownership.Owner,
		Context:    "lab",
		Host:       seedHost,
		Cluster:    cluster.Metadata.Name,
		Attributes: map[string]string{
			"seedHost": seedHost,
		},
	}
	confirmed := map[string]string{cluster.Metadata.Name: cephRecoveryTestFSID}
	root := t.TempDir()

	if err := ValidateDestroyCephOwnershipRecovery(state, nil, root, "lab", []ownership.ResourceRecord{valid}, confirmed); err != nil {
		t.Fatalf("valid recovery evidence refused: %v", err)
	}
	if err := ValidateDestroyCephOwnershipRecovery(state, []string{}, root, "lab", []ownership.ResourceRecord{valid}, confirmed); err == nil || !strings.Contains(err.Error(), "not a selected managed Ceph cluster") {
		t.Fatalf("out-of-scope recovery error = %v", err)
	}
	if err := ValidateDestroyCephOwnershipRecovery(state, nil, root, "lab", nil, confirmed); err != nil {
		t.Fatalf("missing-record recovery refused: %v", err)
	}

	mutations := []struct {
		name   string
		mutate func(*ownership.ResourceRecord)
		want   string
	}{
		{name: "api version", mutate: func(record *ownership.ResourceRecord) { record.APIVersion = "other/v1" }, want: "apiVersion"},
		{name: "owner", mutate: func(record *ownership.ResourceRecord) { record.Owner = "other" }, want: "owner"},
		{name: "reference role", mutate: func(record *ownership.ResourceRecord) { record.Role = ownership.RoleReference }, want: "role"},
		{name: "context", mutate: func(record *ownership.ResourceRecord) { record.Context = "other" }, want: "context"},
		{name: "cluster", mutate: func(record *ownership.ResourceRecord) { record.Cluster = "other" }, want: "cluster"},
		{name: "host", mutate: func(record *ownership.ResourceRecord) { record.Host = "other" }, want: "host"},
		{name: "seed host", mutate: func(record *ownership.ResourceRecord) { record.Attributes["seedHost"] = "other" }, want: "attributes.seedHost"},
		{name: "invalid fsid", mutate: func(record *ownership.ResourceRecord) { record.Attributes["fsid"] = "invalid" }, want: "not a UUID"},
		{name: "mismatched fsid", mutate: func(record *ownership.ResourceRecord) {
			record.Attributes["fsid"] = "1088ddee-875b-11f1-9b98-303ea72d7724"
		}, want: "confirmed fsid"},
	}
	for _, test := range mutations {
		t.Run(test.name, func(t *testing.T) {
			record := valid
			record.Attributes = map[string]string{"seedHost": seedHost}
			test.mutate(&record)
			err := ValidateDestroyCephOwnershipRecovery(state, nil, root, "lab", []ownership.ResourceRecord{record}, confirmed)
			if err == nil || !strings.Contains(err.Error(), "ownership evidence conflicts") || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("conflicting recovery error = %v, want ownership conflict naming %q", err, test.want)
			}
		})
	}

	for _, fsid := range []string{"", cephRecoveryTestFSID} {
		t.Run("canonical record "+fsid, func(t *testing.T) {
			caseRoot := t.TempDir()
			record := valid
			record.Attributes = map[string]string{"seedHost": seedHost, "fsid": fsid}
			if err := ownership.SaveResource(caseRoot, record); err != nil {
				t.Fatalf("SaveResource: %v", err)
			}
			if err := ValidateDestroyCephOwnershipRecovery(state, nil, caseRoot, "lab", []ownership.ResourceRecord{record}, confirmed); err != nil {
				t.Fatalf("canonical recovery evidence refused: %v", err)
			}
		})
	}

	t.Run("malformed canonical record", func(t *testing.T) {
		caseRoot := t.TempDir()
		path := filepath.Join(caseRoot, ownership.ResourceDirName, string(ownership.KindStorageCluster), cluster.Metadata.Name+".json")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("create record dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(`{broken`), 0o600); err != nil {
			t.Fatalf("write malformed record: %v", err)
		}
		err := ValidateDestroyCephOwnershipRecovery(state, nil, caseRoot, "lab", nil, confirmed)
		if err == nil || !strings.Contains(err.Error(), "decode ownership resource") {
			t.Fatalf("malformed canonical recovery error = %v", err)
		}
	})

	t.Run("foreign canonical context", func(t *testing.T) {
		caseRoot := t.TempDir()
		record := valid
		record.Context = "other"
		if err := ownership.SaveResource(caseRoot, record); err != nil {
			t.Fatalf("SaveResource: %v", err)
		}
		err := ValidateDestroyCephOwnershipRecovery(state, nil, caseRoot, "lab", nil, confirmed)
		if err == nil || !strings.Contains(err.Error(), "context") {
			t.Fatalf("foreign-context canonical recovery error = %v", err)
		}
	})
}

func TestApplyDestroyCephOwnershipRecoveryExtraVar(t *testing.T) {
	plan := WorkflowPlan{}
	confirmed := map[string]string{"ceph-b": cephRecoveryTestFSID, "ceph-a": "1088ddee-875b-11f1-9b98-303ea72d7724"}
	if err := ApplyDestroyCephOwnershipRecoveryExtraVar(&plan, confirmed); err != nil {
		t.Fatalf("ApplyDestroyCephOwnershipRecoveryExtraVar: %v", err)
	}
	want := `{"bootwright_ceph_destroy_confirmed_fsids":{"ceph-a":"1088ddee-875b-11f1-9b98-303ea72d7724","ceph-b":"2088ddee-875b-11f1-9b98-303ea72d7724"}}`
	if len(plan.ExtraVarPairs) != 1 || plan.ExtraVarPairs[0] != want {
		t.Fatalf("extra vars = %v, want %q", plan.ExtraVarPairs, want)
	}
}

func cephRecoveryTestCluster() v1alpha1.StorageCluster {
	return v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph-a"},
		Spec: v1alpha1.StorageClusterSpec{
			Type:       v1alpha1.StorageClusterTypeCeph,
			Management: v1alpha1.StorageClusterManagementManaged,
			Ceph: &v1alpha1.StorageClusterCephSpec{
				Cephadm: v1alpha1.StorageCephadmSpec{
					Bootstrap: v1alpha1.StorageCephadmBootstrap{Node: "seed"},
				},
				Topology: v1alpha1.StorageCephTopology{
					Nodes: []v1alpha1.StorageCephNode{{
						Name:       "seed",
						MachineRef: v1alpha1.LocalObjectReference{Name: "srv4203"},
					}},
				},
			},
		},
	}
}
