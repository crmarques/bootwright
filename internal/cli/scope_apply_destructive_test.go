package cli

import (
	"bytes"
	"strings"
	"testing"

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

func TestEmitApplyDataLossWarningsNamesOCPReinstalls(t *testing.T) {
	reinstalls := []string{"reinstall ContainerCluster/dc1-ocp (recorded install inputs differ from current desired inputs — e.g. rotated secret material or changed install config; --override reinstalls the cluster and wipes its node disks)"}

	var out bytes.Buffer
	plan := &converge.WorkflowPlan{}
	emitApplyDataLossWarningsAndVars(&out, workflow.ApplyModeOverride, nil, nil, plan, "", nil, "", reinstalls)
	if !strings.Contains(out.String(), "reinstall ContainerCluster/dc1-ocp") || !strings.Contains(out.String(), "node disks wiped") {
		t.Fatalf("override reinstall warning must name the reinstalled cluster(s), got %q", out.String())
	}

	out.Reset()
	emitApplyDataLossWarningsAndVars(&out, workflow.ApplyModeOverride, nil, nil, &converge.WorkflowPlan{}, "", nil, "", nil)
	if strings.Contains(out.String(), "reinstall ContainerCluster/") {
		t.Fatalf("no reinstall descriptors must print no reinstall warning, got %q", out.String())
	}
}
