package cli

import (
	"fmt"
	"io"
	"strings"
	"time"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

type runTimingsLimits struct {
	Parallelism                 int  `json:"parallelism"`
	ParallelismUnbounded        bool `json:"parallelismUnbounded"`
	ParallelismPerHost          int  `json:"parallelismPerHost"`
	ParallelismPerHostUnbounded bool `json:"parallelismPerHostUnbounded"`
	ParallelismRedfish          int  `json:"parallelismRedfish"`
	ParallelismRedfishUnbounded bool `json:"parallelismRedfishUnbounded"`
}

type runTimingsTask struct {
	ID                 string   `json:"id"`
	Label              string   `json:"label"`
	Kind               string   `json:"kind"`
	Cluster            string   `json:"cluster,omitempty"`
	Status             string   `json:"status"`
	ReadyAt            string   `json:"readyAt,omitempty"`
	StartedAt          string   `json:"startedAt,omitempty"`
	EndedAt            string   `json:"endedAt,omitempty"`
	DurationSeconds    float64  `json:"durationSeconds"`
	QueueWaitSeconds   float64  `json:"queueWaitSeconds"`
	BlockedWaitSeconds float64  `json:"blockedWaitSeconds"`
	BlockedOn          []string `json:"blockedOn,omitempty"`
}

type runTimingsHop struct {
	ID                string  `json:"id"`
	Label             string  `json:"label"`
	Kind              string  `json:"kind"`
	Cluster           string  `json:"cluster,omitempty"`
	Status            string  `json:"status"`
	DurationSeconds   float64 `json:"durationSeconds"`
	CumulativeSeconds float64 `json:"cumulativeSeconds"`
}

type runTimingsPath struct {
	TotalSeconds float64         `json:"totalSeconds"`
	Hops         []runTimingsHop `json:"hops"`
}

type runTimingsReport struct {
	Run              string           `json:"run"`
	Target           string           `json:"target"`
	Scope            string           `json:"scope,omitempty"`
	Status           string           `json:"status"`
	StartedAt        string           `json:"startedAt,omitempty"`
	EndedAt          string           `json:"endedAt,omitempty"`
	WallClockSeconds float64          `json:"wallClockSeconds"`
	Limits           runTimingsLimits `json:"limits"`
	CriticalPath     runTimingsPath   `json:"criticalPath"`
	Tasks            []runTimingsTask `json:"tasks"`
}

func runStatusTimings(stdout io.Writer, cf *commonFlags, runID string, asJSON bool) error {
	ctx, err := cf.resolve()
	if err != nil {
		return failErr(1, err)
	}
	ledger, err := loadRunLedgerForTimings(ctx.RunsDir, runID)
	if err != nil {
		return failErr(1, err)
	}
	report := buildRunTimingsReport(ledger)
	if asJSON {
		if err := cliout.JSON(stdout, report); err != nil {
			return failErr(1, err)
		}
		return nil
	}
	printRunTimings(cliout.New(stdout), report)
	return nil
}

func loadRunLedgerForTimings(runsDir, runID string) (workflow.RunLedger, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		ledger, found, err := workflow.LoadRunLedger(runsDir)
		if err != nil {
			return workflow.RunLedger{}, err
		}
		if !found {
			return workflow.RunLedger{}, fmt.Errorf("no current run ledger under %s; pass --run <runID> to inspect a recorded run%s", runsDir, archivedRunHint(runsDir))
		}
		return ledger, nil
	}
	ledger, found, err := workflow.LoadArchivedRunLedger(runsDir, runID)
	if err != nil {
		return workflow.RunLedger{}, err
	}
	if found {
		return ledger, nil
	}
	if current, currentFound, currentErr := workflow.LoadRunLedger(runsDir); currentErr == nil && currentFound && current.RunID == runID {
		return current, nil
	}
	return workflow.RunLedger{}, fmt.Errorf("no run ledger for %q under %s%s", runID, runsDir, archivedRunHint(runsDir))
}

func archivedRunHint(runsDir string) string {
	ids := workflow.ArchivedRunIDs(runsDir)
	if len(ids) == 0 {
		return ""
	}
	if len(ids) > 5 {
		ids = ids[len(ids)-5:]
	}
	return "; recorded runs: " + strings.Join(ids, ", ")
}

func buildRunTimingsReport(ledger workflow.RunLedger) runTimingsReport {
	report := runTimingsReport{
		Run:              ledger.RunID,
		Target:           ledger.Target,
		Scope:            ledger.Scope,
		Status:           string(ledger.Status),
		StartedAt:        formatRunTime(&ledger.StartedAt),
		EndedAt:          formatRunTime(ledger.EndedAt),
		WallClockSeconds: durationSeconds(ledger.WallClock()),
		Limits: runTimingsLimits{
			Parallelism:                 ledger.Limits.Parallelism,
			ParallelismUnbounded:        ledger.Limits.ParallelismUnbounded(),
			ParallelismPerHost:          ledger.Limits.ParallelismPerHost,
			ParallelismPerHostUnbounded: ledger.Limits.ParallelismPerHostUnbounded(),
			ParallelismRedfish:          ledger.Limits.ParallelismRedfish,
			ParallelismRedfishUnbounded: ledger.Limits.ParallelismRedfishUnbounded(),
		},
		Tasks: []runTimingsTask{},
	}
	path := ledger.CriticalPath()
	report.CriticalPath = runTimingsPath{TotalSeconds: durationSeconds(path.Total), Hops: []runTimingsHop{}}
	for _, hop := range path.Hops {
		report.CriticalPath.Hops = append(report.CriticalPath.Hops, runTimingsHop{
			ID:                hop.Timing.Entry.ID,
			Label:             applyTaskDisplayLabel(hop.Timing.Entry.Label),
			Kind:              hop.Timing.Entry.Kind,
			Cluster:           hop.Timing.Entry.Cluster,
			Status:            string(hop.Timing.Entry.Status),
			DurationSeconds:   durationSeconds(hop.Timing.Duration),
			CumulativeSeconds: durationSeconds(hop.Cumulative),
		})
	}
	for _, timing := range ledger.TaskTimings() {
		report.Tasks = append(report.Tasks, runTimingsTask{
			ID:                 timing.Entry.ID,
			Label:              applyTaskDisplayLabel(timing.Entry.Label),
			Kind:               timing.Entry.Kind,
			Cluster:            timing.Entry.Cluster,
			Status:             string(timing.Entry.Status),
			ReadyAt:            formatRunTime(timing.Entry.ReadyAt),
			StartedAt:          formatRunTime(timing.Entry.StartedAt),
			EndedAt:            formatRunTime(timing.Entry.EndedAt),
			DurationSeconds:    durationSeconds(timing.Duration),
			QueueWaitSeconds:   durationSeconds(timing.QueueWait),
			BlockedWaitSeconds: durationSeconds(timing.BlockedWait),
			BlockedOn:          timing.Entry.BlockedOn,
		})
	}
	return report
}

func printRunTimings(p *cliout.Printer, report runTimingsReport) {
	p.Command("status --timings")
	p.Section("Run")
	fields := []cliout.Field{
		{Key: "Run", Value: report.Run},
		{Key: "Target", Value: report.Target},
		{Key: "Status", Value: report.Status},
	}
	if report.Scope != "" {
		fields = append(fields, cliout.Field{Key: "Scope", Value: report.Scope})
	}
	if report.StartedAt != "" {
		fields = append(fields, cliout.Field{Key: "Started", Value: report.StartedAt})
	}
	if report.EndedAt != "" {
		fields = append(fields, cliout.Field{Key: "Ended", Value: report.EndedAt})
	}
	fields = append(fields,
		cliout.Field{Key: "Wall clock", Value: formatRunSeconds(report.WallClockSeconds)},
		cliout.Field{Key: "Parallelism", Value: runTimingsLimitsSummary(report.Limits)},
	)
	p.Fields(fields)

	p.Section("Critical path")
	if len(report.CriticalPath.Hops) == 0 {
		p.Status(cliout.StatusSkip, "critical path", "no task recorded both a start and an end time")
	} else {
		lines := make([]cliout.TaskLine, 0, len(report.CriticalPath.Hops))
		for _, hop := range report.CriticalPath.Hops {
			lines = append(lines, cliout.TaskLine{
				Status: applyStepStatus(workflow.TaskStatus(hop.Status)),
				Label:  hop.Label,
				Detail: fmt.Sprintf("%s (cumulative %s)", formatRunSeconds(hop.DurationSeconds), formatRunSeconds(hop.CumulativeSeconds)),
			})
		}
		p.Tasks(lines)
		p.Fields([]cliout.Field{{Key: "Total", Value: criticalPathTotalDetail(report)}})
	}

	p.Section("Task timings")
	if len(report.Tasks) == 0 {
		p.Status(cliout.StatusSkip, "tasks", "the run ledger recorded no tasks")
		return
	}
	lines := make([]cliout.TaskLine, 0, len(report.Tasks))
	for _, task := range report.Tasks {
		lines = append(lines, cliout.TaskLine{
			Status: applyStepStatus(workflow.TaskStatus(task.Status)),
			Label:  task.Label,
			Detail: runTimingsTaskDetail(task),
		})
	}
	p.Tasks(lines)
}

func runTimingsTaskDetail(task runTimingsTask) string {
	detail := fmt.Sprintf("%s  queue %s  blocked %s",
		formatRunSeconds(task.DurationSeconds),
		formatRunSeconds(task.QueueWaitSeconds),
		formatRunSeconds(task.BlockedWaitSeconds))
	if len(task.BlockedOn) > 0 {
		detail += " on " + strings.Join(task.BlockedOn, ", ")
	}
	if task.Cluster != "" {
		detail += "  cluster " + task.Cluster
	}
	if task.Kind != "" {
		detail += "  kind " + task.Kind
	}
	return detail
}

func criticalPathTotalDetail(report runTimingsReport) string {
	total := formatRunSeconds(report.CriticalPath.TotalSeconds)
	if report.WallClockSeconds <= 0 {
		return total
	}
	share := report.CriticalPath.TotalSeconds / report.WallClockSeconds * 100
	return fmt.Sprintf("%s of %s wall clock (%.0f%%)", total, formatRunSeconds(report.WallClockSeconds), share)
}

func runTimingsLimitsSummary(limits runTimingsLimits) string {
	return fmt.Sprintf("tasks %s, per host %s, Redfish %s",
		applyLimitValue(limits.Parallelism, limits.ParallelismUnbounded),
		applyLimitValue(limits.ParallelismPerHost, limits.ParallelismPerHostUnbounded),
		applyLimitValue(limits.ParallelismRedfish, limits.ParallelismRedfishUnbounded))
}

func formatRunTime(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func durationSeconds(d time.Duration) float64 {
	if d <= 0 {
		return 0
	}
	return float64(d.Round(time.Millisecond)) / float64(time.Second)
}

func formatRunSeconds(seconds float64) string {
	d := time.Duration(seconds * float64(time.Second)).Round(time.Millisecond)
	if d <= 0 {
		return "0s"
	}
	if d < time.Second {
		return d.String()
	}
	return d.Round(time.Second).String()
}
