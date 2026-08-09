package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/render"
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

func TestFilterReclaimAuthorizationCoversOnlyUnboundedManagedAllDevices(t *testing.T) {
	cluster := func(name string, osd *v1alpha1.StorageCephNodeOSD) v1alpha1.StorageCluster {
		return v1alpha1.StorageCluster{
			Metadata: v1alpha1.Metadata{Name: name},
			Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{{
				Name:  name + "-node",
				Roles: []string{v1alpha1.StorageCephRoleOSD},
				OSD:   osd,
			}}}}},
		}
	}
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{
		cluster("unbounded", &v1alpha1.StorageCephNodeOSD{DataDevices: &v1alpha1.StorageCephDeviceSelection{All: true}}),
		cluster("model", &v1alpha1.StorageCephNodeOSD{DataDevices: &v1alpha1.StorageCephDeviceSelection{All: true, Model: "DATA"}}),
		cluster("limited", &v1alpha1.StorageCephNodeOSD{DataDevices: &v1alpha1.StorageCephDeviceSelection{All: true, Limit: 1}}),
		cluster("unmanaged", &v1alpha1.StorageCephNodeOSD{DataDevices: &v1alpha1.StorageCephDeviceSelection{All: true}, Unmanaged: true}),
	}}
	objects := make([]workflow.ObjectClassification, 0, len(state.StorageClusters))
	for _, storage := range state.StorageClusters {
		objects = append(objects, workflow.ObjectClassification{Kind: workflow.ObjectKindStorageCluster, Label: "StorageCluster/" + storage.Metadata.Name})
	}

	got := filterReclaimAuthorizedClusters(state, objects)
	if len(got) != 1 || got[0] != "unbounded" {
		t.Fatalf("filterReclaimAuthorizedClusters = %v, want only unbounded; a narrowing or unmanaged selector must never inherit the all-disks wipe", got)
	}
}

func TestWarnReclaimUnownedDevicesStatesBothReadings(t *testing.T) {
	var buf bytes.Buffer
	warnReclaimUnownedDevices(&buf, "/dev/nvme0n1")
	got := buf.String()
	for _, want := range []string{"/dev/nvme0n1", "unowned-devices", "orphan", "LIVE OSD", "mounted"} {
		if !strings.Contains(got, want) {
			t.Errorf("the unowned-devices warning must name %q: the gate can no longer tell an orphan from a live OSD for the operator, so it must state both readings and the one refusal no token relaxes. got %q", want, got)
		}
	}
}

func TestUnownedDeviceAuthorizationExtraVarRidesOnlyTheToken(t *testing.T) {
	var plan converge.WorkflowPlan
	converge.ApplyCephUnownedDeviceAuthorizationExtraVar(&plan, false)
	if len(plan.ExtraVarPairs) != 0 {
		t.Fatalf("an unauthorized run must send no unowned-device extra var, got %v", plan.ExtraVarPairs)
	}
	converge.ApplyCephUnownedDeviceAuthorizationExtraVar(&plan, true)
	if len(plan.ExtraVarPairs) != 1 || plan.ExtraVarPairs[0] != converge.CephAuthorizeUnownedDevicesExtraVar+"=true" {
		t.Fatalf("the token must reach the roles as %s=true, got %v", converge.CephAuthorizeUnownedDevicesExtraVar, plan.ExtraVarPairs)
	}
}

func TestForeignDaemonReclaimRidesTheTokenAndAStorageConverge(t *testing.T) {
	storageTasks := []workflow.ApplyTask{{Entry: workflow.TaskLedgerEntry{Kind: workflow.ApplyTaskKindStorageCluster, Cluster: "ceph"}}}
	prereqTasks := []workflow.ApplyTask{{Entry: workflow.TaskLedgerEntry{Kind: workflow.ApplyTaskKindStorageInfra, Cluster: "ceph"}}}
	cases := []struct {
		name     string
		given    []string
		tasks    []workflow.ApplyTask
		wantVar  bool
		wantWarn bool
		wantIdle bool
	}{
		{"token and a converge arms the reclaim", []string{authorizeForeignDaemons}, storageTasks, true, true, false},
		{"a converge without the token stays closed", nil, storageTasks, false, false, false},
		{"the token without a storage converge is inert and says so", []string{authorizeForeignDaemons}, prereqTasks, false, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			auth, err := parseAuthorizations(tc.given, authorizeVerbApply)
			if err != nil {
				t.Fatalf("parseAuthorizations: %v", err)
			}
			var plan converge.WorkflowPlan
			var buf bytes.Buffer
			applyForeignCephadmDaemons(&buf, &plan, auth, tc.tasks)
			hasVar := len(plan.ExtraVarPairs) == 1 && plan.ExtraVarPairs[0] == converge.CephAuthorizeForeignDaemonsExtraVar+"=true"
			if hasVar != tc.wantVar {
				t.Fatalf("extra var %s: want %v, got %v", converge.CephAuthorizeForeignDaemonsExtraVar, tc.wantVar, plan.ExtraVarPairs)
			}
			if warned := strings.Contains(buf.String(), authorizeForeignDaemons); warned != tc.wantWarn {
				t.Fatalf("warning: want %v, got %q", tc.wantWarn, buf.String())
			}
			if idle := len(auth.unused()) > 0; idle != tc.wantIdle {
				t.Fatalf("a token the run consumed must never report as inert and one it could not consume must: want inert=%v, got %v", tc.wantIdle, auth.unused())
			}
		})
	}
}

func TestIncompleteBootstrapRebuildAuthorizationNeedsEveryControllerProof(t *testing.T) {
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{
			Management: v1alpha1.StorageClusterManagementManaged,
			Ceph: &v1alpha1.StorageClusterCephSpec{
				Cephadm: v1alpha1.StorageCephadmSpec{Bootstrap: v1alpha1.StorageCephadmBootstrap{Node: "node-a"}},
			},
		},
	}, {
		Metadata: v1alpha1.Metadata{Name: "external"},
		Spec: v1alpha1.StorageClusterSpec{
			Management: v1alpha1.StorageClusterManagementExternal,
			Ceph: &v1alpha1.StorageClusterCephSpec{
				Cephadm: v1alpha1.StorageCephadmSpec{Bootstrap: v1alpha1.StorageCephadmBootstrap{Node: "node-b"}},
			},
		},
	}}}
	seedHost := render.StorageSeedHostName(state.StorageClusters[0])
	externalSeedHost := render.StorageSeedHostName(state.StorageClusters[1])
	exactRecord := ownership.ResourceRecord{
		APIVersion: "bootwright.io/ownership/v1alpha1",
		Kind:       string(ownership.KindStorageCluster),
		Name:       "ceph",
		Owner:      ownership.Owner,
		Context:    "ctx",
		Host:       seedHost,
		Cluster:    "ceph",
		Attributes: map[string]string{"seedHost": seedHost},
	}
	externalRecord := ownership.ResourceRecord{
		APIVersion: "bootwright.io/ownership/v1alpha1",
		Kind:       string(ownership.KindStorageCluster),
		Name:       "external",
		Owner:      ownership.Owner,
		Context:    "ctx",
		Host:       externalSeedHost,
		Cluster:    "external",
		Attributes: map[string]string{"seedHost": externalSeedHost},
	}
	with := func(edit func(*ownership.ResourceRecord)) []ownership.ResourceRecord {
		record := exactRecord
		record.Attributes = map[string]string{"seedHost": exactRecord.Attributes["seedHost"]}
		edit(&record)
		return []ownership.ResourceRecord{record}
	}
	storageTasks := []workflow.ApplyTask{
		{Entry: workflow.TaskLedgerEntry{Kind: workflow.ApplyTaskKindStorageCluster, Cluster: "ceph"}},
		{Entry: workflow.TaskLedgerEntry{Kind: workflow.ApplyTaskKindStorageCluster, Cluster: "external"}},
	}
	cases := []struct {
		name    string
		mode    workflow.ApplyMode
		allow   bool
		records []ownership.ResourceRecord
		tasks   []workflow.ApplyTask
		wantArm bool
	}{
		{name: "explicit rebuild and data loss on selected exact owner record", mode: workflow.ApplyModeRebuild, allow: true, records: []ownership.ResourceRecord{exactRecord, externalRecord}, tasks: storageTasks, wantArm: true},
		{name: "missing data loss stays closed", mode: workflow.ApplyModeRebuild, records: []ownership.ResourceRecord{exactRecord}, tasks: storageTasks},
		{name: "reconcile stays closed", mode: workflow.ApplyModeReconcile, allow: true, records: []ownership.ResourceRecord{exactRecord}, tasks: storageTasks},
		{name: "missing controller owner record stays closed", mode: workflow.ApplyModeRebuild, allow: true, tasks: storageTasks},
		{name: "wrong api version stays closed", mode: workflow.ApplyModeRebuild, allow: true, records: with(func(record *ownership.ResourceRecord) { record.APIVersion = "" }), tasks: storageTasks},
		{name: "wrong owner stays closed", mode: workflow.ApplyModeRebuild, allow: true, records: with(func(record *ownership.ResourceRecord) { record.Owner = "other" }), tasks: storageTasks},
		{name: "reference role stays closed", mode: workflow.ApplyModeRebuild, allow: true, records: with(func(record *ownership.ResourceRecord) { record.Role = ownership.RoleReference }), tasks: storageTasks},
		{name: "wrong name stays closed", mode: workflow.ApplyModeRebuild, allow: true, records: with(func(record *ownership.ResourceRecord) { record.Name = "other" }), tasks: storageTasks},
		{name: "wrong context stays closed", mode: workflow.ApplyModeRebuild, allow: true, records: with(func(record *ownership.ResourceRecord) { record.Context = "other" }), tasks: storageTasks},
		{name: "wrong cluster stays closed", mode: workflow.ApplyModeRebuild, allow: true, records: with(func(record *ownership.ResourceRecord) { record.Cluster = "other" }), tasks: storageTasks},
		{name: "wrong host stays closed", mode: workflow.ApplyModeRebuild, allow: true, records: with(func(record *ownership.ResourceRecord) { record.Host = "other" }), tasks: storageTasks},
		{name: "wrong recorded seed stays closed", mode: workflow.ApplyModeRebuild, allow: true, records: with(func(record *ownership.ResourceRecord) { record.Attributes["seedHost"] = "other" }), tasks: storageTasks},
		{name: "scope without storage converge stays closed", mode: workflow.ApplyModeRebuild, allow: true, records: []ownership.ResourceRecord{exactRecord}, tasks: []workflow.ApplyTask{{Entry: workflow.TaskLedgerEntry{Kind: workflow.ApplyTaskKindStorageInfra, Cluster: "ceph"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := &converge.WorkflowPlan{State: state}
			var out bytes.Buffer
			armed := applyIncompleteBootstrapRebuildAuthorization(&out, "ctx", tc.mode, plan, tc.tasks, tc.records, tc.allow)
			if armed != tc.wantArm {
				t.Fatalf("armed=%v, want %v; vars=%v output=%q", armed, tc.wantArm, plan.ExtraVarPairs, out.String())
			}
			if !tc.wantArm {
				if len(plan.ExtraVarPairs) != 0 || out.Len() != 0 {
					t.Fatalf("closed case emitted authority: vars=%v output=%q", plan.ExtraVarPairs, out.String())
				}
				return
			}
			if len(plan.ExtraVarPairs) != 1 || plan.ExtraVarPairs[0] != "bootwright_ceph_incomplete_bootstrap_authorized_clusters=ceph" {
				t.Fatalf("authority must name only the selected owned managed cluster, got %v", plan.ExtraVarPairs)
			}
			for _, want := range []string{"interrupted first Ceph bootstrap", "ceph", "exact Bootwright owner record", "selected desired seed", "marker is absent", "unreachable", "rm-cluster --zap-osds", "healthy matching cluster", "untouched"} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("conditional authorization warning missing %q: %q", want, out.String())
				}
			}
		})
	}
}

func TestWarnForeignCephadmDaemonsStatesWhatSurvivesTheRemoval(t *testing.T) {
	var buf bytes.Buffer
	warnForeignCephadmDaemons(&buf, []string{"ceph-prd-01"})
	got := buf.String()
	for _, want := range []string{"ceph-prd-01", "foreign-daemons", "rm-cluster --force --fsid", "--zap-osds", "quorum", "re-probes"} {
		if !strings.Contains(got, want) {
			t.Errorf("the foreign-daemons warning must name %q: it destroys state of a cluster Bootwright does not own, so it must state the scope of the removal, that no disk is zapped, what a still-live cluster loses, and that the gate re-checks. got %q", want, got)
		}
	}
}

func reclaimPostDestroyFixture() (v1alpha1.State, []workflow.ObjectClassification) {
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{{
		Metadata: v1alpha1.Metadata{Name: "ceph-prd-01"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{{Name: "node-01", Devices: []string{"/dev/nvme0n1"}}}},
		}},
	}}}
	objects := []workflow.ObjectClassification{{Kind: workflow.ObjectKindStorageCluster, Label: "StorageCluster/ceph-prd-01"}}
	return state, objects
}

func TestResolveApplyReclaimDevicesActsWithoutOwnershipUnderTheToken(t *testing.T) {
	state, objects := reclaimPostDestroyFixture()

	auth, err := parseAuthorizations([]string{authorizeDataLoss, authorizeUnownedDevices}, authorizeVerbApply)
	if err != nil {
		t.Fatalf("parseAuthorizations: %v", err)
	}
	plan := &converge.WorkflowPlan{State: state}
	invocation := resolvedInvocation{verb: invocationApply, flags: invocationFlags{mode: workflow.ApplyModeReconcile, reclaimDevices: "all"}}
	var out bytes.Buffer
	resolved, clusters, rerr := resolveApplyReclaimDevices(&out, plan, auth, objects, "all", invocation)
	if rerr != nil {
		t.Fatalf("the token pair must let the reclaim act on a cluster a destroy released, got %v", rerr)
	}
	if resolved != "/dev/nvme0n1" {
		t.Fatalf("resolved = %q, want the declared device expansion of the selected unowned cluster", resolved)
	}
	if len(clusters) != 1 || clusters[0] != "ceph-prd-01" {
		t.Fatalf("clusters = %v, want the selected cluster so the role-side reclaim gate opens", clusters)
	}
	if vars := strings.Join(plan.ExtraVarPairs, "\n"); !strings.Contains(vars, converge.CephAuthorizeUnownedDevicesExtraVar+"=true") {
		t.Fatalf("the unowned-device authorization must reach the roles, got %v", plan.ExtraVarPairs)
	}

	bare, err := parseAuthorizations([]string{authorizeDataLoss}, authorizeVerbApply)
	if err != nil {
		t.Fatalf("parseAuthorizations: %v", err)
	}
	resolved, clusters, rerr = resolveApplyReclaimDevices(&bytes.Buffer{}, &converge.WorkflowPlan{State: state}, bare, objects, "all", invocation)
	if rerr != nil || resolved != "all" || len(clusters) != 0 {
		t.Fatalf("without the token an unowned cluster must stay out of the reclaim (resolved=%q clusters=%v err=%v)", resolved, clusters, rerr)
	}
}

func TestWarnApplyReclaimSelectionSteersThePostDestroyRecovery(t *testing.T) {
	_, objects := reclaimPostDestroyFixture()

	var out bytes.Buffer
	warnApplyReclaimSelection(&out, objects, "/dev/nvme0n1", nil)
	for _, want := range []string{"no device will be reclaimed", "--authorize " + authorizeDataLoss + "," + authorizeUnownedDevices, "bootwright destroy"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the no-op warning must name %q so the operator is not sent through the ownership re-recording dance, got %q", want, out.String())
		}
	}

	out.Reset()
	warnApplyReclaimSelection(&out, objects, "/dev/nvme0n1", []string{"ceph-prd-01"})
	for _, want := range []string{"WIPE", "ceph-prd-01", "hold no Bootwright ownership record", authorizeUnownedDevices} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the reclaim warning must name %q when it acts on an unrecorded cluster, got %q", want, out.String())
		}
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
		{"reinstall descriptor with yes refuses", []string{"reinstall ContainerCluster/dc1-ocp (live cluster with no install record; --mode rebuild reinstalls it and wipes its node disks)"}, true, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			invocation := resolvedInvocation{verb: invocationApply, flags: invocationFlags{mode: workflow.ApplyModeReconcile, selection: runSelection{clusters: "dc1-ocp"}, yes: tc.yes}}
			err := destructiveOverrideYesGuard(tc.destructive, tc.yes, tc.allow, invocation)
			if tc.wantErr != (err != nil) {
				t.Fatalf("guard(destructive=%v yes=%v allow=%v) err=%v, wantErr=%v", tc.destructive, tc.yes, tc.allow, err, tc.wantErr)
			}
			if err != nil {
				for _, obj := range tc.destructive {
					if !strings.Contains(err.Error(), obj) {
						t.Fatalf("refusal must name %q: %v", obj, err)
					}
				}
				if !strings.Contains(err.Error(), "--authorize data-loss") {
					t.Fatalf("refusal must point at --authorize data-loss: %v", err)
				}
				if !strings.Contains(err.Error(), "--clusters dc1-ocp") {
					t.Fatalf("refusal must reproduce the run's own selection so the named command is the one to run: %v", err)
				}
			}
		})
	}
}

func TestWarnDestructiveApplyDisclosesOnEveryAcceptedPath(t *testing.T) {
	destructive := []string{"reinstall ContainerCluster/dc1-ocp (installed record matches desired inputs but the cluster does not report Available=True; to keep its data, repair the cluster to Available=True and re-run plain apply — --mode rebuild reinstalls it and wipes its node disks)"}

	var out bytes.Buffer
	warnDestructiveApply(&out, destructive, runSelection{clusters: "dc1-ocp"})
	for _, want := range []string{"will DESTROY data", "reinstall ContainerCluster/dc1-ocp", "--clusters"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("data-loss warning must contain %q, got %q", want, out.String())
		}
	}

	out.Reset()
	warnDestructiveApply(&out, nil, runSelection{})
	if out.Len() != 0 {
		t.Fatalf("no destructive objects must print no warning, got %q", out.String())
	}

	if got := destructiveApplyConfirmPrompt(destructive, true); strings.Contains(got, "DESTRUCTIVE") {
		t.Fatalf("--authorize data-loss pre-accepts the escalated prompt, got %q", got)
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
	_ = emitApplyDataLossWarningsAndVars(&out, workflow.ApplyModeRebuild, objects, nil, plan, "", nil, nil, "", nil, false)
	vars := strings.Join(plan.ExtraVarPairs, "\n")
	if !strings.Contains(vars, "bootwright_ceph_subobject_rebuild_authorized=ceph-a/rbd") {
		t.Fatalf("record-classified drifted sub-object must be authorized for its acked rebuild, got %v", plan.ExtraVarPairs)
	}
	if strings.Contains(vars, "ceph-b/rbd") {
		t.Fatalf("an undrifted sub-object must NOT be authorized without --authorize data-loss, got %v", plan.ExtraVarPairs)
	}
	if !strings.Contains(out.String(), "StoragePool/ceph-a.rbd") {
		t.Fatalf("data-loss warning must name the drifted sub-object, got %q", out.String())
	}

	allowPlan := &converge.WorkflowPlan{}
	_ = emitApplyDataLossWarningsAndVars(&bytes.Buffer{}, workflow.ApplyModeRebuild, objects, nil, allowPlan, "", nil, nil, "", nil, true)
	allowVars := strings.Join(allowPlan.ExtraVarPairs, "\n")
	if !strings.Contains(allowVars, "ceph-a/rbd") || !strings.Contains(allowVars, "ceph-b/rbd") {
		t.Fatalf("--authorize data-loss must authorize every selected cluster's sub-objects so live-only drift has the documented path forward, got %v", allowPlan.ExtraVarPairs)
	}

	continuePlan := &converge.WorkflowPlan{}
	_ = emitApplyDataLossWarningsAndVars(&bytes.Buffer{}, workflow.ApplyModeReconcile, objects, nil, continuePlan, "", nil, nil, "", nil, true)
	if strings.Contains(strings.Join(continuePlan.ExtraVarPairs, "\n"), "bootwright_ceph_subobject_rebuild_authorized") {
		t.Fatalf("non-override apply must never authorize sub-object destroys, got %v", continuePlan.ExtraVarPairs)
	}
}

func TestEmitApplyDataLossWarningsNamesOCPReinstalls(t *testing.T) {
	reinstalls := []string{"reinstall ContainerCluster/dc1-ocp (recorded install inputs differ from current desired inputs — e.g. rotated secret material or changed install config; --mode rebuild reinstalls the cluster and wipes its node disks)"}

	var out bytes.Buffer
	plan := &converge.WorkflowPlan{}
	_ = emitApplyDataLossWarningsAndVars(&out, workflow.ApplyModeRebuild, nil, nil, plan, "", nil, nil, "", reinstalls, false)
	if !strings.Contains(out.String(), "reinstall ContainerCluster/dc1-ocp") || !strings.Contains(out.String(), "node disks wiped") {
		t.Fatalf("override reinstall warning must name the reinstalled cluster(s), got %q", out.String())
	}

	out.Reset()
	_ = emitApplyDataLossWarningsAndVars(&out, workflow.ApplyModeRebuild, nil, nil, &converge.WorkflowPlan{}, "", nil, nil, "", nil, false)
	if strings.Contains(out.String(), "reinstall ContainerCluster/") {
		t.Fatalf("no reinstall descriptors must print no reinstall warning, got %q", out.String())
	}
}
