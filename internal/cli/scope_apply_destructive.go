package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func emitApplyDataLossWarningsAndVars(stdout io.Writer, mode workflow.ApplyMode, objects []workflow.ObjectClassification, tasks []workflow.ApplyTask, plan *converge.WorkflowPlan, reclaimDevices string, releasedClusters []string) {
	var substrateReset []string
	if mode == workflow.ApplyModeOverride {
		if wiped := converge.OverrideDestructiveStorageClusters(objects); len(wiped) > 0 {
			cliout.NewContinuation(stdout).Warning("override", "wipes and rebuilds Ceph cluster(s) "+strings.Join(wiped, ", ")+": cephadm rm-cluster --zap-osds DESTROYS ALL OSD DATA on the cluster before re-bootstrapping. A change to cluster identity (seedHost/monIP/network) triggers this; an OSD-device add reconciles in place and does NOT wipe.")
		}
		if rebuilt := converge.OverrideDriftedStorageSubObjects(objects); len(rebuilt) > 0 {
			cliout.NewContinuation(stdout).Warning("override", "rebuilds drifted storage sub-objects: "+strings.Join(rebuilt, ", ")+". A structural change (pool type/erasure profile, or a CephFS metadata or default data pool) DESTROYS the data in that pool/filesystem; size, crush, and application changes reconcile in place.")
		}
		converge.ApplyReconcilableOnlyStorageExtraVar(plan, converge.ReconcilableOnlyStorageClusters(objects))
		converge.ApplyRebuildAuthorizedStorageExtraVar(plan, converge.RebuildAuthorizedStorageClusters(objects))
		if _, reset := workflow.OverrideDestructiveMachineSubstrate(objects); len(reset) > 0 {
			cliout.NewContinuation(stdout).Warning("override", "reinstalls managed-OS machine(s) of cluster(s) "+strings.Join(reset, ", ")+": their VMs are destroyed and re-created and their disks wiped. Only clusters whose machine set structurally drifted are reset; a matching machine is left running.")
			substrateReset = reset
		}
	}
	if len(releasedClusters) > 0 {
		cliout.NewContinuation(stdout).Warning("destroyed substrate", "the machine substrate of cluster(s) "+strings.Join(releasedClusters, ", ")+" was destroyed by a previous `bootwright destroy`: this apply re-creates their machines and reinstalls any managed OS, wiping the target disks.")
		substrateReset = workflow.UnionClusterNames(substrateReset, releasedClusters)
	}
	if len(substrateReset) > 0 {
		converge.ApplySubstrateResetExtraVar(plan, substrateReset)
	}
	if reclaimDevices != "" {
		owned := converge.OwnedStorageClusters(objects)
		if len(owned) == 0 {
			cliout.NewContinuation(stdout).Warning("reclaim", "--reclaim-devices was given but no selected StorageCluster is recorded as Bootwright-owned; no device will be reclaimed (reclaim only wipes disks of an owned cluster).")
		} else {
			cliout.NewContinuation(stdout).Warning("reclaim", "will WIPE device(s) "+reclaimDevices+" on the owned Ceph cluster(s) "+strings.Join(owned, ", ")+" before apply — IRREVERSIBLE data loss. Only a named device that is a declared OSD device and is not mounted or a system disk is wiped.")
		}
		converge.ApplyReclaimDevicesExtraVars(plan, reclaimDevices, owned)
	}
	if firstBoot := workflow.BareMetalFirstInstallClusters(objects, tasks, plan.State); len(firstBoot) > 0 {
		cliout.NewContinuation(stdout).Warning("bare-metal boot", "first apply will boot the OS installer on the bare-metal host(s) of "+strings.Join(firstBoot, ", ")+" and coreos-installer will DISK-WIPE their target disks. Before booting, each BMC is checked for an already-running OS (Redfish occupancy guard); confirm the BMC addresses point at unused/authorized machines.")
	}
	converge.ApplyOCPReinstallClustersExtraVar(plan, workflow.UnionClusterNames(converge.RecordedContainerClusters(objects), releasedContainerClusters(plan.State, releasedClusters)))
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

func destructiveApplyConfirmPrompt(stdout io.Writer, destructive []string, allowDestroy bool) string {
	if len(destructive) == 0 || allowDestroy {
		return "Continue with apply? [y/N] (default: no): "
	}
	cliout.NewContinuation(stdout).Warning("data loss", "will DESTROY data — "+strings.Join(destructive, ", ")+" — disks wiped / Ceph OSD data zapped. This is irreversible.")
	return "Confirm this DESTRUCTIVE action (accept data loss)? [y/N] (default: no): "
}

func destructiveOverrideYesGuard(destructive []string, yes, allowDestroy bool) error {
	if len(destructive) == 0 || allowDestroy || !yes {
		return nil
	}
	return fmt.Errorf("apply would destroy data: %s — disks are wiped and any Ceph OSD data is zapped. --yes does not authorize data loss: add --allow-destroy to proceed non-interactively, or drop --yes to confirm interactively", strings.Join(destructive, ", "))
}
