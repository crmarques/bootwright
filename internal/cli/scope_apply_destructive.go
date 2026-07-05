package cli

import (
	"fmt"
	"io"
	"strings"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

// emitApplyDataLossWarningsAndVars prints the pre-confirm data-loss warnings for a
// mutating apply and threads the storage safety extra vars onto the plan: the
// --override Ceph wipe / storage sub-object rebuild warnings, the reconcilable-only
// and positive rebuild-authorization tokens that gate the seed role's zap, the
// --reclaim-devices in-band wipe warning + ownership allowlist, and the first
// bare-metal install disk-wipe warning. It is the single place the apply command
// surfaces "this will destroy data / re-image a host" so the confirm that follows is
// informed. objects is the classified scope; tasks the planned graph; plan receives
// the threaded extra vars.
func emitApplyDataLossWarningsAndVars(stdout io.Writer, mode workflow.ApplyMode, objects []workflow.ObjectClassification, tasks []workflow.ApplyTask, plan *converge.WorkflowPlan, reclaimDevices string) {
	// --override rebuilds drifted storage sub-objects; a structural change
	// (pool type/erasure profile, CephFS metadata or default data pool) is
	// data-destroying. Warn before the confirm prompt so the operator sees
	// which pools/filesystems are at risk.
	if mode == workflow.ApplyModeOverride {
		if wiped := converge.OverrideDestructiveStorageClusters(objects); len(wiped) > 0 {
			cliout.NewContinuation(stdout).Warning("override", "wipes and rebuilds Ceph cluster(s) "+strings.Join(wiped, ", ")+": cephadm rm-cluster --zap-osds DESTROYS ALL OSD DATA on the cluster before re-bootstrapping. A change to cluster identity (seedHost/monIP/network) triggers this; an OSD-device add reconciles in place and does NOT wipe.")
		}
		if rebuilt := converge.OverrideDriftedStorageSubObjects(objects); len(rebuilt) > 0 {
			cliout.NewContinuation(stdout).Warning("override", "rebuilds drifted storage sub-objects: "+strings.Join(rebuilt, ", ")+". A structural change (pool type/erasure profile, or a CephFS metadata or default data pool) DESTROYS the data in that pool/filesystem; size, crush, and application changes reconcile in place.")
		}
		// A StorageCluster whose only drift is a reconcilable OSD-device add is
		// reconciled in place by the seed role, not wiped: pass its name so the
		// override apply-mode gate suppresses rm-cluster --zap-osds for it.
		converge.ApplyReconcilableOnlyStorageExtraVar(plan, converge.ReconcilableOnlyStorageClusters(objects))
		// Positive rebuild-authorization token — the last line of defense. The
		// seed role's rm-cluster --zap-osds runs ONLY for a cluster named here,
		// which is exactly the structurally drifted set warned about above. A
		// healthy owned cluster (a no-drift match) is in neither, so --override
		// reconciles it idempotently in place instead of zapping it.
		converge.ApplyRebuildAuthorizedStorageExtraVar(plan, converge.RebuildAuthorizedStorageClusters(objects))
	}
	// --reclaim-devices wipes the named OSD disks in-band before the apply's
	// empty-device gate, to recover owned OSD disks whose on-node marker a
	// managed-OS reinstall erased. It is gated in Ansible on the device being a
	// declared OSD device of a controller-owned cluster and not mounted or a
	// system disk; here it emits the irreversible-data-loss warning and threads
	// the owned-cluster allowlist so a foreign/greenfield cluster's disks are
	// never reclaimed.
	if reclaimDevices != "" {
		owned := converge.OwnedStorageClusters(objects)
		if len(owned) == 0 {
			cliout.NewContinuation(stdout).Warning("reclaim", "--reclaim-devices was given but no selected StorageCluster is recorded as Bootwright-owned; no device will be reclaimed (reclaim only wipes disks of an owned cluster).")
		} else {
			cliout.NewContinuation(stdout).Warning("reclaim", "will WIPE device(s) "+reclaimDevices+" on the owned Ceph cluster(s) "+strings.Join(owned, ", ")+" before apply — IRREVERSIBLE data loss. Only a named device that is a declared OSD device and is not mounted or a system disk is wiped.")
		}
		converge.ApplyReclaimDevicesExtraVars(plan, reclaimDevices, owned)
	}
	// Irreversible-disk-wipe warning for a first bare-metal install. The OCP
	// agent-install path has no pre-boot occupancy probe (a deferred,
	// hardware-dependent driver), so a first apply onto a mis-pointed BMC — or
	// a re-apply after the controller records were lost — would silently
	// re-image a physical host that may be running production. Name the hosts
	// before the confirm so the operator can abort; an owned/recorded cluster
	// is excluded (its install-state healthy-skip already protects it).
	if firstBoot := workflow.BareMetalFirstInstallClusters(objects, tasks); len(firstBoot) > 0 {
		cliout.NewContinuation(stdout).Warning("bare-metal boot", "first apply will boot the OS installer on the bare-metal host(s) of "+strings.Join(firstBoot, ", ")+" and coreos-installer will DISK-WIPE their target disks. bootwright does not probe whether these hosts are already in use — confirm the BMC addresses point at unused/authorized machines before continuing.")
	}
}

// destructiveApplyConfirmPrompt returns the interactive confirmation prompt for an
// apply, upgrading it to an explicit data-loss prompt (and emitting the object list as
// a warning) when the run would destructively rebuild machines/clusters under --override
// without --allow-destroy. A non-destructive run keeps the routine prompt.
func destructiveApplyConfirmPrompt(stdout io.Writer, destructive []string, allowDestroy bool) string {
	if len(destructive) == 0 || allowDestroy {
		return "Continue with apply? [y/N] (default: no): "
	}
	cliout.NewContinuation(stdout).Warning("override", "will DESTROY and rebuild "+strings.Join(destructive, ", ")+" — disks wiped / Ceph OSD data zapped. This is irreversible.")
	return "Confirm this DESTRUCTIVE rebuild (accept data loss)? [y/N] (default: no): "
}

// destructiveOverrideYesGuard is the data-loss seatbelt for a non-interactive
// destructive --override. --yes skips the routine apply confirm but never authorizes
// data loss (mirroring how --yes never implies --override for destroy protection); only
// --allow-destroy authorizes it non-interactively. An empty destructive set,
// --allow-destroy, or an interactive run (!yes, which reaches the interactive
// data-loss confirm) returns nil. It is independent of destroyProtection: a protected
// environment already fails closed earlier, so this gates the unprotected case where a
// mis-scoped --override could otherwise wipe a machine or cluster with only --yes.
func destructiveOverrideYesGuard(destructive []string, yes, allowDestroy bool) error {
	if len(destructive) == 0 || allowDestroy || !yes {
		return nil
	}
	return fmt.Errorf("apply --override would destructively rebuild %s — disks are wiped and any Ceph OSD data is zapped. --yes does not authorize data loss: add --allow-destroy to proceed non-interactively, or drop --yes to confirm interactively", strings.Join(destructive, ", "))
}
