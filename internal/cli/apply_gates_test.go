package cli

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/workspace"
)

func TestApplyRefusesForeignContextEvidenceBeforeItCanBeOverwritten(t *testing.T) {
	ownershipDir := t.TempDir()
	if err := ownership.SaveResource(ownershipDir, ownership.ResourceRecord{
		Kind:       string(ownership.KindStorageCluster),
		Name:       "ceph-a",
		Owner:      ownership.Owner,
		Context:    "foreign",
		Cluster:    "ceph-a",
		Host:       "seed-a",
		Attributes: map[string]string{"seedHost": "seed-a"},
	}); err != nil {
		t.Fatalf("SaveResource: %v", err)
	}
	invocation := resolvedInvocation{
		verb:        invocationApply,
		contextName: "lab",
		flags:       invocationFlags{mode: workflow.ApplyModeReconcile},
	}
	_, _, err := applyOwnershipRecords(workspace.Context{Name: "lab", OwnershipDir: ownershipDir}, false, &invocation)
	if err == nil {
		t.Fatal("apply accepted foreign-context evidence from its ownership store")
	}
	for _, want := range []string{"storage-cluster/ceph-a", "context=\"foreign\"", "context \"lab\"", "noncanonical identity", "do not belong to this context", "remove it only after proving it stale", "`bootwright apply", "--mode reconcile", "--context lab`"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("apply ownership refusal %q does not contain %q", err, want)
		}
	}
	if records, warnings, dryErr := applyOwnershipRecords(workspace.Context{Name: "lab", OwnershipDir: ownershipDir}, true, &invocation); dryErr != nil || len(records) != 0 || len(warnings) != 1 {
		t.Fatalf("dry-run evidence = records=%v warnings=%v err=%v, want one surfaced warning and no trusted record", records, warnings, dryErr)
	}
}

func TestApplyOwnershipEvidenceRequiresCurrentCanonicalIdentity(t *testing.T) {
	tests := []struct {
		name       string
		apiVersion string
		owner      string
		context    string
		want       string
	}{
		{name: "wrong API", apiVersion: "bootwright.io/ownership/v2", owner: ownership.Owner, context: "lab", want: "apiVersion"},
		{name: "wrong owner", apiVersion: "bootwright.io/ownership/v1alpha1", owner: "other", context: "lab", want: "owner"},
		{name: "missing context", apiVersion: "bootwright.io/ownership/v1alpha1", owner: ownership.Owner, want: "context"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ownershipDir := t.TempDir()
			if err := ownership.SaveResource(ownershipDir, ownership.ResourceRecord{
				APIVersion: tc.apiVersion,
				Kind:       string(ownership.KindStorageCluster),
				Name:       "ceph-a",
				Owner:      tc.owner,
				Context:    tc.context,
				Cluster:    "ceph-a",
				Host:       "seed-a",
				Attributes: map[string]string{"seedHost": "seed-a"},
			}); err != nil {
				t.Fatalf("SaveResource: %v", err)
			}
			ctx := workspace.Context{Name: "lab", OwnershipDir: ownershipDir}
			_, _, err := applyOwnershipRecords(ctx, false, nil)
			if err == nil || !strings.Contains(err.Error(), "noncanonical identity") || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("apply ownership identity error = %v, want noncanonical %s refusal", err, tc.want)
			}
			records, warnings, err := applyOwnershipRecords(ctx, true, nil)
			if err != nil || len(records) != 0 || len(warnings) != 1 {
				t.Fatalf("dry-run evidence = records=%v warnings=%v err=%v, want one surfaced warning and no trusted record", records, warnings, err)
			}
		})
	}
}
