package arbiter

import (
	"fmt"
	"strings"
)

type LivePlanFailure string

const (
	LivePlanStateUnreadable     LivePlanFailure = "live-state-unreadable"
	LivePlanStretchModeDisabled LivePlanFailure = "stretch-mode-disabled"
	LivePlanTiebreakerMissing   LivePlanFailure = "tiebreaker-missing"
	LivePlanResidueAmbiguous    LivePlanFailure = "residue-ambiguous"
)

type LivePlanError struct {
	Failure    LivePlanFailure
	Cluster    string
	DesiredMon string
	StrayMons  []string
	Cause      error
}

func (e *LivePlanError) Error() string {
	if e == nil {
		return "arbiter live plan failed"
	}
	switch e.Failure {
	case LivePlanStateUnreadable:
		if e.Cause != nil {
			return fmt.Sprintf("read the live monmap of storage cluster %s: %v; an arbiter replacement cannot identify the live tiebreaker without that evidence", e.Cluster, e.Cause)
		}
		return fmt.Sprintf("storage cluster %s answered no readable live Ceph state; an arbiter replacement cannot identify the live tiebreaker without that evidence", e.Cluster)
	case LivePlanStretchModeDisabled:
		return fmt.Sprintf("storage cluster %s is not in stretch mode in its live monmap (ceph mon dump reports stretch_mode false), so it has no live tiebreaker to replace", e.Cluster)
	case LivePlanTiebreakerMissing:
		return fmt.Sprintf("storage cluster %s reports stretch mode but names no tiebreaker_mon in its live monmap; Bootwright will not guess which mon to retire, and the authored target is mon.%s", e.Cluster, e.DesiredMon)
	case LivePlanResidueAmbiguous:
		return fmt.Sprintf("storage cluster %s already answers with mon.%s as its stretch tiebreaker, but its monmap also carries undeclared mon(s) %s; Bootwright cannot identify which interrupted-replacement residue is safe to retire", e.Cluster, e.DesiredMon, strings.Join(e.StrayMons, ", "))
	default:
		return fmt.Sprintf("storage cluster %s arbiter live plan failed with unrecognized evidence %q", e.Cluster, e.Failure)
	}
}

func (e *LivePlanError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}
