package workflow

import (
	"fmt"
	"time"

	"github.com/crmarques/bootwright/internal/converge/remedy"
)

func MarkClusterInstallTaskStarted(clustersDir, contextName, secretsDir, runID string, task ApplyTask, now time.Time) error {
	phase, ok := clusterInstallTaskStartPhase(task.Entry.Kind)
	if !ok || task.Entry.Cluster == "" {
		return nil
	}
	if !stateHasContainerCluster(task.State, task.Entry.Cluster) {
		return nil
	}
	hash, structuralHash, err := clusterInstallHashes(contextName, task.State, task.Entry.Cluster, secretsDir)
	if err != nil {
		return err
	}
	record, found, err := LoadClusterInstallRecord(clustersDir, task.Entry.Cluster)
	if err != nil {
		return err
	}
	if !found || record.Status == ClusterInstallStatusInstalled {
		record = ClusterInstallRecord{Cluster: task.Entry.Cluster, StartedAt: now.UTC()}
	}
	record.DesiredHash = hash
	record.StructuralHash = structuralHash
	record.HashSchema = ConvergeHashSchema
	record.Status = ClusterInstallStatusInstalling
	record.Phase = phase
	record.RunID = runID
	record.UpdatedAt = now.UTC()
	record.InstalledAt = nil
	if task.Entry.Kind == ApplyTaskKindClusterISO {
		record.InstallerVersion = ""
	}
	if err := SaveClusterInstallRecord(clustersDir, record); err != nil {
		return err
	}
	if task.Entry.Kind == ApplyTaskKindInstallWait {
		return SaveClusterConnectionRecord(clustersDir, clusterConnectionRecord(clustersDir, task.Entry.Cluster, task.State.Environments, now))
	}
	return nil
}

func MarkClusterInstallTaskSucceeded(clustersDir, runsDir, contextName, secretsDir, runID string, task ApplyTask, now time.Time) error {
	phase, ok := clusterInstallTaskSuccessPhase(task.Entry.Kind)
	if !ok || task.Entry.Cluster == "" {
		return nil
	}
	if !stateHasContainerCluster(task.State, task.Entry.Cluster) {
		return nil
	}
	record, found, err := LoadClusterInstallRecord(clustersDir, task.Entry.Cluster)
	if err != nil {
		return err
	}
	if !found {
		record = ClusterInstallRecord{Cluster: task.Entry.Cluster, StartedAt: now.UTC()}
	}
	hash, structuralHash, input, err := clusterInstallHashesAndInput(contextName, task.State, task.Entry.Cluster, secretsDir)
	if err != nil {
		return err
	}
	if task.Entry.Kind == ApplyTaskKindInstallWait {
		if err := saveSuccessfulInputSnapshot(runsDir, runID, clusterInstallSnapshotResourceID(task.Entry.Cluster), task.Entry.ID, task.Entry.Kind, TaskStatusOK, ConvergeHashSchema, input); err != nil {
			return err
		}
	}
	record.DesiredHash = hash
	record.StructuralHash = structuralHash
	record.HashSchema = ConvergeHashSchema
	record.RunID = runID
	record.Phase = phase
	record.UpdatedAt = now.UTC()
	if task.Entry.Kind == ApplyTaskKindClusterISO {
		installerVersion, err := LoadClusterInstallerVersion(clustersDir, task.Entry.Cluster)
		if err != nil {
			return err
		}
		record.InstallerVersion = installerVersion
	}
	if task.Entry.Kind == ApplyTaskKindInstallWait {
		record.Status = ClusterInstallStatusInstalled
		t := now.UTC()
		record.InstalledAt = &t
	} else {
		record.Status = ClusterInstallStatusInstalling
	}
	if err := SaveClusterInstallRecord(clustersDir, record); err != nil {
		return err
	}
	if task.Entry.Kind == ApplyTaskKindInstallWait {
		return SaveClusterConnectionRecord(clustersDir, clusterConnectionRecord(clustersDir, task.Entry.Cluster, task.State.Environments, now))
	}
	return nil
}

func MarkClusterInstallTaskFailed(clustersDir, contextName, secretsDir, runID string, task ApplyTask, now time.Time) error {
	phase, ok := clusterInstallTaskStartPhase(task.Entry.Kind)
	if !ok || task.Entry.Cluster == "" {
		return nil
	}
	if !stateHasContainerCluster(task.State, task.Entry.Cluster) {
		return nil
	}
	record, found, err := LoadClusterInstallRecord(clustersDir, task.Entry.Cluster)
	if err != nil {
		return err
	}
	if !found {
		record = ClusterInstallRecord{Cluster: task.Entry.Cluster, StartedAt: now.UTC()}
	}
	hash, structuralHash, err := clusterInstallHashes(contextName, task.State, task.Entry.Cluster, secretsDir)
	if err != nil {
		return err
	}
	record.DesiredHash = hash
	record.StructuralHash = structuralHash
	record.HashSchema = ConvergeHashSchema
	record.Status = ClusterInstallStatusFailed
	record.Phase = phase
	record.RunID = runID
	record.UpdatedAt = now.UTC()
	return SaveClusterInstallRecord(clustersDir, record)
}

func ClusterInstallPostSuccessError(clustersDir string, task ApplyTask) error {
	if task.Entry.Kind != ApplyTaskKindInstallWait || task.Entry.Cluster == "" || !stateHasContainerCluster(task.State, task.Entry.Cluster) {
		return nil
	}
	record, found, err := LoadClusterInstallRecord(clustersDir, task.Entry.Cluster)
	if err != nil {
		return err
	}
	if !found {
		return &ClusterInstallStateError{
			Cluster:    task.Entry.Cluster,
			Condition:  ClusterInstallConditionMissingPostSuccessRecord,
			RecordPath: ClusterInstallRecordPath(clustersDir, task.Entry.Cluster),
			Message:    fmt.Sprintf("ContainerCluster/%s completed its install wait but its install record is missing at %s; bootwright refuses to treat the cluster as safely installed or rerun its installer implicitly", task.Entry.Cluster, ClusterInstallRecordPath(clustersDir, task.Entry.Cluster)),
			Request:    clusterInstallRemedy(remedy.ActionRebuildCluster, task.Entry.Cluster),
		}
	}
	return clusterInstallVersionMismatch(record, clusterInstallDeclaredVersion(task.State, task.Entry.Cluster), true)
}

func clusterInstallTaskStartPhase(kind string) (ClusterInstallPhase, bool) {
	switch kind {
	case ApplyTaskKindClusterISO:
		return ClusterInstallPhaseCreatingISO, true
	case ApplyTaskKindNodeBoot:
		return ClusterInstallPhaseBooting, true
	case ApplyTaskKindBootstrapWait:
		return ClusterInstallPhaseWaitingBootstrap, true
	case ApplyTaskKindInstallWait:
		return ClusterInstallPhaseWaiting, true
	default:
		return "", false
	}
}

func clusterInstallTaskSuccessPhase(kind string) (ClusterInstallPhase, bool) {
	switch kind {
	case ApplyTaskKindClusterISO:
		return ClusterInstallPhaseISOCreated, true
	case ApplyTaskKindNodeBoot:
		return ClusterInstallPhaseNodesBooted, true
	case ApplyTaskKindBootstrapWait:
		return ClusterInstallPhaseBootstrapComplete, true
	case ApplyTaskKindInstallWait:
		return ClusterInstallPhaseComplete, true
	default:
		return "", false
	}
}

func clusterInstallPhaseMayHaveBooted(phase ClusterInstallPhase) bool {
	switch phase {
	case ClusterInstallPhaseBooting, ClusterInstallPhaseNodesBooted, ClusterInstallPhaseWaitingBootstrap, ClusterInstallPhaseBootstrapComplete, ClusterInstallPhaseWaiting, ClusterInstallPhaseComplete:
		return true
	default:
		return false
	}
}
