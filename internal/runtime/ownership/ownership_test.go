package ownership

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResourceRecordRoundTrip(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 6, 6, 9, 0, 0, 0, time.UTC)
	record := ResourceRecord{
		APIVersion: "bootwright.io/ownership/v1alpha1",
		Kind:       "libvirt-domain",
		Name:       "cluster-a-machine-0",
		Owner:      "bootwright",
		Context:    "lab",
		Host:       "provider-0",
		Provider:   "libvirt",
		Cluster:    "cluster-a",
		Machine:    "machine-0",
		Paths:      []string{"/var/lib/libvirt/images/bootwright/lab/clusters/cluster-a/machines/machine-0"},
		HostFacts:  map[string]string{"ansible_connection": "local"},
		Labels:     map[string]string{"bootwright.kind": "machine"},
		Attributes: map[string]string{"network": "bw-cluster-a"},
		UpdatedAt:  now,
	}
	if err := SaveResource(root, record); err != nil {
		t.Fatalf("SaveResource: %v", err)
	}
	records, err := LoadResources(root)
	if err != nil {
		t.Fatalf("LoadResources: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	got := records[0]
	if got.Kind != record.Kind || got.Name != record.Name || !got.UpdatedAt.Equal(now) {
		t.Fatalf("record = %+v, want %+v", got, record)
	}
	wantPath := filepath.Join(root, ResourceDirName, "libvirt-domain", "cluster-a-machine-0.json")
	path, err := ResourcePath(root, record)
	if err != nil {
		t.Fatalf("ResourcePath: %v", err)
	}
	if path != wantPath {
		t.Fatalf("ResourcePath = %q, want %q", path, wantPath)
	}
}

func TestResourceRecordLoadsNaiveUTCUpdatedAt(t *testing.T) {
	root := t.TempDir()
	recordDir := filepath.Join(root, ResourceDirName, "bmc-emulator")
	if err := os.MkdirAll(recordDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(recordDir, "lab-libvirt-provider.json")
	data := []byte(`{
  "apiVersion": "bootwright.io/ownership/v1alpha1",
  "kind": "bmc-emulator",
  "name": "lab-libvirt-provider",
  "owner": "bootwright",
  "updatedAt": "2026-06-06T14:11:02.479692"
}
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	records, err := LoadResources(root)
	if err != nil {
		t.Fatalf("LoadResources: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	want := time.Date(2026, 6, 6, 14, 11, 2, 479692000, time.UTC)
	if !records[0].UpdatedAt.Equal(want) {
		t.Fatalf("UpdatedAt = %s, want %s", records[0].UpdatedAt.Format(time.RFC3339Nano), want.Format(time.RFC3339Nano))
	}
}

func TestResourceRecordRejectsUnsafeNames(t *testing.T) {
	err := SaveResource(t.TempDir(), ResourceRecord{Kind: "libvirt/domain", Name: "../machine"})
	if err == nil {
		t.Fatal("SaveResource unexpectedly accepted unsafe record path")
	}
}

func TestResourceRecordRejectsSecretLikeData(t *testing.T) {
	err := SaveResource(t.TempDir(), ResourceRecord{
		Kind:       "libvirt-domain",
		Name:       "cluster-a-machine-0",
		Attributes: map[string]string{"password": "redacted"},
	})
	if err == nil {
		t.Fatal("SaveResource unexpectedly accepted sensitive field")
	}
}
