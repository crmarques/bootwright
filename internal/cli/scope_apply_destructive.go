package cli

import (
	"context"
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

func overrideReinstallPlan(cmdCtx context.Context, clustersDir, contextName, secretsDir string, state v1alpha1.State, tasks []workflow.ApplyTask) (descriptors, acked []string, err error) {
	reinstalls, err := workflow.OverrideRebuildInstalledClusters(cmdCtx, clustersDir, contextName, secretsDir, state, tasks, nil)
	if err != nil {
		return nil, nil, err
	}
	return workflow.ClusterReinstallDescriptors(reinstalls), workflow.ClusterReinstallNames(reinstalls), nil
}

func filterReclaimDestructiveDescriptors(clusters []string) []string {
	if len(clusters) == 0 {
		return nil
	}
	return []string{"auto-reclaim dirty filter-OSD disks on Ceph cluster(s) " + strings.Join(clusters, ", ")}
}

func emitApplyDataLossWarningsAndVars(stdout io.Writer, mode workflow.ApplyMode, objects []workflow.ObjectClassification, tasks []workflow.ApplyTask, plan *converge.WorkflowPlan, reclaimDevices string, releasedRecords []workflow.SubstrateReleaseRecord, clustersDir string, ocpReinstalls []string, allowDestroy bool) bool {
	releasedClusters := workflow.SubstrateReleaseClusterNames(releasedRecords)
	consumedDataLoss := false
	var substrateReset []string
	if mode == workflow.ApplyModeRebuild {
		if len(ocpReinstalls) > 0 {
			cliout.NewContinuation(stdout).Warning("mode rebuild", "reinstalls ContainerCluster(s) — node disks wiped: "+strings.Join(ocpReinstalls, "; ")+". Each reinstalled node's managed-OS machine must be powered off before the run reaches its installer boot; a powered-on machine is refused there.")
		}
		if wiped := converge.OverrideDestructiveStorageClusters(objects); len(wiped) > 0 {
			cliout.NewContinuation(stdout).Warning("mode rebuild", "wipes and rebuilds Ceph cluster(s) "+strings.Join(wiped, ", ")+": cephadm rm-cluster --zap-osds DESTROYS ALL OSD DATA on the cluster before re-bootstrapping. A change to cluster identity (seedHost/monIP/network) triggers this; an OSD-device add reconciles in place and does NOT wipe.")
		}
		if rebuilt := converge.OverrideDriftedStorageSubObjects(objects); len(rebuilt) > 0 {
			cliout.NewContinuation(stdout).Warning("mode rebuild", "rebuilds drifted storage sub-objects: "+strings.Join(rebuilt, ", ")+". A structural change (pool type/erasure profile, or a CephFS metadata or default data pool) DESTROYS the data in that pool/filesystem; size, crush, and application changes reconcile in place.")
		}
		converge.ApplyReconcilableOnlyStorageExtraVar(plan, converge.ReconcilableOnlyStorageClusters(objects))
		converge.ApplyRebuildAuthorizedStorageExtraVar(plan, converge.RebuildAuthorizedStorageClusters(objects))
		subObjectKeys := converge.SubObjectRebuildAuthorizedKeys(objects)
		if allowDestroy {
			widened := workflow.UnionClusterNames(subObjectKeys, converge.AllStorageSubObjectRebuildKeys(objects))
			if len(widened) > len(subObjectKeys) {
				consumedDataLoss = true
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
				cliout.NewContinuation(stdout).Warning("authorize data-loss", "additionally authorizes destructive rebuild of storage sub-objects whose records match but whose LIVE structural identity may have drifted out of band: "+strings.Join(extra, ", ")+". Each is rebuilt (its data destroyed) only if its live pool/filesystem identity mismatches the declaration; an in-sync object is untouched.")
			}
			subObjectKeys = widened
		}
		converge.ApplySubObjectRebuildAuthorizedExtraVar(plan, subObjectKeys)
		if allowDestroy {
			if filterReclaim := filterReclaimAuthorizedClusters(plan.State, objects); len(filterReclaim) > 0 {
				consumedDataLoss = true
				cliout.NewContinuation(stdout).Warning("all-devices OSD reclaim", "authorizes automatic disk reclaim on Ceph cluster(s) "+strings.Join(filterReclaim, ", ")+": on a host whose OSDs are declared with data_devices.all=true, EVERY disk that is unavailable to ceph-volume, has NO mounted filesystem, and is NOT already an OSD of this cluster is WIPED (ceph orch device zap) before the OSD apply — IRREVERSIBLE, and NOT limited to disks that once held Ceph. Mounted/OS/system disks and this cluster's live OSDs are never touched. Do NOT use all=true on a host that also carries data to keep or runs a second Ceph cluster: an unmounted disk of a co-resident cluster is not distinguishable and would be wiped.")
				converge.ApplyFilterReclaimAuthorizedExtraVar(plan, filterReclaim)
			}
		}
		if _, reset := workflow.OverrideDestructiveMachineSubstrate(objects); len(reset) > 0 {
			cliout.NewContinuation(stdout).Warning("mode rebuild", "reinstalls managed-OS machine(s) of cluster(s) "+strings.Join(reset, ", ")+": their VMs are destroyed and re-created and their disks wiped. Only clusters whose machine set structurally drifted are reset; a matching machine is left running. A bare-metal machine of a reset cluster keeps running its OS until its reinstall boot, which is refused until the machine is first powered off.")
			substrateReset = reset
		}
	}
	if len(releasedRecords) > 0 {
		cliout.NewContinuation(stdout).Warning("destroyed substrate", "the machine substrate of "+describeReleasedSubstrates(releasedRecords)+" was destroyed by a previous `bootwright destroy`: this apply re-creates the released machines and reinstalls any managed OS, wiping the target disks. A released machine still running its OS is refused at that installer boot: power it off first.")
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
		cliout.NewContinuation(stdout).Warning("bare-metal boot", "first apply will boot the OS installer on the bare-metal host(s) of "+strings.Join(firstBoot, ", ")+" and coreos-installer will DISK-WIPE their target disks. Each machine must already be powered off: the installer boot is refused while a machine is powered on or its power state is unreadable (power-off gate). Confirm the BMC addresses point at the intended machines.")
	}
	if firstInstall := workflow.FirstInstallManagedOSBareMetalClusters(objects, tasks); len(firstInstall) > 0 {
		cliout.NewContinuation(stdout).Warning("managed-OS install", "first apply may boot the OS installer on bare-metal machine(s) of "+strings.Join(firstInstall, ", ")+" and the kickstart install DISK-WIPES their target disks. Each machine that needs installing must be powered off: the installer boot is refused while a machine is powered on or its power state is unreadable (power-off gate); a machine already running its bootwright-installed OS is left untouched.")
	}
	return consumedDataLoss
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

func applyJSONReinstallDrift(mode workflow.ApplyMode, clustersDir, contextName, secretsDir string, state v1alpha1.State, tasks []workflow.ApplyTask) []string {
	if mode != workflow.ApplyModeRebuild {
		return nil
	}
	return workflow.OverrideReinstallInputDriftedClusters(clustersDir, contextName, secretsDir, state, tasks)
}

func forecastReleasedReinstallDataLoss(stdout io.Writer, dryRun bool, runsDir string, state v1alpha1.State, tasks []workflow.ApplyTask) {
	if !dryRun {
		return
	}
	records, err := workflow.ConsumableSubstrateReleases(runsDir, tasks)
	if err != nil {
		return
	}
	forecast := releasedBareMetalReinstallDescriptors(state, records)
	if len(forecast) == 0 {
		return
	}
	cliout.NewContinuation(stdout).Warning("data loss", "a real run of this apply would "+strings.Join(forecast, "; ")+". A previous `bootwright destroy` released that substrate without wiping it, so the reinstall is where the still-running OS is lost, and each released machine must be powered off before its installer boot — a powered-on machine is refused there. Under --yes it needs --authorize "+authorizeDataLoss+"; interactively it stops at the destructive prompt")
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

func reclaimDestructiveDescriptors(reclaimDevices string, owned []string) []string {
	if reclaimDevices == "" || len(owned) == 0 {
		return nil
	}
	return []string{"reclaim-devices " + reclaimDevices + " on Ceph cluster(s) " + strings.Join(owned, ", ")}
}

func resolveApplyReclaimDevices(stdout io.Writer, plan *converge.WorkflowPlan, auth *authorizations, objects []workflow.ObjectClassification, reclaimDevices string) (string, []string, error) {
	owned := converge.OwnedStorageClusters(objects)
	if err := converge.CheckReclaimDestroyProtection(plan.State, owned, auth.has(authorizeDataLoss)); err != nil {
		return "", nil, failErr(1, err)
	}
	resolved, err := converge.ResolveReclaimDevices(plan.State, owned, reclaimDevices)
	if err != nil {
		return "", nil, failErr(2, err)
	}
	if len(owned) > 0 {
		if unmatched, declared := converge.UnmatchedReclaimDevices(plan.State, owned, resolved); len(unmatched) > 0 {
			return "", nil, failErr(2, reclaimUnmatchedError(unmatched, owned, declared))
		}
	}
	applyReclaimUnownedDevices(stdout, plan, auth, resolved)
	return resolved, owned, nil
}

func describeReclaimSelection(reclaimDevices string) string {
	if converge.ReclaimDevicesRequestsAll(reclaimDevices) {
		return "every declared OSD device"
	}
	return "device(s) " + reclaimDevices
}

func applyReclaimUnownedDevices(stdout io.Writer, plan *converge.WorkflowPlan, auth *authorizations, reclaimDevices string) {
	if !auth.allows(authorizeUnownedDevices) {
		return
	}
	warnReclaimUnownedDevices(stdout, reclaimDevices)
	converge.ApplyCephUnownedDeviceAuthorizationExtraVar(plan, true)
}

func applyForeignCephadmDaemons(stdout io.Writer, plan *converge.WorkflowPlan, auth *authorizations, tasks []workflow.ApplyTask) {
	clusters := workflow.StorageConvergeClusterNames(tasks)
	if len(clusters) == 0 {
		return
	}
	if !auth.allows(authorizeForeignDaemons) {
		return
	}
	warnForeignCephadmDaemons(stdout, clusters)
	converge.ApplyCephForeignDaemonAuthorizationExtraVar(plan, true)
}

func warnForeignCephadmDaemons(stdout io.Writer, clusters []string) {
	cliout.NewContinuation(stdout).Warning("authorize "+authorizeForeignDaemons, "a node of Ceph cluster(s) "+strings.Join(clusters, ", ")+" that still runs cephadm units of an fsid this apply does not own is reclaimed with `cephadm rm-cluster --force --fsid <fsid>` instead of refused. That is fsid-scoped: it stops and removes only that cluster's daemons, units and /var/lib/ceph state on that node, and leaves the applied cluster's own daemons alone. It passes no --zap-osds, so the other cluster's OSD disks keep their data — what is destroyed is its presence on this node, and if that cluster is still live elsewhere it loses this node's daemons, a monitor among them leaving its quorum. The gate re-probes the node afterwards and still refuses if any foreign unit survived.")
}

func warnReclaimUnownedDevices(stdout io.Writer, reclaimDevices string) {
	cliout.NewContinuation(stdout).Warning("authorize "+authorizeUnownedDevices, "additionally wipes named device(s) "+reclaimDevices+" that carry LVM or dm-crypt holders without a Bootwright OSD ownership record for their node. Without this token such a device fails closed as a possible live bluestore OSD. It is the right token for an orphan stack left by a Ceph cluster that was destroyed or was never Bootwright's; on a node whose Ceph daemons are still running it wipes a LIVE OSD and its data. A mounted, in-use, or unprobeable device still fails closed and no token relaxes that.")
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

func warnDestructiveApply(stdout io.Writer, destructive []string, selection runSelection) {
	if len(destructive) == 0 {
		return
	}
	cliout.NewContinuation(stdout).Warning("data loss", "will DESTROY data — "+strings.Join(destructive, ", ")+" — disks wiped / Ceph OSD data zapped. This is irreversible. If the list names an object you did not intend to rebuild, re-run with "+selection.narrowFlag()+" to narrow the destructive set.")
}

func destructiveApplyConfirmPrompt(destructive []string, allowDestroy bool) string {
	if len(destructive) == 0 || allowDestroy {
		return "Continue with apply? [y/N] (default: no): "
	}
	return "Confirm this DESTRUCTIVE action (accept data loss)? [y/N] (default: no): "
}

func destructiveOverrideYesGuard(destructive []string, yes, allowDestroy bool, selection runSelection) error {
	if len(destructive) == 0 || allowDestroy || !yes {
		return nil
	}
	return fmt.Errorf("apply would destroy data: %s — disks are wiped and any Ceph OSD data is zapped. --yes does not authorize data loss: re-run `%s` to proceed non-interactively, or drop --yes to confirm interactively; if the list names an object you did not intend to rebuild, re-run with %s to narrow the destructive set", strings.Join(destructive, ", "), selection.command("apply", "--authorize "+authorizeDataLoss, "--yes"), selection.narrowFlag())
}
