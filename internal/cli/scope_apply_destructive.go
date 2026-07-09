package cli

import (
	"fmt"
	"io"
	"strings"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func emitApplyDataLossWarningsAndVars(stdout io.Writer, mode workflow.ApplyMode, objects []workflow.ObjectClassification, tasks []workflow.ApplyTask, plan *converge.WorkflowPlan, reclaimDevices string) {
	if mode == workflow.ApplyModeOverride {
		if wiped := converge.OverrideDestructiveStorageClusters(objects); len(wiped) > 0 {
			cliout.NewContinuation(stdout).Warning("override", "wipes and rebuilds Ceph cluster(s) "+strings.Join(wiped, ", ")+": cephadm rm-cluster --zap-osds DESTROYS ALL OSD DATA on the cluster before re-bootstrapping. A change to cluster identity (seedHost/monIP/network) triggers this; an OSD-device add reconciles in place and does NOT wipe.")
		}
		if rebuilt := converge.OverrideDriftedStorageSubObjects(objects); len(rebuilt) > 0 {
			cliout.NewContinuation(stdout).Warning("override", "rebuilds drifted storage sub-objects: "+strings.Join(rebuilt, ", ")+". A structural change (pool type/erasure profile, or a CephFS metadata or default data pool) DESTROYS the data in that pool/filesystem; size, crush, and application changes reconcile in place.")
		}
		converge.ApplyReconcilableOnlyStorageExtraVar(plan, converge.ReconcilableOnlyStorageClusters(objects))
		converge.ApplyRebuildAuthorizedStorageExtraVar(plan, converge.RebuildAuthorizedStorageClusters(objects))
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
	if firstBoot := workflow.BareMetalFirstInstallClusters(objects, tasks); len(firstBoot) > 0 {
		cliout.NewContinuation(stdout).Warning("bare-metal boot", "first apply will boot the OS installer on the bare-metal host(s) of "+strings.Join(firstBoot, ", ")+" and coreos-installer will DISK-WIPE their target disks. Before booting, each BMC is checked for an already-running OS (Redfish occupancy guard); confirm the BMC addresses point at unused/authorized machines.")
		converge.ApplyOCPFirstInstallClustersExtraVar(plan, firstBoot)
	}
}

func destructiveApplyConfirmPrompt(stdout io.Writer, destructive []string, allowDestroy bool) string {
	if len(destructive) == 0 || allowDestroy {
		return "Continue with apply? [y/N] (default: no): "
	}
	cliout.NewContinuation(stdout).Warning("override", "will DESTROY and rebuild "+strings.Join(destructive, ", ")+" — disks wiped / Ceph OSD data zapped. This is irreversible.")
	return "Confirm this DESTRUCTIVE rebuild (accept data loss)? [y/N] (default: no): "
}

func destructiveOverrideYesGuard(destructive []string, yes, allowDestroy bool) error {
	if len(destructive) == 0 || allowDestroy || !yes {
		return nil
	}
	return fmt.Errorf("apply --override would destructively rebuild %s — disks are wiped and any Ceph OSD data is zapped. --yes does not authorize data loss: add --allow-destroy to proceed non-interactively, or drop --yes to confirm interactively", strings.Join(destructive, ", "))
}
