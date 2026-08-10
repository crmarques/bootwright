package ownership

import (
	"os"
	"path/filepath"
	"strings"
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

func TestLoadResourcesSkipsBadRecordWithoutDroppingGood(t *testing.T) {
	root := t.TempDir()
	good := ResourceRecord{Kind: "libvirt-domain", Name: "good-machine", Owner: Owner, Host: "h"}
	if err := SaveResource(root, good); err != nil {
		t.Fatalf("SaveResource: %v", err)
	}
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

func TestLoadResourcesRejectsRecordStoredUnderAnotherIdentity(t *testing.T) {
	root := t.TempDir()
	record := ResourceRecord{Kind: "libvirt-domain", Name: "machine-a", Owner: Owner}
	if err := SaveResource(root, record); err != nil {
		t.Fatalf("SaveResource: %v", err)
	}
	canonical, err := ResourcePath(root, record)
	if err != nil {
		t.Fatal(err)
	}
	misnamed := filepath.Join(filepath.Dir(canonical), "machine-b.json")
	if err := os.Rename(canonical, misnamed); err != nil {
		t.Fatalf("rename record: %v", err)
	}

	records, warnings, err := LoadResourcesWithWarnings(root)
	if err != nil {
		t.Fatalf("LoadResourcesWithWarnings: %v", err)
	}
	if len(records) != 0 || len(warnings) != 1 {
		t.Fatalf("misnamed ownership evidence produced records=%+v warnings=%v", records, warnings)
	}
	if !strings.Contains(warnings[0].Error(), canonical) {
		t.Fatalf("warning does not name canonical record path %q: %v", canonical, warnings[0])
	}
}

func TestLoadResourcesRejectsSymlinkedRecord(t *testing.T) {
	root := t.TempDir()
	record := ResourceRecord{Kind: "libvirt-domain", Name: "machine-a", Owner: Owner}
	if err := SaveResource(root, record); err != nil {
		t.Fatalf("SaveResource: %v", err)
	}
	canonical, err := ResourcePath(root, record)
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(filepath.Dir(canonical), "machine-link.json")
	if err := os.Symlink(canonical, link); err != nil {
		t.Fatalf("symlink record: %v", err)
	}

	records, warnings, err := LoadResourcesWithWarnings(root)
	if err != nil {
		t.Fatalf("LoadResourcesWithWarnings: %v", err)
	}
	if len(records) != 1 || records[0].Name != record.Name || len(warnings) != 1 {
		t.Fatalf("symlinked ownership evidence produced records=%+v warnings=%v", records, warnings)
	}
	if !strings.Contains(warnings[0].Error(), "symbolic links") {
		t.Fatalf("symlink warning = %v", warnings[0])
	}
}

func TestLoadResourcesRejectsUnsafeOwnershipDirectories(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*testing.T) string
		want    string
	}{
		{
			name: "ownership root symlink",
			prepare: func(t *testing.T) string {
				root := filepath.Join(t.TempDir(), "ownership")
				if err := os.Symlink(t.TempDir(), root); err != nil {
					t.Fatalf("symlink ownership root: %v", err)
				}
				return root
			},
			want: "symbolic links",
		},
		{
			name: "resource root symlink",
			prepare: func(t *testing.T) string {
				root := t.TempDir()
				if err := os.Symlink(t.TempDir(), filepath.Join(root, ResourceDirName)); err != nil {
					t.Fatalf("symlink resource root: %v", err)
				}
				return root
			},
			want: "symbolic links",
		},
		{
			name: "resource root file",
			prepare: func(t *testing.T) string {
				root := t.TempDir()
				if err := os.WriteFile(filepath.Join(root, ResourceDirName), []byte("not a directory"), 0o600); err != nil {
					t.Fatalf("write resource root file: %v", err)
				}
				return root
			},
			want: "not a directory",
		},
		{
			name: "kind directory symlink",
			prepare: func(t *testing.T) string {
				root := t.TempDir()
				base := filepath.Join(root, ResourceDirName)
				if err := os.MkdirAll(base, 0o700); err != nil {
					t.Fatalf("create resource root: %v", err)
				}
				if err := os.Symlink(t.TempDir(), filepath.Join(base, "controller-name-resolver")); err != nil {
					t.Fatalf("symlink ownership kind directory: %v", err)
				}
				return root
			},
			want: "symbolic links",
		},
		{
			name: "kind directory file",
			prepare: func(t *testing.T) string {
				root := t.TempDir()
				base := filepath.Join(root, ResourceDirName)
				if err := os.MkdirAll(base, 0o700); err != nil {
					t.Fatalf("create resource root: %v", err)
				}
				if err := os.WriteFile(filepath.Join(base, "controller-name-resolver"), []byte("not a directory"), 0o600); err != nil {
					t.Fatalf("write ownership kind file: %v", err)
				}
				return root
			},
			want: "not a directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := tt.prepare(t)
			records, warnings, err := LoadResourcesWithWarnings(root)
			if err != nil {
				t.Fatalf("LoadResourcesWithWarnings: %v", err)
			}
			if len(records) != 0 || len(warnings) != 1 {
				t.Fatalf("unsafe ownership directory produced records=%+v warnings=%v", records, warnings)
			}
			if !strings.Contains(warnings[0].Error(), tt.want) {
				t.Fatalf("unsafe ownership directory warning = %v, want %q", warnings[0], tt.want)
			}
		})
	}
}
