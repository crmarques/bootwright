package cli

import (
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/ownership"
)

type applyDestructiveSet struct {
	mode               workflow.ApplyMode
	objects            []workflow.ObjectClassification
	state              v1alpha1.State
	planState          v1alpha1.State
	clustersDir        string
	reinstalls         []string
	releasedRecords    []workflow.SubstrateReleaseRecord
	rebuiltHosts       []string
	reclaimDevices     string
	ownedReclaim       []string
	allowDestroy       bool
	provisionedStorage map[string]bool
}

func applyRunDestructiveDescriptors(mode workflow.ApplyMode, objects []workflow.ObjectClassification, state, planState v1alpha1.State, clustersDir string, reinstalls []string, released []workflow.SubstrateReleaseRecord, rebuiltHosts []string, reclaimDevices string, ownedReclaim []string, allowDestroy bool, provisionedStorage map[string]bool) []string {
	return applyDestructiveDescriptors(applyDestructiveSet{
		mode:               mode,
		objects:            objects,
		state:              state,
		planState:          planState,
		clustersDir:        clustersDir,
		reinstalls:         reinstalls,
		releasedRecords:    released,
		rebuiltHosts:       rebuiltHosts,
		reclaimDevices:     reclaimDevices,
		ownedReclaim:       ownedReclaim,
		allowDestroy:       allowDestroy,
		provisionedStorage: provisionedStorage,
	})
}

func applyDestructiveDescriptors(in applyDestructiveSet) []string {
	var out []string
	if in.mode == workflow.ApplyModeRebuild {
		out = append(out, workflow.OverrideDestructiveDriftedObjects(in.objects)...)
		out = append(out, in.reinstalls...)
	}
	out = append(out, releasedBareMetalReinstallDescriptors(in.planState, in.releasedRecords)...)
	out = append(out, converge.KubeVirtTenantDestroyDescriptors(in.state, in.clustersDir, in.rebuiltHosts, in.provisionedStorage)...)
	out = append(out, reclaimDestructiveDescriptors(in.reclaimDevices, in.ownedReclaim)...)
	if in.mode == workflow.ApplyModeRebuild && in.allowDestroy {
		out = append(out, filterReclaimDestructiveDescriptors(filterReclaimAuthorizedClusters(in.planState, in.objects))...)
	}
	return out
}

func applyRequiredAuthorizations(auth *authorizations, contextName string, mode workflow.ApplyMode, state, planState v1alpha1.State, tasks []workflow.ApplyTask, runsDir, clustersDir string, reinstallDrift []string, reclaimDevices string, provisionedStorage map[string]bool, ownershipRecords []ownership.ResourceRecord) []requiredAuthorization {
	forecast := newAuthorizationForecast(auth)
	reclaimUnownedRequired := false
	objects, classifyErr := workflow.ClassifyApplyObjects(tasks, runsDir)
	if classifyErr != nil {
		forecast.mayConsult(authorizeDataLoss, true, "this preview could not classify the run's objects against its run records ("+classifyErr.Error()+"), so it cannot settle what a real run destroys; the real run recomputes the same classification and fails closed on that error")
	} else {
		released, releaseErr := workflow.ConsumableSubstrateReleases(runsDir, tasks)
		if releaseErr != nil {
			forecast.mayConsult(authorizeDataLoss, true, "a destroyed cluster's rebuild authorization could not be read ("+releaseErr.Error()+"), so this preview cannot settle whether a real run reinstalls its machines and wipes their disks")
		}
		var substrateReset []string
		if mode == workflow.ApplyModeRebuild {
			_, substrateReset = workflow.OverrideDestructiveMachineSubstrate(objects)
		}
		substrateReset = workflow.UnionClusterNames(substrateReset, workflow.SubstrateReleaseClusterNames(released))
		ownedReclaim := reclaimEligibleClusters(planState, auth, objects)
		resolvedReclaim, resolveErr := converge.ResolveReclaimDevices(planState, ownedReclaim, reclaimDevices)
		if resolveErr != nil {
			resolvedReclaim = ""
		}
		reclaimUnownedRequired = reclaimDevices != "" && len(converge.OwnedStorageClusters(objects)) == 0 && len(converge.SelectedCephStorageClusters(planState, objects)) > 0
		forecast.consult(authorizeUnownedDevices, reclaimUnownedRequired, "no selected StorageCluster is recorded as Bootwright-owned — the state a successful destroy leaves — and without the token the reclaim acts on no device at all")
		descriptors := applyDestructiveDescriptors(applyDestructiveSet{
			mode:               mode,
			objects:            objects,
			state:              state,
			planState:          planState,
			clustersDir:        clustersDir,
			reinstalls:         forecastReinstallDescriptors(reinstallDrift),
			releasedRecords:    released,
			rebuiltHosts:       workflow.UnionClusterNames(reinstallDrift, substrateReset),
			reclaimDevices:     resolvedReclaim,
			ownedReclaim:       ownedReclaim,
			allowDestroy:       auth.has(authorizeDataLoss),
			provisionedStorage: provisionedStorage,
		})
		forecast.consult(authorizeDataLoss, len(descriptors) > 0, "a real run would "+strings.Join(descriptors, ", ")+" — target disks wiped and any Ceph OSD data zapped")
		forecast.mayConsult(authorizeDataLoss, len(descriptors) == 0 && mode == workflow.ApplyModeRebuild && len(workflow.InstalledRecordedClusters(clustersDir, tasks)) > 0, "plan mode does not probe cluster availability; a real --mode rebuild additionally reinstalls (node disks wiped) any installed cluster that does not report Available=True")
		forecast.mayConsult(authorizeDataLoss, mode == workflow.ApplyModeRebuild && len(incompleteBootstrapRebuildCandidates(contextName, planState, tasks, ownershipRecords)) > 0, "a selected managed StorageCluster with an exact Bootwright owner record for its desired seed may be an interrupted first bootstrap; only the host can prove the config-plus-record, missing-marker, unreachable-cluster shape whose cleanup runs cephadm rm-cluster --zap-osds")
	}
	forecast.mayConsult(authorizeUnownedDevices, reclaimDevices != "" && !reclaimUnownedRequired, "whether a named --reclaim-devices path carries LVM or dm-crypt holders without a Bootwright OSD ownership record is decided on the node, so a preview cannot settle it")
	forecast.mayConsult(authorizeForeignDaemons, len(workflow.StorageConvergeClusterNames(tasks)) > 0, "whether an enrolled storage node still runs cephadm units of an fsid this apply does not own is decided on the node, so a preview cannot settle it")
	return forecast.list()
}
