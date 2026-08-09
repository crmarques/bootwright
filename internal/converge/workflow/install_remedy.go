package workflow

import (
	"fmt"
	"strings"
	"time"

	"github.com/crmarques/bootwright/internal/converge/remedy"
)

type ClusterInstallCondition string

const (
	ClusterInstallConditionExistingRecord                  ClusterInstallCondition = "existing-record"
	ClusterInstallConditionReinstallNotAcknowledged        ClusterInstallCondition = "reinstall-not-acknowledged"
	ClusterInstallConditionAvailabilityChanged             ClusterInstallCondition = "availability-changed"
	ClusterInstallConditionPostBootInputDrift              ClusterInstallCondition = "post-boot-input-drift"
	ClusterInstallConditionAvailabilityProbeFailed         ClusterInstallCondition = "availability-probe-failed"
	ClusterInstallConditionUnavailable                     ClusterInstallCondition = "unavailable"
	ClusterInstallConditionLegacyInstallEvidenceUnreadable ClusterInstallCondition = "legacy-install-evidence-unreadable"
	ClusterInstallConditionLegacyConvergeEvidenceMismatch  ClusterInstallCondition = "legacy-converge-evidence-mismatch"
	ClusterInstallConditionMissingISORecord                ClusterInstallCondition = "missing-iso-record"
	ClusterInstallConditionISOInputDrift                   ClusterInstallCondition = "iso-input-drift"
	ClusterInstallConditionISOIncomplete                   ClusterInstallCondition = "iso-incomplete"
	ClusterInstallConditionNoInstallRecord                 ClusterInstallCondition = "no-install-record"
	ClusterInstallConditionUncertainBoot                   ClusterInstallCondition = "uncertain-boot"
	ClusterInstallConditionUnrecognizedPhase               ClusterInstallCondition = "unrecognized-phase"
	ClusterInstallConditionMissingPostSuccessRecord        ClusterInstallCondition = "missing-post-success-record"
)

type ClusterInstallStateError struct {
	Cluster    string
	Clusters   []string
	Condition  ClusterInstallCondition
	Status     ClusterInstallStatus
	Phase      ClusterInstallPhase
	RecordPath string
	Details    []string
	Message    string
	Cause      error
	Request    remedy.Request
}

func (e *ClusterInstallStateError) Error() string {
	if e.Cause == nil {
		return e.Message
	}
	return fmt.Sprintf("%s: %v", e.Message, e.Cause)
}

func (e *ClusterInstallStateError) Unwrap() error {
	return e.Cause
}

func (e *ClusterInstallStateError) Remedy() remedy.Request {
	return cloneRemedyRequest(e.Request)
}

type ClusterInstallResumeExpiredError struct {
	Cluster   string
	Phase     ClusterInstallPhase
	StartedAt time.Time
	Deadline  time.Time
}

func (e *ClusterInstallResumeExpiredError) Error() string {
	if e.StartedAt.IsZero() {
		return fmt.Sprintf("ContainerCluster/%s has incomplete post-boot install state at phase %q with no recorded start time; bootwright cannot prove that another automatic wait is still inside the %s resume ceiling and refuses before any mutation", e.Cluster, e.Phase, ClusterInstallResumeCeiling)
	}
	return fmt.Sprintf("ContainerCluster/%s has incomplete post-boot install state at phase %q from %s; its automatic resume ceiling ended at %s after %s, so bootwright refuses another wait before any mutation", e.Cluster, e.Phase, e.StartedAt.UTC().Format(time.RFC3339), e.Deadline.UTC().Format(time.RFC3339), ClusterInstallResumeCeiling)
}

func (e *ClusterInstallResumeExpiredError) Remedy() remedy.Request {
	return clusterInstallRemedy(remedy.ActionDestroyAndReapplyCluster, e.Cluster)
}

type ClusterInstallISOAgeError struct {
	Cluster     string
	PublishedAt time.Time
	ObservedAt  time.Time
	FreshWindow time.Duration
}

func (e *ClusterInstallISOAgeError) Error() string {
	switch {
	case e.PublishedAt.IsZero():
		return fmt.Sprintf("ContainerCluster/%s has a published agent ISO but its install record has no publish time; bootwright cannot prove that the ISO's embedded bootstrap certificates remain inside the %s freshness window and refuses before node boot", e.Cluster, e.FreshWindow)
	case e.ObservedAt.IsZero():
		return fmt.Sprintf("ContainerCluster/%s has a published agent ISO from %s but the current observation time is unavailable; bootwright cannot prove that the ISO's embedded bootstrap certificates remain inside the %s freshness window and refuses before node boot", e.Cluster, e.PublishedAt.UTC().Format(time.RFC3339), e.FreshWindow)
	case e.PublishedAt.After(e.ObservedAt):
		return fmt.Sprintf("ContainerCluster/%s has a published agent ISO time %s after the current observation time %s; bootwright cannot prove the ISO's age or bootstrap-certificate freshness and refuses before node boot", e.Cluster, e.PublishedAt.UTC().Format(time.RFC3339), e.ObservedAt.UTC().Format(time.RFC3339))
	default:
		return fmt.Sprintf("ContainerCluster/%s has a published agent ISO from %s that is %s old, outside the %s freshness window; bootwright refuses before node boot because stale embedded bootstrap certificates can fail after boot with misleading certificate or network symptoms", e.Cluster, e.PublishedAt.UTC().Format(time.RFC3339), e.ObservedAt.Sub(e.PublishedAt).Round(time.Second), e.FreshWindow)
	}
}

func (e *ClusterInstallISOAgeError) Remedy() remedy.Request {
	return clusterInstallRemedy(remedy.ActionRegenerateClusterISO, e.Cluster)
}

type ClusterInstallVersionError struct {
	Cluster            string
	Phase              ClusterInstallPhase
	InstallerVersion   string
	DeclaredVersion    string
	NodesMayHaveBooted bool
	InstallCompleted   bool
}

func (e *ClusterInstallVersionError) Error() string {
	recorded := strings.TrimSpace(e.InstallerVersion)
	if recorded == "" {
		recorded = "<missing>"
	}
	declared := strings.TrimSpace(e.DeclaredVersion)
	if declared == "" {
		declared = "<undeclared>"
	}
	switch {
	case e.InstallCompleted:
		return fmt.Sprintf("ContainerCluster/%s finished installing and its successful install evidence was retained, but the agent ISO installer version is %s while desired state declares %s; bootwright leaves this run nonzero so the release skew is not treated as normal and requires a deliberate future rebuild", e.Cluster, recorded, declared)
	case e.NodesMayHaveBooted:
		return fmt.Sprintf("ContainerCluster/%s is already past node boot at phase %q, but the agent ISO installer version is %s while desired state declares %s; bootwright will finish the already-started install before requiring a deliberate future rebuild", e.Cluster, e.Phase, recorded, declared)
	default:
		return fmt.Sprintf("ContainerCluster/%s has not booted any node, but its recorded agent ISO installer version is %s while desired state declares %s; bootwright refuses to boot and requires deliberate ISO regeneration", e.Cluster, recorded, declared)
	}
}

func (e *ClusterInstallVersionError) Remedy() remedy.Request {
	action := remedy.ActionRegenerateClusterISO
	if e.NodesMayHaveBooted || e.InstallCompleted {
		action = remedy.ActionRebuildCluster
	}
	return clusterInstallRemedy(action, e.Cluster)
}

func clusterInstallRemedy(action remedy.Action, clusters ...string) remedy.Request {
	targets := make([]remedy.Target, 0, len(clusters))
	for _, cluster := range clusters {
		if name := strings.TrimSpace(cluster); name != "" {
			targets = append(targets, remedy.Target{Role: remedy.TargetRoleContainerCluster, Name: name})
		}
	}
	return remedy.Request{Action: action, Targets: targets}
}

func cloneRemedyRequest(request remedy.Request) remedy.Request {
	request.Targets = append([]remedy.Target(nil), request.Targets...)
	return request
}
