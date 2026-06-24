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

func TestResourceRecordLoadsNumericAttributes(t *testing.T) {
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
  "attributes": {
    "redfishUnit": "bootwright-sushy-lab-libvirt-provider.service",
    "redfishPort": 8000,
    "vMediaPort": 8001
  },
  "updatedAt": "2026-06-06T14:11:02Z"
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
	attrs := records[0].Attributes
	if attrs["redfishPort"] != "8000" || attrs["vMediaPort"] != "8001" {
		t.Fatalf("numeric attributes not coerced to strings: %#v", attrs)
	}
	if attrs["redfishUnit"] != "bootwright-sushy-lab-libvirt-provider.service" {
		t.Fatalf("string attribute lost: %#v", attrs)
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

// The sensitive scan must tolerate benign connection strings and paths (ssh args,
// known-hosts paths, FQDNs) that merely contain a word like token/kubeconfig, so a
// recorded host stays loadable and reclaimable, while still rejecting actual
// embedded credential material and fields named like a secret.
func TestSensitiveScanTolersBenignConnectionDataButRejectsCredentials(t *testing.T) {
	benign := ResourceRecord{
		Kind: "libvirt-domain", Name: "node0", Owner: Owner, Host: "h",
		HostFacts: map[string]string{
			"ansible_ssh_common_args": "-o UserKnownHostsFile=/var/lib/bootwright/token-lab/known_hosts -o ProxyCommand=ssh kubeconfig-bastion",
		},
	}
	if err := ValidateResource(benign); err != nil {
		t.Fatalf("benign connection string must validate, got: %v", err)
	}
	pem := ResourceRecord{Kind: "libvirt-domain", Name: "node1", Owner: Owner, Attributes: map[string]string{"blob": "-----BEGIN PRIVATE KEY-----abc"}}
	if err := ValidateResource(pem); err == nil {
		t.Fatal("embedded PEM value accepted; value scan too narrow")
	}
	keyed := ResourceRecord{Kind: "libvirt-domain", Name: "node2", Owner: Owner, Attributes: map[string]string{"kubeconfig": "/some/path"}}
	if err := ValidateResource(keyed); err == nil {
		t.Fatal("field named like a secret accepted")
	}
}

func TestResourceRecordRejectsPathTraversal(t *testing.T) {
	err := SaveResource(t.TempDir(), ResourceRecord{
		Kind:  "libvirt-domain",
		Name:  "cluster-a-machine-0",
		Paths: []string{"/var/lib/../../etc/passwd"},
	})
	if err == nil {
		t.Fatal("SaveResource unexpectedly accepted owned path containing ..")
	}
}

func TestFilterByContextDropsForeignContextRecords(t *testing.T) {
	records := []ResourceRecord{
		{Kind: "libvirt-domain", Name: "lab-a", Context: "lab"},
		{Kind: "libvirt-domain", Name: "other-a", Context: "other"},
		{Kind: "libvirt-network", Name: "legacy", Context: ""},
	}
	got := FilterByContext(records, "lab")
	if len(got) != 2 {
		t.Fatalf("FilterByContext kept %d records, want 2 (matching + unstamped): %+v", len(got), got)
	}
	for _, record := range got {
		if record.Name == "other-a" {
			t.Fatalf("FilterByContext kept a foreign-context record: %+v", record)
		}
	}
}

func TestFilterByContextEmptyContextKeepsAll(t *testing.T) {
	records := []ResourceRecord{
		{Kind: "libvirt-domain", Name: "lab-a", Context: "lab"},
		{Kind: "libvirt-domain", Name: "other-a", Context: "other"},
	}
	if got := FilterByContext(records, ""); len(got) != len(records) {
		t.Fatalf("FilterByContext with empty context kept %d records, want %d", len(got), len(records))
	}
}

// One unreadable/invalid record must not hide every other recorded resource: a
// destroy sweep has to be able to reclaim the good ones (and a record can be
// written by the Ansible role, which does not run Go's validation, so an invalid
// record can land on disk). LoadResources skips it; LoadResourcesWithWarnings
// surfaces the skip.
func TestLoadResourcesSkipsBadRecordWithoutDroppingGood(t *testing.T) {
	root := t.TempDir()
	good := ResourceRecord{Kind: "libvirt-domain", Name: "good-machine", Owner: Owner, Host: "h"}
	if err := SaveResource(root, good); err != nil {
		t.Fatalf("SaveResource: %v", err)
	}
	// A corrupt file the atomic writer could leave behind, plus a syntactically
	// valid record that trips the load-time sensitive scan (a field named like a
	// secret): both must be skipped, not fatal.
	corruptDir := filepath.Join(root, ResourceDirName, "libvirt-domain")
	if err := os.WriteFile(filepath.Join(corruptDir, "truncated.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}
	sensitiveDir := filepath.Join(root, ResourceDirName, "infra-component")
	if err := os.MkdirAll(sensitiveDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sensitiveDir, "leaky.json"),
		[]byte(`{"kind":"infra-component","name":"leaky","attributes":{"password":"abc"}}`), 0o600); err != nil {
		t.Fatalf("write sensitive: %v", err)
	}

	records, err := LoadResources(root)
	if err != nil {
		t.Fatalf("LoadResources must not fail on a bad record: %v", err)
	}
	if len(records) != 1 || records[0].Name != "good-machine" {
		t.Fatalf("expected only the good record, got %+v", records)
	}
	_, warnings, err := LoadResourcesWithWarnings(root)
	if err != nil {
		t.Fatalf("LoadResourcesWithWarnings: %v", err)
	}
	if len(warnings) != 2 {
		t.Fatalf("expected 2 skip warnings (corrupt + sensitive), got %d: %v", len(warnings), warnings)
	}
}
