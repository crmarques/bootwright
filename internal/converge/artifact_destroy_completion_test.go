package converge

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/ownership"
)

func exactArtifactOwnerRecord() ownership.ResourceRecord {
	return ownership.ResourceRecord{
		APIVersion: "bootwright.io/ownership/v1alpha1",
		Kind:       ownershipInfraComponentKind,
		Name:       "provider-a-artifacts",
		Owner:      ownership.Owner,
		Role:       ownership.RoleOwner,
		Context:    "lab",
		Host:       "service-host-a",
		Provider:   "provider-a",
		Labels: map[string]string{
			"bootwright.kind":     artifactServerRecordKindLabel,
			"bootwright.provider": "provider-a",
			"bootwright.name":     "artifacts",
		},
		Attributes: map[string]string{"componentKind": artifactServerRecordKindLabel},
	}
}

func TestArtifactServerOwnerHostsRequiresExactSelectedAuthority(t *testing.T) {
	record := exactArtifactOwnerRecord()
	hosts, err := artifactServerOwnerHosts(
		[]ownership.ResourceRecord{record},
		"lab",
		[]string{InfraComponentReclaimExtraVar + "=provider-a-artifacts"},
	)
	if err != nil || !slices.Equal(hosts, []string{"service-host-a"}) {
		t.Fatalf("exact artifact owner hosts = %v, %v", hosts, err)
	}
	for _, mutate := range []func(*ownership.ResourceRecord){
		func(value *ownership.ResourceRecord) { value.APIVersion = "foreign/v1" },
		func(value *ownership.ResourceRecord) { value.Context = "foreign" },
		func(value *ownership.ResourceRecord) { value.Host = "" },
		func(value *ownership.ResourceRecord) { value.Provider = "copied" },
		func(value *ownership.ResourceRecord) { value.Attributes["componentKind"] = "proxy" },
	} {
		bad := exactArtifactOwnerRecord()
		mutate(&bad)
		if _, err := artifactServerOwnerHosts([]ownership.ResourceRecord{bad}, "lab", []string{InfraComponentReclaimExtraVar + "=provider-a-artifacts"}); err == nil || !strings.Contains(err.Error(), "refusing artifact-server") {
			t.Fatalf("malformed selected record accepted: %+v, %v", bad, err)
		}
	}
}

func TestResetArtifactServerConvergenceEvidenceTargetsOnlyReclaimedOwnerHosts(t *testing.T) {
	runsDir := t.TempDir()
	hosts := []string{"service-host-a", "service-host-b", "unrelated-host"}
	for _, host := range hosts {
		resourceID := workflow.ApplyTaskKindInfraComponentServices + "/infra-component." + host
		path := workflow.ConvergeSafetyRecordPath(runsDir, resourceID)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("evidence"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := resetArtifactServerConvergenceEvidence(runsDir, hosts[:2]); err != nil {
		t.Fatal(err)
	}
	for _, host := range hosts[:2] {
		path := workflow.ConvergeSafetyRecordPath(runsDir, workflow.ApplyTaskKindInfraComponentServices+"/infra-component."+host)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("reclaimed host evidence %s remains: %v", host, err)
		}
	}
	unrelated := workflow.ConvergeSafetyRecordPath(runsDir, workflow.ApplyTaskKindInfraComponentServices+"/infra-component.unrelated-host")
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatalf("unrelated host evidence was removed: %v", err)
	}
}
