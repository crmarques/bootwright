package workflow

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/internal/converge/remedy"
)

func validateClusterInstallRecordState(clustersDir, cluster string, record ClusterInstallRecord) error {
	knownPhase := false
	switch record.Phase {
	case "", ClusterInstallPhaseCreatingISO, ClusterInstallPhaseISOCreated, ClusterInstallPhaseBooting, ClusterInstallPhaseNodesBooted, ClusterInstallPhaseWaitingBootstrap, ClusterInstallPhaseBootstrapComplete, ClusterInstallPhaseWaiting, ClusterInstallPhaseComplete:
		knownPhase = true
	}
	if !knownPhase {
		return &ClusterInstallStateError{
			Cluster:    cluster,
			Condition:  ClusterInstallConditionUnrecognizedPhase,
			Status:     record.Status,
			Phase:      record.Phase,
			RecordPath: ClusterInstallRecordPath(clustersDir, cluster),
			Message:    fmt.Sprintf("ContainerCluster/%s has unrecognized install phase %q in its install record at %s; bootwright cannot prove which install steps already changed the cluster and refuses before any mutation", cluster, record.Phase, ClusterInstallRecordPath(clustersDir, cluster)),
			Request:    clusterInstallRemedy(remedy.ActionRebuildCluster, cluster),
		}
	}
	valid := false
	switch record.Status {
	case ClusterInstallStatusInstalling, ClusterInstallStatusFailed:
		valid = record.Phase != ClusterInstallPhaseComplete
	case ClusterInstallStatusInstalled, ClusterInstallStatusDestroyed:
		valid = record.Phase == ClusterInstallPhaseComplete
	}
	if valid {
		return validateClusterInstallRecordEvidence(clustersDir, cluster, record)
	}
	return &ClusterInstallStateError{
		Cluster:    cluster,
		Condition:  ClusterInstallConditionInvalidRecordState,
		Status:     record.Status,
		Phase:      record.Phase,
		RecordPath: ClusterInstallRecordPath(clustersDir, cluster),
		Message:    fmt.Sprintf("ContainerCluster/%s has unsupported install record lifecycle state status %q and phase %q at %s; bootwright cannot prove which install steps already changed the cluster and refuses before any mutation", cluster, record.Status, record.Phase, ClusterInstallRecordPath(clustersDir, cluster)),
		Request:    clusterInstallRemedy(remedy.ActionRebuildCluster, cluster),
	}
}

func ValidateClusterInstallRecord(clustersDir, cluster string, record ClusterInstallRecord) error {
	return validateClusterInstallRecordState(clustersDir, cluster, record)
}

func validateClusterInstallRecordEvidence(clustersDir, cluster string, record ClusterInstallRecord) error {
	var details []string
	if record.Cluster != cluster {
		details = append(details, fmt.Sprintf("record cluster is %q, want %q", record.Cluster, cluster))
	}
	if record.Status == ClusterInstallStatusDestroyed {
		if len(details) == 0 {
			return nil
		}
		return invalidClusterInstallRecordEvidenceError(clustersDir, cluster, record, details)
	}
	validSchema := record.HashSchema == ConvergeHashSchema || record.Status == ClusterInstallStatusInstalled && record.HashSchema == ConvergeHashSchema-1
	if !validSchema {
		want := fmt.Sprintf("%d", ConvergeHashSchema)
		if record.Status == ClusterInstallStatusInstalled {
			want = fmt.Sprintf("%d or %d", ConvergeHashSchema-1, ConvergeHashSchema)
		}
		details = append(details, fmt.Sprintf("hashSchema is %d, want %s", record.HashSchema, want))
	}
	if !canonicalSHA256(record.DesiredHash) {
		details = append(details, "desiredHash is not sha256 followed by 64 lowercase hexadecimal characters")
	}
	if !canonicalSHA256(record.StructuralHash) {
		details = append(details, "structuralHash is not sha256 followed by 64 lowercase hexadecimal characters")
	}
	if strings.TrimSpace(record.RunID) == "" {
		details = append(details, "runId is empty")
	}
	postBoot := clusterInstallPhaseMayHaveBooted(record.Phase) && record.Phase != ClusterInstallPhaseComplete
	if record.StartedAt.IsZero() && !postBoot {
		details = append(details, "startedAt is missing")
	}
	if record.UpdatedAt.IsZero() && record.Phase != ClusterInstallPhaseISOCreated {
		details = append(details, "updatedAt is missing")
	}
	if !record.StartedAt.IsZero() && !record.UpdatedAt.IsZero() && !postBoot && record.UpdatedAt.Before(record.StartedAt) {
		details = append(details, "updatedAt precedes startedAt")
	}
	if record.Status == ClusterInstallStatusInstalled {
		switch {
		case record.InstalledAt == nil || record.InstalledAt.IsZero():
			details = append(details, "installedAt is missing")
		case !record.StartedAt.IsZero() && record.InstalledAt.Before(record.StartedAt):
			details = append(details, "installedAt precedes startedAt")
		case !record.UpdatedAt.IsZero() && record.InstalledAt.After(record.UpdatedAt):
			details = append(details, "installedAt follows updatedAt")
		}
	} else if record.InstalledAt != nil {
		details = append(details, fmt.Sprintf("installedAt is present for status %q", record.Status))
	}
	if len(details) == 0 {
		return nil
	}
	return invalidClusterInstallRecordEvidenceError(clustersDir, cluster, record, details)
}

func invalidClusterInstallRecordEvidenceError(clustersDir, cluster string, record ClusterInstallRecord, details []string) error {
	path := ClusterInstallRecordPath(clustersDir, cluster)
	return &ClusterInstallStateError{
		Cluster:    cluster,
		Condition:  ClusterInstallConditionInvalidRecordEvidence,
		Status:     record.Status,
		Phase:      record.Phase,
		RecordPath: path,
		Details:    append([]string(nil), details...),
		Message:    fmt.Sprintf("ContainerCluster/%s has invalid install record identity or writer evidence at %s: %s; bootwright cannot bind this record to the selected cluster and a complete install lifecycle, so it refuses before any mutation", cluster, path, strings.Join(details, "; ")),
		Request:    clusterInstallRemedy(remedy.ActionRebuildCluster, cluster),
	}
}

func canonicalSHA256(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, r := range value[len("sha256:"):] {
		if r < '0' || r > '9' {
			if r < 'a' || r > 'f' {
				return false
			}
		}
	}
	return true
}
