package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

func filterReclaimAuthorizedClusters(state v1alpha1.State, objects []workflow.ObjectClassification) []string {
	var out []string
	for _, o := range objects {
		if o.Kind != workflow.ObjectKindStorageCluster {
			continue
		}
		name := strings.TrimPrefix(o.Label, "StorageCluster/")
		for _, sc := range state.StorageClusters {
			if sc.Metadata.Name == name && sc.Spec.Ceph != nil && topology.ClusterHasAllDevicesOSDHost(sc) {
				out = append(out, name)
				break
			}
		}
	}
	return out
}

func filterReclaimDestructiveDescriptors(clusters []string) []string {
	if len(clusters) == 0 {
		return nil
	}
	return []string{"auto-reclaim dirty filter-OSD disks on Ceph cluster(s) " + strings.Join(clusters, ", ")}
}

func emitApplyDataLossWarningsAndVars(stdout io.Writer, mode workflow.ApplyMode, objects []workflow.ObjectClassification, tasks []workflow.ApplyTask, plan *converge.WorkflowPlan, reclaimDevices string, releasedRecords []workflow.SubstrateReleaseRecord, clustersDir string, ocpReinstalls []string, allowDestroy bool) {
	releasedClusters := workflow.SubstrateReleaseClusterNames(releasedRecords)
	var substrateReset []string
	if mode == workflow.ApplyModeOverride {
		if len(ocpReinstalls) > 0 {
			cliout.NewContinuation(stdout).Warning("converge-drifted", "reinstalls ContainerCluster(s) — node disks wiped: "+strings.Join(ocpReinstalls, "; "))
		}
		if wiped := converge.OverrideDestructiveStorageClusters(objects); len(wiped) > 0 {
			cliout.NewContinuation(stdout).Warning("converge-drifted", "wipes and rebuilds Ceph cluster(s) "+strings.Join(wiped, ", ")+": cephadm rm-cluster --zap-osds DESTROYS ALL OSD DATA on the cluster before re-bootstrapping. A change to cluster identity (seedHost/monIP/network) triggers this; an OSD-device add reconciles in place and does NOT wipe.")
		}
		if rebuilt := converge.OverrideDriftedStorageSubObjects(objects); len(rebuilt) > 0 {
			cliout.NewContinuation(stdout).Warning("converge-drifted", "rebuilds drifted storage sub-objects: "+strings.Join(rebuilt, ", ")+". A structural change (pool type/erasure profile, or a CephFS metadata or default data pool) DESTROYS the data in that pool/filesystem; size, crush, and application changes reconcile in place.")
		}
		converge.ApplyReconcilableOnlyStorageExtraVar(plan, converge.ReconcilableOnlyStorageClusters(objects))
		converge.ApplyRebuildAuthorizedStorageExtraVar(plan, converge.RebuildAuthorizedStorageClusters(objects))
		subObjectKeys := converge.SubObjectRebuildAuthorizedKeys(objects)
		if allowDestroy {
			widened := workflow.UnionClusterNames(subObjectKeys, converge.AllStorageSubObjectRebuildKeys(objects))
			if len(widened) > len(subObjectKeys) {
				named := make(map[string]bool, len(subObjectKeys))
				for _, key := range subObjectKeys {
					named[key] = true
				}
				var extra []string
				for _, key := range widened {
					if !named[key] {
						extra = append(extra, key)
					}
				}
				cliout.NewContinuation(stdout).Warning("confirm-data-loss", "additionally authorizes destructive rebuild of storage sub-objects whose records match but whose LIVE structural identity may have drifted out of band: "+strings.Join(extra, ", ")+". Each is rebuilt (its data destroyed) only if its live pool/filesystem identity mismatches the declaration; an in-sync object is untouched.")
			}
			subObjectKeys = widened
		}
		converge.ApplySubObjectRebuildAuthorizedExtraVar(plan, subObjectKeys)
		if allowDestroy {
			if filterReclaim := filterReclaimAuthorizedClusters(plan.State, objects); len(filterReclaim) > 0 {
				cliout.NewContinuation(stdout).Warning("all-devices OSD reclaim", "authorizes automatic disk reclaim on Ceph cluster(s) "+strings.Join(filterReclaim, ", ")+": on a host whose OSDs are declared with data_devices.all=true, EVERY disk that is unavailable to ceph-volume, has NO mounted filesystem, and is NOT already an OSD of this cluster is WIPED (ceph orch device zap) before the OSD apply — IRREVERSIBLE, and NOT limited to disks that once held Ceph. Mounted/OS/system disks and this cluster's live OSDs are never touched. Do NOT use all=true on a host that also carries data to keep or runs a second Ceph cluster: an unmounted disk of a co-resident cluster is not distinguishable and would be wiped.")
				converge.ApplyFilterReclaimAuthorizedExtraVar(plan, filterReclaim)
			}
		}
		if _, reset := workflow.OverrideDestructiveMachineSubstrate(objects); len(reset) > 0 {
			cliout.NewContinuation(stdout).Warning("converge-drifted", "reinstalls managed-OS machine(s) of cluster(s) "+strings.Join(reset, ", ")+": their VMs are destroyed and re-created and their disks wiped. Only clusters whose machine set structurally drifted are reset; a matching machine is left running.")
			substrateReset = reset
		}
	}
	if len(releasedRecords) > 0 {
		cliout.NewContinuation(stdout).Warning("destroyed substrate", "the machine substrate of "+describeReleasedSubstrates(releasedRecords)+" was destroyed by a previous `bootwright destroy`: this apply re-creates the released machines and reinstalls any managed OS, wiping the target disks.")
		substrateReset = workflow.UnionClusterNames(substrateReset, releasedClusters)
	}
	if len(substrateReset) > 0 {
		converge.ApplySubstrateResetExtraVar(plan, substrateReset)
	}
	if pairs := workflow.SubstrateReleaseMachinePairs(releasedRecords); len(pairs) > 0 {
		converge.ApplySubstrateResetMachinesExtraVar(plan, pairs)
	}
	if reclaimDevices != "" {
		owned := converge.OwnedStorageClusters(objects)
		if len(owned) == 0 {
			cliout.NewContinuation(stdout).Warning("reclaim", "--reclaim-devices was given but no selected StorageCluster is recorded as Bootwright-owned; no device will be reclaimed (reclaim only wipes disks of an owned cluster). Ownership is recorded by a successful apply from this context; if the context's runs/ records were lost, restore them, or first apply with the data-carrying device removed from the StorageCluster declaration (records ownership), then re-add it and re-run --reclaim-devices.")
		} else {
			cliout.NewContinuation(stdout).Warning("reclaim", "will WIPE device(s) "+reclaimDevices+" on the owned Ceph cluster(s) "+strings.Join(owned, ", ")+" before apply — IRREVERSIBLE data loss. Only a named device that is a declared OSD device, is not mounted or a system disk, and is on a host whose OSD marker does not already record it is wiped; a marker-recorded device is left in place.")
		}
		converge.ApplyReclaimDevicesExtraVars(plan, reclaimDevices, owned)
	}
	bootProven := workflow.BootProvenContainerClusters(clustersDir, tasks)
	if firstBoot := workflow.BareMetalFirstInstallClusters(bootProven, tasks, plan.State); len(firstBoot) > 0 {
		cliout.NewContinuation(stdout).Warning("bare-metal boot", "first apply will boot the OS installer on the bare-metal host(s) of "+strings.Join(firstBoot, ", ")+" and coreos-installer will DISK-WIPE their target disks. Before booting, each BMC is checked for an already-running OS (Redfish occupancy guard); confirm the BMC addresses point at unused/authorized machines.")
	}
	converge.ApplyOCPReinstallClustersExtraVar(plan, workflow.UnionClusterNames(bootProven, releasedContainerClusters(plan.State, releasedClusters)))
}

func describeReleasedSubstrates(records []workflow.SubstrateReleaseRecord) string {
	var parts []string
	for _, record := range records {
		if len(record.Machines) > 0 {
			parts = append(parts, record.Cluster+" (machine(s) "+strings.Join(record.Machines, ", ")+")")
			continue
		}
		parts = append(parts, record.Cluster)
	}
	return "cluster(s) " + strings.Join(parts, "; ")
}

func releasedBareMetalReinstallDescriptors(state v1alpha1.State, records []workflow.SubstrateReleaseRecord) []string {
	providers := make(map[string]string, len(state.InfraProviders))
	for _, provider := range state.InfraProviders {
		providers[provider.Metadata.Name] = provider.Spec.Type
	}
	machines := make(map[string]v1alpha1.Machine, len(state.Machines))
	for _, machine := range state.Machines {
		machines[machine.Metadata.Name] = machine
	}
	var out []string
	for _, record := range records {
		released := record.Machines
		if len(released) == 0 {
			released = workflow.ClusterSubstrateMachineNames(state, record.Cluster)
		}
		var bareMetal []string
		for _, name := range released {
			machine, ok := machines[name]
			if !ok || !v1alpha1.MachineInstallsOS(machine) {
				continue
			}
			if providers[machine.Spec.Substrate.ProviderRef.Name] == v1alpha1.ProvisionerBareMetal {
				bareMetal = append(bareMetal, name)
			}
		}
		if len(bareMetal) > 0 {
			out = append(out, "reinstall destroy-released bare-metal machine(s) "+strings.Join(bareMetal, ", ")+" of cluster "+record.Cluster+" (still-running OS wiped)")
		}
	}
	return out
}

func releasedContainerClusters(state v1alpha1.State, released []string) []string {
	declared := map[string]bool{}
	for _, cluster := range state.ContainerClusters {
		declared[cluster.Metadata.Name] = true
	}
	var out []string
	for _, name := range released {
		if declared[name] {
			out = append(out, name)
		}
	}
	return out
}

func reclaimDestructiveDescriptors(reclaimDevices string, owned []string) []string {
	if reclaimDevices == "" || len(owned) == 0 {
		return nil
	}
	return []string{"reclaim-devices " + reclaimDevices + " on Ceph cluster(s) " + strings.Join(owned, ", ")}
}

func reclaimUnmatchedError(unmatched, owned, declared []string) error {
	noun, verb := "entry", "does"
	if len(unmatched) > 1 {
		noun, verb = "entries", "do"
	}
	remedy := "the owned cluster(s) declare no OSD devices to match against"
	if len(declared) > 0 {
		remedy = "declare it in the StorageCluster or pass one of: " + strings.Join(declared, ", ")
	}
	return fmt.Errorf("--reclaim-devices %s %s %s not match any declared OSD device of owned Ceph cluster(s) %s — matching is by the exact declared path; %s", noun, strings.Join(unmatched, ", "), verb, strings.Join(owned, ", "), remedy)
}

func noteIneffectiveAllowDestroy(stdout io.Writer, allowDestroy, dryRun bool, destructive []string) {
	if !allowDestroy {
		return
	}
	if dryRun {
		cliout.NewContinuation(stdout).Warning("dry-run", "--confirm-data-loss is not consumed by a dry-run; the data-loss acknowledgement applies only to a real run")
		return
	}
	if len(destructive) == 0 {
		cliout.NewContinuation(stdout).Warning("confirm-data-loss", "--confirm-data-loss had no effect: this apply plans no data-destroying action (no destructive --converge-drifted rebuild, no owned-cluster device reclaim)")
	}
}

func warnDestructiveApply(stdout io.Writer, destructive []string) {
	if len(destructive) == 0 {
		return
	}
	cliout.NewContinuation(stdout).Warning("data loss", "will DESTROY data — "+strings.Join(destructive, ", ")+" — disks wiped / Ceph OSD data zapped. This is irreversible. If the list names an object you did not intend to rebuild, re-run with --clusters to narrow the destructive set.")
}

func destructiveApplyConfirmPrompt(destructive []string, allowDestroy bool) string {
	if len(destructive) == 0 || allowDestroy {
		return "Continue with apply? [y/N] (default: no): "
	}
	return "Confirm this DESTRUCTIVE action (accept data loss)? [y/N] (default: no): "
}

func destructiveOverrideYesGuard(destructive []string, yes, allowDestroy bool) error {
	if len(destructive) == 0 || allowDestroy || !yes {
		return nil
	}
	return fmt.Errorf("apply would destroy data: %s — disks are wiped and any Ceph OSD data is zapped. --yes does not authorize data loss: add --confirm-data-loss to proceed non-interactively, or drop --yes to confirm interactively; if the list names an object you did not intend to rebuild, re-run with --clusters to narrow the destructive set", strings.Join(destructive, ", "))
}
