package cli

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const (
	diffAppliedRevision  = "rev-1b7e55a0"
	diffSelectedRevision = "rev-8c41d07e"
	diffApplyID          = "op-3f9a0c12"
)

func diffPool(name, size string) *api.Object {
	object := api.NewObject(api.CephPool, name, api.Value{}, api.MapValue(
		api.FieldValue{Name: "clusterRef", Value: api.StringValue("ceph-lab")},
		api.FieldValue{Name: "role", Value: api.StringValue("rbd")},
		api.FieldValue{Name: "replicated", Value: api.MapValue(
			api.FieldValue{Name: "size", Value: api.IntegerValue(size)},
			api.FieldValue{Name: "minSize", Value: api.IntegerValue("2")},
		)},
		api.FieldValue{Name: "autoscale", Value: api.MapValue(api.FieldValue{Name: "mode", Value: api.StringValue("on")})},
	))
	return &object
}

func diffClient(name string) *api.Object {
	object := api.NewObject(api.CephClient, name, api.Value{}, api.MapValue(
		api.FieldValue{Name: "clusterRef", Value: api.StringValue("ceph-lab")},
		api.FieldValue{Name: "poolRef", Value: api.StringValue("rbd-data")},
		api.FieldValue{Name: "access", Value: api.StringValue("readWrite")},
	))
	return &object
}

func diffApplied(object *api.Object, realization lifecycle.RealizationStatus, plan string, blocks ...string) lifecycle.ObjectDiff {
	return lifecycle.ObjectDiff{
		Kind: object.Kind(), Name: object.Name(), Plan: plan, Blocks: blocks, Realization: realization,
		Real: lifecycle.RealSide{Source: "applied", State: "present", Object: object},
	}
}

func diffRecorded() *lifecycle.DiffResult {
	changed := diffApplied(diffPool("rbd-data", "2"), lifecycle.RealizationDone, "changed", "ceph-pool-rbd-data")
	changed.Desired = diffPool("rbd-data", "3")
	added := lifecycle.ObjectDiff{
		Kind: api.CephPool, Name: "rbd-archive", Plan: "added", Blocks: []string{"ceph-pool-rbd-archive"},
		Desired: diffPool("rbd-archive", "3"),
		Real:    lifecycle.RealSide{Source: "applied", State: "absent", Reason: "no operation of this context realized it"},
	}
	removed := diffApplied(diffClient("rbd-canary"), lifecycle.RealizationDone, "removed", "ceph-client-rbd-canary")
	unchanged := diffApplied(diffClient("rbd-reader"), lifecycle.RealizationDone, "same", "ceph-client-rbd-reader")
	unchanged.Desired = diffClient("rbd-reader")
	return &lifecycle.DiffResult{
		Context:   lifecycle.ContextIdentity{Name: "lab", Revision: diffSelectedRevision, Mode: "ready"},
		Operation: lifecycle.DiffOperation{ID: diffApplyID, Verb: "apply", State: "done", Apply: diffApplyID, Revision: diffAppliedRevision, Applied: "readable"},
		Input:     "changed",
		Plan:      lifecycle.DiffPlan{State: "differs", Changed: 1, Added: 1, Removed: 1, Rebuilt: []string{}},
		Objects:   []lifecycle.ObjectDiff{added, changed, removed, unchanged},
	}
}

func diffUnknown() *lifecycle.DiffResult {
	result := diffRecorded()
	result.Input = "unchanged"
	result.Plan = lifecycle.DiffPlan{State: "same", Rebuilt: []string{}}
	result.Context.Revision = diffAppliedRevision
	unknown := lifecycle.ObjectDiff{
		Kind: api.CephClient, Name: "rbd-fenced", Plan: "same", Blocks: []string{"ceph-client-rbd-fenced"}, Realization: lifecycle.RealizationFailed,
		Desired: diffClient("rbd-fenced"),
		Real: lifecycle.RealSide{
			Source: "applied", State: "unknown",
			Reason: "its blocks read failed: ceph-client-rbd-fenced",
			Remedy: "review its durable state with bootwright status --context lab",
		},
	}
	result.Objects = []lifecycle.ObjectDiff{unknown, result.Objects[3]}
	return result
}

func diffNone() *lifecycle.DiffResult {
	result := diffUnknown()
	result.Objects = result.Objects[1:]
	result.Plan.Rebuilt = []string{}
	return result
}

func diffMixed() *lifecycle.DiffResult {
	result := diffRecorded()
	unknown := diffUnknown().Objects[0]
	result.Objects = []lifecycle.ObjectDiff{result.Objects[0], result.Objects[1], result.Objects[2], unknown, result.Objects[3]}
	return result
}

func diffProvider(diskGiB string) *api.Object {
	profile := api.MapValue(
		api.FieldValue{Name: "name", Value: api.StringValue("rhel")},
		api.FieldValue{Name: "diskGiB", Value: api.IntegerValue(diskGiB)},
		api.FieldValue{Name: "dataDisks", Value: api.ListValue()},
	)
	object := api.NewObject(api.InfraProvider, "lab-libvirt", api.Value{}, api.MapValue(
		api.FieldValue{Name: "libvirt", Value: api.MapValue(api.FieldValue{Name: "machineProfiles", Value: api.ListValue(profile)})},
	))
	return &object
}

func diffMachine(name string) *api.Object {
	object := api.NewObject(api.Machine, name, api.Value{}, api.MapValue(
		api.FieldValue{Name: "substrate", Value: api.MapValue(api.FieldValue{Name: "providerRef", Value: api.StringValue("lab-libvirt")})},
	))
	return &object
}

func diffObserved() *lifecycle.DiffResult {
	provider := lifecycle.ObjectDiff{
		Kind: api.InfraProvider, Name: "lab-libvirt", Plan: "same", Blocks: []string{"substrate-host-lab-libvirt"}, Realization: lifecycle.RealizationDone,
		Desired: diffProvider("80"),
		Real: lifecycle.RealSide{Source: "observed", State: "present", Object: diffProvider("100"), NotCompared: []string{
			"spec.libvirt.uri", "spec.libvirt.machineRef", "spec.libvirt.bmcEmulation", "spec.libvirt.networkAttachments",
			"spec.libvirt.machineProfiles[rhel].cpu", "spec.libvirt.machineProfiles[rhel].memoryMiB",
		}},
	}
	partial := lifecycle.ObjectDiff{
		Kind: api.Machine, Name: "rhel-01", Plan: "same", Blocks: []string{"machine-rhel-01"}, Realization: lifecycle.RealizationDone,
		Desired: diffMachine("rhel-01"),
		Real: lifecycle.RealSide{
			Source: "observed", State: "partial", Object: diffMachine("rhel-01"),
			NotCompared: []string{"spec.substrate.libvirt.profileRef", "spec.hardware", "spec.os", "spec.network", "spec.access"},
			Notes: []string{
				"machine-rhel-01: part of this effect is realized as its frozen request states",
				"machine-rhel-01: domain running; its controller reports power On",
			},
		},
	}
	unknown := lifecycle.ObjectDiff{
		Kind: api.Machine, Name: "rhel-02", Plan: "same", Blocks: []string{"machine-rhel-02"}, Realization: lifecycle.RealizationDone,
		Desired: diffMachine("rhel-02"),
		Real: lifecycle.RealSide{
			Source: "observed", State: "unknown",
			Reason: "machine-rhel-02: the hypervisor did not answer for the domain",
			Remedy: "restore what the reason names, then repeat bootwright diff --context lab --observe",
		},
	}
	applied := diffApplied(diffClient("rbd-reader"), lifecycle.RealizationDone, "same", "ceph-client-rbd-reader")
	applied.Desired = diffClient("rbd-reader")
	applied.Real.Notes = []string{"observed present; client-ceph-v1 renders no field, so the fields shown are what apply " + diffApplyID + " applied"}
	return &lifecycle.DiffResult{
		Context:   lifecycle.ContextIdentity{Name: "lab", Revision: diffAppliedRevision, Mode: "ready"},
		Operation: lifecycle.DiffOperation{ID: diffApplyID, Verb: "apply", State: "done", Apply: diffApplyID, Revision: diffAppliedRevision, Applied: "readable"},
		Input:     "unchanged",
		Plan:      lifecycle.DiffPlan{State: "same", Rebuilt: []string{}},
		Observed:  true,
		Blocks:    lifecycle.DiffBlocks{Observed: 3, Unknown: 1, Unprojected: 1},
		Objects:   []lifecycle.ObjectDiff{partial, unknown, provider, applied},
	}
}

func diffRebuilt() *lifecycle.DiffResult {
	result := diffNone()
	result.Plan = lifecycle.DiffPlan{State: "differs", Changed: 1, Rebuilt: []string{"ceph-pool-rbd-data"}}
	return result
}

func diffLostRecord() *lifecycle.DiffResult {
	result := diffUnknown()
	result.Operation.State = "failed"
	result.Objects[0].Real.Reason = "its blocks read failed: ceph-client-rbd-fenced; the block record is lost"
	return result
}

func diffPaused() *lifecycle.DiffResult {
	result := diffNone()
	result.Operation.State = "paused"
	return result
}

func diffDoneWithFailedBlock() *lifecycle.DiffResult {
	result := diffUnknown()
	result.Operation.State = "done"
	return result
}

func diffGoldens() []cliGolden {
	return []cliGolden{
		{golden: "cli-diff-build", args: "diff", record: func(r *dispatchRecord) { r.result.lifecycleDiff = diffRebuilt() }},
		{golden: "cli-diff-lost-record", args: "diff", record: func(r *dispatchRecord) { r.result.lifecycleDiff = diffLostRecord() }},
		{golden: "cli-diff-paused", args: "diff", record: func(r *dispatchRecord) { r.result.lifecycleDiff = diffPaused() }},
		{golden: "cli-diff-done-failed-block", args: "diff", record: func(r *dispatchRecord) { r.result.lifecycleDiff = diffDoneWithFailedBlock() }},
		{golden: "cli-diff", args: "diff", record: func(r *dispatchRecord) { r.result.lifecycleDiff = diffRecorded() }},
		{golden: "cli-diff-json", args: "diff --output json", record: func(r *dispatchRecord) { r.result.lifecycleDiff = diffMixed() }},
		{golden: "cli-diff-none", args: "diff", record: func(r *dispatchRecord) { r.result.lifecycleDiff = diffNone() }},
		{golden: "cli-diff-unknown", args: "diff", record: func(r *dispatchRecord) { r.result.lifecycleDiff = diffUnknown() }},
		{golden: "cli-diff-observe", args: "diff --observe", record: func(r *dispatchRecord) { r.result.lifecycleDiff = diffObserved() }},
		{golden: "cli-diff-observe-json", args: "diff --observe --output json", record: func(r *dispatchRecord) { r.result.lifecycleDiff = diffObserved() }},
	}
}
