package cli

import (
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

type applyDestructiveSet struct {
	mode            workflow.ApplyMode
	objects         []workflow.ObjectClassification
	state           v1alpha1.State
	planState       v1alpha1.State
	clustersDir     string
	reinstalls      []string
	releasedRecords []workflow.SubstrateReleaseRecord
	rebuiltHosts    []string
	reclaimDevices  string
	ownedReclaim    []string
	allowDestroy    bool
}

func applyDestructiveDescriptors(in applyDestructiveSet) []string {
	var out []string
	if in.mode == workflow.ApplyModeRebuild {
		out = append(out, workflow.OverrideDestructiveDriftedObjects(in.objects)...)
		out = append(out, in.reinstalls...)
	}
	out = append(out, releasedBareMetalReinstallDescriptors(in.planState, in.releasedRecords)...)
	out = append(out, converge.KubeVirtTenantDestroyDescriptors(in.state, in.clustersDir, in.rebuiltHosts)...)
	out = append(out, reclaimDestructiveDescriptors(in.reclaimDevices, in.ownedReclaim)...)
	if in.mode == workflow.ApplyModeRebuild && in.allowDestroy {
		out = append(out, filterReclaimDestructiveDescriptors(filterReclaimAuthorizedClusters(in.planState, in.objects))...)
	}
	return out
}

func applyRequiredAuthorizations(auth *authorizations, mode workflow.ApplyMode, state, planState v1alpha1.State, tasks []workflow.ApplyTask, runsDir, clustersDir string, reinstallDrift []string, reclaimDevices string) []requiredAuthorization {
	forecast := newAuthorizationForecast(auth)
	if objects, err := workflow.ClassifyApplyObjects(tasks, runsDir); err == nil {
		released, _ := workflow.ConsumableSubstrateReleases(runsDir, tasks)
		var substrateReset []string
		if mode == workflow.ApplyModeRebuild {
			_, substrateReset = workflow.OverrideDestructiveMachineSubstrate(objects)
		}
		substrateReset = workflow.UnionClusterNames(substrateReset, workflow.SubstrateReleaseClusterNames(released))
		descriptors := applyDestructiveDescriptors(applyDestructiveSet{
			mode:            mode,
			objects:         objects,
			state:           state,
			planState:       planState,
			clustersDir:     clustersDir,
			reinstalls:      forecastReinstallDescriptors(reinstallDrift),
			releasedRecords: released,
			rebuiltHosts:    workflow.UnionClusterNames(reinstallDrift, substrateReset),
			reclaimDevices:  reclaimDevices,
			ownedReclaim:    converge.OwnedStorageClusters(objects),
			allowDestroy:    auth.has(authorizeDataLoss),
		})
		forecast.consult(authorizeDataLoss, len(descriptors) > 0, "a real run would "+strings.Join(descriptors, ", ")+" — target disks wiped and any Ceph OSD data zapped")
		forecast.mayConsult(authorizeDataLoss, len(descriptors) == 0 && mode == workflow.ApplyModeRebuild && len(workflow.InstalledRecordedClusters(clustersDir, tasks)) > 0, "plan mode does not probe cluster availability; a real --mode rebuild additionally reinstalls (node disks wiped) any installed cluster that does not report Available=True")
	}
	forecast.mayConsult(authorizeUnownedDevices, reclaimDevices != "", "whether a named --reclaim-devices path carries LVM or dm-crypt holders without a Bootwright OSD ownership record is decided on the node, so a preview cannot settle it")
	forecast.mayConsult(authorizeForeignDaemons, len(workflow.StorageConvergeClusterNames(tasks)) > 0, "whether an enrolled storage node still runs cephadm units of an fsid this apply does not own is decided on the node, so a preview cannot settle it")
	return forecast.list()
}
