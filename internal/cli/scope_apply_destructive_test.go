package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func TestReclaimDestructiveDescriptors(t *testing.T) {
	if got := reclaimDestructiveDescriptors("/dev/sdb", []string{"ceph"}); len(got) != 1 || !strings.Contains(got[0], "/dev/sdb") || !strings.Contains(got[0], "ceph") {
		t.Fatalf("reclaim of an owned cluster must yield a data-loss descriptor, got %v", got)
	}
	if got := reclaimDestructiveDescriptors("/dev/sdb", nil); got != nil {
		t.Fatalf("reclaim with no owned cluster must not add a data-loss descriptor, got %v", got)
	}
	if got := reclaimDestructiveDescriptors("", []string{"ceph"}); got != nil {
		t.Fatalf("no reclaim devices must not add a data-loss descriptor, got %v", got)
	}
}

func TestReclaimUnmatchedError(t *testing.T) {
	err := reclaimUnmatchedError([]string{"/dev/disk/by-id/wwn-0x5000"}, []string{"ceph1"}, []string{"/dev/sdb"})
	msg := err.Error()
	for _, want := range []string{"--reclaim-devices entry", "/dev/disk/by-id/wwn-0x5000", "does not match", "ceph1", "exact declared path", "/dev/sdb"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("reclaim unmatched error missing %q, got %q", want, msg)
		}
	}
	multi := reclaimUnmatchedError([]string{"/dev/x", "/dev/y"}, []string{"ceph1"}, nil).Error()
	if !strings.Contains(multi, "entries") || !strings.Contains(multi, "do not match") {
		t.Fatalf("multiple unmatched entries must pluralize, got %q", multi)
	}
	if !strings.Contains(multi, "declare no OSD devices") {
		t.Fatalf("no declared devices must state so, got %q", multi)
	}
}

func TestDestructiveOverrideYesGuard(t *testing.T) {
	objs := []string{"Machine/db1", "StorageCluster/ceph"}
	cases := []struct {
		name        string
		destructive []string
		yes         bool
		allow       bool
		wantErr     bool
	}{
		{"yes without allow refuses", objs, true, false, true},
		{"yes with allow proceeds", objs, true, true, false},
		{"interactive falls through to confirm", objs, false, false, false},
		{"no destructive objects proceeds", nil, true, false, false},
		{"allow with no yes proceeds", objs, false, true, false},
		{"reinstall descriptor with yes refuses", []string{"reinstall ContainerCluster/dc1-ocp (live cluster with no install record; --override reinstalls it and wipes its node disks)"}, true, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := destructiveOverrideYesGuard(tc.destructive, tc.yes, tc.allow)
			if tc.wantErr != (err != nil) {
				t.Fatalf("guard(destructive=%v yes=%v allow=%v) err=%v, wantErr=%v", tc.destructive, tc.yes, tc.allow, err, tc.wantErr)
			}
			if err != nil {
				for _, obj := range tc.destructive {
					if !strings.Contains(err.Error(), obj) {
						t.Fatalf("refusal must name %q: %v", obj, err)
					}
				}
				if !strings.Contains(err.Error(), "--allow-destroy") {
					t.Fatalf("refusal must point at --allow-destroy: %v", err)
				}
				if !strings.Contains(err.Error(), "--clusters") {
					t.Fatalf("refusal must name the --clusters escape to narrow the destructive set: %v", err)
				}
			}
		})
	}
}

func TestWarnDestructiveApplyDisclosesOnEveryAcceptedPath(t *testing.T) {
	destructive := []string{"reinstall ContainerCluster/dc1-ocp (installed record matches desired inputs but the cluster does not report Available=True; to keep its data, repair the cluster to Available=True and re-run plain apply — --override reinstalls it and wipes its node disks)"}

	var out bytes.Buffer
	warnDestructiveApply(&out, destructive)
	for _, want := range []string{"will DESTROY data", "reinstall ContainerCluster/dc1-ocp", "--clusters"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("data-loss warning must contain %q, got %q", want, out.String())
		}
	}

	out.Reset()
	warnDestructiveApply(&out, nil)
	if out.Len() != 0 {
		t.Fatalf("no destructive objects must print no warning, got %q", out.String())
	}

	if got := destructiveApplyConfirmPrompt(destructive, true); strings.Contains(got, "DESTRUCTIVE") {
		t.Fatalf("--allow-destroy pre-accepts the escalated prompt, got %q", got)
	}
	if got := destructiveApplyConfirmPrompt(destructive, false); !strings.Contains(got, "DESTRUCTIVE") {
		t.Fatalf("interactive destructive apply must escalate the prompt, got %q", got)
	}
	if got := destructiveApplyConfirmPrompt(nil, false); strings.Contains(got, "DESTRUCTIVE") {
		t.Fatalf("non-destructive apply must keep the plain prompt, got %q", got)
	}
}

func classifiedSubObjectDriftObjects(t *testing.T) []workflow.ObjectClassification {
	t.Helper()
	runsDir := t.TempDir()
	now := time.Unix(1700000000, 0)
	state := func() v1alpha1.State {
		cluster := func(name string) v1alpha1.StorageCluster {
			return v1alpha1.StorageCluster{
				Metadata: v1alpha1.Metadata{Name: name},
				Spec: v1alpha1.StorageClusterSpec{
					Type:       v1alpha1.StorageClusterTypeCeph,
					Management: v1alpha1.StorageClusterManagementManaged,
					Ceph:       &v1alpha1.StorageClusterCephSpec{Distribution: v1alpha1.StorageCephDistributionOSS},
				},
			}
		}
		pool := func(clusterName string) v1alpha1.StoragePool {
			return v1alpha1.StoragePool{
				Metadata: v1alpha1.Metadata{Name: "rbd"},
				Spec: v1alpha1.StoragePoolSpec{
					StorageClusterRef: v1alpha1.LocalObjectReference{Name: clusterName},
					Ceph:              v1alpha1.StoragePoolCephSpec{Role: v1alpha1.StoragePoolRoleRBD},
				},
			}
		}
		return v1alpha1.State{
			StorageClusters: []v1alpha1.StorageCluster{cluster("ceph-a"), cluster("ceph-b")},
			StoragePools:    []v1alpha1.StoragePool{pool("ceph-a"), pool("ceph-b")},
		}
	}
	before := state()
	after := state()
	after.StoragePools[0].Spec.Ceph.Type = v1alpha1.StoragePoolTypeErasureCode
	for _, name := range []string{"ceph-a", "ceph-b"} {
		if err := workflow.MarkStorageSubObjectsConvergeSafety(runsDir, "ctx", "apply", before, name, workflow.ConvergeSafetyStatusReconciled, now); err != nil {
			t.Fatalf("mark sub-objects of %s: %v", name, err)
		}
	}
	tasks, err := workflow.PlanApplyTasksChecked(converge.AllScope.ApplyTarget(), after)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	objects, err := workflow.ClassifyApplyObjects(tasks, runsDir)
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	return objects
}

func TestEmitApplyDataLossWarningsAuthorizeDriftedSubObjects(t *testing.T) {
	objects := classifiedSubObjectDriftObjects(t)

	var out bytes.Buffer
	plan := &converge.WorkflowPlan{}
	emitApplyDataLossWarningsAndVars(&out, workflow.ApplyModeOverride, objects, nil, plan, "", nil, "", nil, false)
	vars := strings.Join(plan.ExtraVarPairs, "\n")
	if !strings.Contains(vars, "bootwright_ceph_subobject_rebuild_authorized=ceph-a/rbd") {
		t.Fatalf("record-classified drifted sub-object must be authorized for its acked rebuild, got %v", plan.ExtraVarPairs)
	}
	if strings.Contains(vars, "ceph-b/rbd") {
		t.Fatalf("an undrifted sub-object must NOT be authorized without --allow-destroy, got %v", plan.ExtraVarPairs)
	}
	if !strings.Contains(out.String(), "StoragePool/ceph-a.rbd") {
		t.Fatalf("data-loss warning must name the drifted sub-object, got %q", out.String())
	}

	allowPlan := &converge.WorkflowPlan{}
	emitApplyDataLossWarningsAndVars(&bytes.Buffer{}, workflow.ApplyModeOverride, objects, nil, allowPlan, "", nil, "", nil, true)
	allowVars := strings.Join(allowPlan.ExtraVarPairs, "\n")
	if !strings.Contains(allowVars, "ceph-a/rbd") || !strings.Contains(allowVars, "ceph-b/rbd") {
		t.Fatalf("--allow-destroy must authorize every selected cluster's sub-objects so live-only drift has the documented path forward, got %v", allowPlan.ExtraVarPairs)
	}

	continuePlan := &converge.WorkflowPlan{}
	emitApplyDataLossWarningsAndVars(&bytes.Buffer{}, workflow.ApplyModeContinue, objects, nil, continuePlan, "", nil, "", nil, true)
	if strings.Contains(strings.Join(continuePlan.ExtraVarPairs, "\n"), "bootwright_ceph_subobject_rebuild_authorized") {
		t.Fatalf("non-override apply must never authorize sub-object destroys, got %v", continuePlan.ExtraVarPairs)
	}
}

func TestEmitApplyDataLossWarningsNamesOCPReinstalls(t *testing.T) {
	reinstalls := []string{"reinstall ContainerCluster/dc1-ocp (recorded install inputs differ from current desired inputs — e.g. rotated secret material or changed install config; --override reinstalls the cluster and wipes its node disks)"}

	var out bytes.Buffer
	plan := &converge.WorkflowPlan{}
	emitApplyDataLossWarningsAndVars(&out, workflow.ApplyModeOverride, nil, nil, plan, "", nil, "", reinstalls, false)
	if !strings.Contains(out.String(), "reinstall ContainerCluster/dc1-ocp") || !strings.Contains(out.String(), "node disks wiped") {
		t.Fatalf("override reinstall warning must name the reinstalled cluster(s), got %q", out.String())
	}

	out.Reset()
	emitApplyDataLossWarningsAndVars(&out, workflow.ApplyModeOverride, nil, nil, &converge.WorkflowPlan{}, "", nil, "", nil, false)
	if strings.Contains(out.String(), "reinstall ContainerCluster/") {
		t.Fatalf("no reinstall descriptors must print no reinstall warning, got %q", out.String())
	}
}
