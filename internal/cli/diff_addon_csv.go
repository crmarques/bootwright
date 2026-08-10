package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons/oc"
	extensionrecords "github.com/crmarques/bootwright/internal/addons/records"
	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/clusteraccess"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

const liveAddonCSVProbeParallelism = 8

type liveAddonCSVComparison struct {
	Cluster      string                           `json:"cluster"`
	Addon        string                           `json:"addon"`
	Namespace    string                           `json:"namespace"`
	Subscription string                           `json:"subscription"`
	Recorded     *extensionrecords.CSVObservation `json:"recorded,omitempty"`
	Live         *extensionrecords.CSVObservation `json:"live,omitempty"`
	State        string                           `json:"state"`
	Note         string                           `json:"note,omitempty"`
}

type addonCSVProbeGroup struct {
	cluster string
	addon   string
	reports []workflow.AddonCSVReport
}

func liveAddonCSVSelection(selected []workflow.AddonCSVReport, roots []workflow.StateCheckRoot) []workflow.AddonCSVReport {
	absentClusters := map[string]bool{}
	for _, root := range roots {
		if root.Kind == workflow.ApplyClusterKindContainer && root.Absent {
			absentClusters[root.Name] = true
		}
	}
	filtered := make([]workflow.AddonCSVReport, 0, len(selected))
	for _, report := range selected {
		if !absentClusters[report.Cluster] {
			filtered = append(filtered, report)
		}
	}
	return filtered
}

func probeLiveAddonCSVs(ctx context.Context, state v1alpha1.State, contextName, clustersDir, runsDir string, selected []workflow.AddonCSVReport) []liveAddonCSVComparison {
	groups := addonCSVProbeGroups(selected)
	results := make([][]liveAddonCSVComparison, len(groups))
	slots := make(chan struct{}, liveAddonCSVProbeParallelism)
	var wg sync.WaitGroup
	for index := range groups {
		wg.Add(1)
		slots <- struct{}{}
		go func(index int) {
			defer wg.Done()
			defer func() { <-slots }()
			results[index] = probeLiveAddonCSVGroup(ctx, state, contextName, clustersDir, runsDir, groups[index])
		}(index)
	}
	wg.Wait()
	var comparisons []liveAddonCSVComparison
	for _, group := range results {
		comparisons = append(comparisons, group...)
	}
	return comparisons
}

func addonCSVProbeGroups(selected []workflow.AddonCSVReport) []addonCSVProbeGroup {
	var groups []addonCSVProbeGroup
	for _, report := range selected {
		if len(groups) == 0 || groups[len(groups)-1].cluster != report.Cluster || groups[len(groups)-1].addon != report.Addon {
			groups = append(groups, addonCSVProbeGroup{cluster: report.Cluster, addon: report.Addon})
		}
		groups[len(groups)-1].reports = append(groups[len(groups)-1].reports, report)
	}
	return groups
}

func probeLiveAddonCSVGroup(ctx context.Context, state v1alpha1.State, contextName, clustersDir, runsDir string, group addonCSVProbeGroup) []liveAddonCSVComparison {
	comparisons := baseAddonCSVComparisons(group.reports)
	data, err := clusteraccess.Kubeconfig(state, contextName, clustersDir, group.cluster)
	if err != nil {
		markAddonCSVUnavailable(comparisons, "cluster access unavailable: "+firstErrorLine(err))
		return comparisons
	}
	file, err := os.CreateTemp(runsDir, ".diff-addon-kubeconfig-")
	if err != nil {
		markAddonCSVUnavailable(comparisons, "could not stage kubeconfig: "+firstErrorLine(err))
		return comparisons
	}
	path := file.Name()
	defer os.Remove(path)
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		markAddonCSVUnavailable(comparisons, "could not stage kubeconfig: "+firstErrorLine(err))
		return comparisons
	}
	if err := file.Close(); err != nil {
		markAddonCSVUnavailable(comparisons, "could not stage kubeconfig: "+firstErrorLine(err))
		return comparisons
	}
	extension := csvProbeExtension(group)
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	result, err := oc.ObserveReadiness(probeCtx, oc.CommandRunner{}, path, extension)
	cancel()
	if err != nil {
		markAddonCSVUnavailable(comparisons, "live CSV probe failed: "+firstErrorLine(err))
		return comparisons
	}
	matchLiveCSVObservations(comparisons, result.CSVObservations, result.Detail)
	return comparisons
}

func csvProbeExtension(group addonCSVProbeGroup) v1alpha1.ClusterAddon {
	checks := make([]v1alpha1.ClusterAddonReadinessCheck, 0, len(group.reports))
	for _, report := range group.reports {
		checks = append(checks, v1alpha1.ClusterAddonReadinessCheck{CSVSucceeded: &v1alpha1.ClusterAddonCSVReadiness{
			Namespace: report.Namespace, Subscription: report.Subscription,
		}})
	}
	return v1alpha1.ClusterAddon{
		Metadata: v1alpha1.Metadata{Name: group.addon},
		Spec:     v1alpha1.ClusterAddonSpec{Readiness: v1alpha1.ClusterAddonReadiness{Checks: checks}},
	}
}

func baseAddonCSVComparisons(reports []workflow.AddonCSVReport) []liveAddonCSVComparison {
	out := make([]liveAddonCSVComparison, 0, len(reports))
	for _, report := range reports {
		out = append(out, liveAddonCSVComparison{
			Cluster: report.Cluster, Addon: report.Addon, Namespace: report.Namespace,
			Subscription: report.Subscription, Recorded: report.Recorded,
		})
	}
	return out
}

func matchLiveCSVObservations(comparisons []liveAddonCSVComparison, observations []extensionrecords.CSVObservation, detail string) {
	used := make([]bool, len(observations))
	for index := range comparisons {
		comparison := &comparisons[index]
		for observationIndex := range observations {
			observation := observations[observationIndex]
			if used[observationIndex] || observation.Namespace != comparison.Namespace || observation.Subscription != comparison.Subscription {
				continue
			}
			used[observationIndex] = true
			comparison.Live = &observation
			break
		}
		switch {
		case comparison.Live == nil:
			comparison.State = "unavailable"
			comparison.Note = strings.TrimSpace(detail)
			if comparison.Note == "" {
				comparison.Note = "live CSV did not report Succeeded with a version"
			}
		case comparison.Recorded == nil:
			comparison.State = "unrecorded"
			comparison.Note = "no ready CSV observation recorded"
		case comparison.Recorded.InstalledCSV == comparison.Live.InstalledCSV && comparison.Recorded.Version == comparison.Live.Version:
			comparison.State = "match"
		default:
			comparison.State = "changed"
		}
	}
}

func markAddonCSVUnavailable(comparisons []liveAddonCSVComparison, note string) {
	for index := range comparisons {
		comparisons[index].State = "unavailable"
		comparisons[index].Note = note
	}
}

func printLiveAddonCSVs(p *cliout.Printer, comparisons []liveAddonCSVComparison) {
	if len(comparisons) == 0 {
		return
	}
	p.Section("Add-on CSV observations")
	for _, comparison := range comparisons {
		label := comparison.Cluster + "/" + comparison.Addon + " CSV " + comparison.Namespace + "/" + comparison.Subscription
		switch comparison.State {
		case "match":
			p.Status(cliout.StatusOK, label, formatLiveCSV(comparison.Live)+"; matches observation from "+comparison.Recorded.ObservedAt.Format(time.RFC3339))
		case "changed":
			p.Status(cliout.StatusWarn, label, "recorded "+formatRecordedCSV(comparison.Recorded)+"; live "+formatLiveCSV(comparison.Live))
		case "unrecorded":
			p.Status(cliout.StatusInfo, label, "live "+formatLiveCSV(comparison.Live)+"; "+comparison.Note)
		default:
			p.Status(cliout.StatusWarn, label, comparison.Note)
		}
	}
}

func formatRecordedCSV(observation *extensionrecords.CSVObservation) string {
	if observation == nil {
		return "none"
	}
	return fmt.Sprintf("%s version=%s observed=%s", observation.InstalledCSV, observation.Version, observation.ObservedAt.Format(time.RFC3339))
}

func formatLiveCSV(observation *extensionrecords.CSVObservation) string {
	if observation == nil {
		return "unavailable"
	}
	return observation.InstalledCSV + " version=" + observation.Version
}
