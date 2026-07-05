package cli

import (
	"context"
	"io"
	"os"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons/oc"
	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/clusteraccess"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/storage/cephdiff"
	"github.com/crmarques/bootwright/internal/storage/cephstate"
	"github.com/crmarques/bootwright/internal/workspace"
)

// clusterVersionJSONPath extracts "<Available-status>|<desired-version>" from a
// ClusterVersion, the shallow container-cluster liveness signal the installer's
// own idempotency guard trusts.
const clusterVersionJSONPath = `jsonpath={.status.conditions[?(@.type=="Available")].status}|{.status.desired.version}`

// liveDiffReport is the result of `bootwright diff` in live mode: the real-state
// comparison for each storage and container cluster, laid over the offline
// report's structural skeleton (never-applied roots, orphans, infrastructure).
type liveDiffReport struct {
	InSync         bool                          `json:"inSync"`
	Storage        []liveStorageDiff             `json:"storage,omitempty"`
	Container      []liveContainerDiff           `json:"container,omitempty"`
	Infrastructure []liveInfraResource           `json:"infrastructure,omitempty"`
	Absent         []string                      `json:"absent,omitempty"`
	Undeclared     []workflow.UndeclaredResource `json:"undeclared,omitempty"`
	LoadWarnings   []string                      `json:"loadWarnings,omitempty"`
	Warnings       []string                      `json:"warnings,omitempty"`
}

// liveStorageDiff is one managed StorageCluster's live comparison. Note carries
// a reason (external, unreachable) when Report is empty.
type liveStorageDiff struct {
	Cluster string          `json:"cluster"`
	Probed  bool            `json:"probed"`
	InSync  bool            `json:"inSync"`
	Note    string          `json:"note,omitempty"`
	Report  cephdiff.Report `json:"report"`
}

// liveContainerDiff is one ContainerCluster's shallow live check.
type liveContainerDiff struct {
	Cluster   string `json:"cluster"`
	Installed bool   `json:"installed"`
	Reachable bool   `json:"reachable"`
	Available bool   `json:"available"`
	Version   string `json:"version,omitempty"`
	Note      string `json:"note,omitempty"`
}

// liveInfraResource carries the offline classification of a non-cluster
// resource, which has no live probe.
type liveInfraResource struct {
	Label          string                                `json:"label"`
	Classification workflow.ConvergeSafetyClassification `json:"classification"`
}

// buildLiveDiff overlays a real-state comparison on the offline report. It
// discovers live Ceph state once (if any managed storage cluster is in scope),
// diffs each storage cluster, shallow-checks each container cluster, and folds
// the offline classification for infrastructure and never-applied roots. A
// discovery or probe failure degrades to a warning, never a fatal error, so the
// default diff stays usable against a partially-built or unreachable environment.
func buildLiveDiff(ctx context.Context, cf *commonFlags, executable string, state v1alpha1.State, offline workflow.StateCheckReport, streamAnsible bool, stderr io.Writer) liveDiffReport {
	live := liveDiffReport{InSync: true}

	storageByName := map[string]v1alpha1.StorageCluster{}
	for _, cluster := range state.StorageClusters {
		storageByName[cluster.Metadata.Name] = cluster
	}

	needDiscovery := false
	for _, root := range offline.Roots {
		if root.Kind == workflow.ApplyClusterKindStorage && !root.Absent {
			if cluster, ok := storageByName[root.Name]; ok && v1alpha1.StorageClusterManaged(cluster) {
				needDiscovery = true
				break
			}
		}
	}

	discos := map[string]cephstate.Discovery{}
	if needDiscovery {
		got, warning := runCephDiscovery(ctx, cf, executable, state, streamAnsible, stderr)
		if warning != "" {
			live.Warnings = append(live.Warnings, warning)
		}
		discos = got
	}

	clustersDir := workspace.ControllerClustersDir(cf.ctx.Name)
	for _, root := range offline.Roots {
		switch root.Kind {
		case workflow.ApplyClusterKindStorage:
			if root.Absent {
				live.Absent = append(live.Absent, "StorageCluster/"+root.Name)
				live.InSync = false
				continue
			}
			live.Storage = append(live.Storage, diffStorageCluster(state, storageByName[root.Name], root.Name, discos, &live))
		case workflow.ApplyClusterKindContainer:
			if root.Absent {
				live.Absent = append(live.Absent, "ContainerCluster/"+root.Name)
				live.InSync = false
				continue
			}
			container := probeContainerCluster(ctx, state, clustersDir, cf.ctx.RunsDir, root.Name)
			// Only a cluster we could reach and that reports NOT Available is a
			// genuine live difference; not-installed/unreachable is a warning we
			// cannot turn into a drift verdict.
			if container.Reachable && !container.Available {
				live.InSync = false
			}
			live.Container = append(live.Container, container)
		default:
			if root.Absent {
				live.Absent = append(live.Absent, root.Kind+"/"+root.Name)
				live.InSync = false
				continue
			}
			for _, resource := range root.Resources {
				live.Infrastructure = append(live.Infrastructure, liveInfraResource{Label: resource.Label, Classification: resource.Classification})
				live.InSync = false
			}
		}
	}

	live.Undeclared = offline.Undeclared
	live.LoadWarnings = offline.LoadWarnings
	return live
}

// diffStorageCluster builds one storage cluster's live diff, flipping live.InSync
// when a probed cluster differs from desired state.
func diffStorageCluster(state v1alpha1.State, cluster v1alpha1.StorageCluster, name string, discos map[string]cephstate.Discovery, live *liveDiffReport) liveStorageDiff {
	result := liveStorageDiff{Cluster: name, InSync: true}
	if cluster.Metadata.Name == "" {
		result.Note = "not found in desired state"
		return result
	}
	if v1alpha1.StorageClusterExternal(cluster) {
		result.Note = "external (imported); not compared"
		return result
	}
	disc, ok := discos[name]
	if !ok || !disc.Probed {
		// Reached no answer (unreachable seed, or Ceph not bootstrapped): cannot
		// confirm drift, so do not fail the diff on it.
		result.Note = "cluster unreachable; could not compare live state"
		return result
	}
	report := cephdiff.Compare(state, cluster, disc)
	result.Probed = true
	result.Report = report
	result.InSync = report.InSync()
	if !result.InSync {
		live.InSync = false
	}
	return result
}

// runCephDiscovery prepares the bundle and runs the read-only discovery playbook,
// degrading a failure to a warning string rather than aborting the diff.
func runCephDiscovery(ctx context.Context, cf *commonFlags, executable string, state v1alpha1.State, streamAnsible bool, stderr io.Writer) (map[string]cephstate.Discovery, string) {
	clustersDir := workspace.ControllerClustersDir(cf.ctx.Name)
	reporter := newWorkflowReporter(stderr)
	bundle, err := prepareWorkflowBundle(false)
	if err != nil {
		return nil, "live Ceph discovery skipped: could not prepare the Ansible bundle: " + firstErrorLine(err)
	}
	discos, err := converge.RunCephStateDiscovery(ctx, stderr, stderr, cf.ctx, clustersDir, executable, bundle.Dir, state, streamAnsible, reporter)
	if err != nil {
		return nil, "live Ceph discovery incomplete: " + firstErrorLine(err)
	}
	return discos, ""
}

// probeContainerCluster runs the shallow ClusterVersion check against a container
// cluster's kubeconfig. Absence of a kubeconfig means it was never installed; an
// oc error means it is unreachable. Neither is treated as drift.
func probeContainerCluster(ctx context.Context, state v1alpha1.State, clustersDir, runsDir, name string) liveContainerDiff {
	data, err := clusteraccess.Kubeconfig(state, clustersDir, name)
	if err != nil {
		return liveContainerDiff{Cluster: name, Note: "not installed (no kubeconfig)"}
	}
	file, err := os.CreateTemp(runsDir, ".diff-kubeconfig-")
	if err != nil {
		return liveContainerDiff{Cluster: name, Installed: true, Note: "could not stage kubeconfig: " + err.Error()}
	}
	path := file.Name()
	defer os.Remove(path)
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return liveContainerDiff{Cluster: name, Installed: true, Note: "could not stage kubeconfig: " + err.Error()}
	}
	if err := file.Close(); err != nil {
		return liveContainerDiff{Cluster: name, Installed: true, Note: "could not stage kubeconfig: " + err.Error()}
	}
	runner := oc.CommandRunner{}
	out, err := runner.Run(ctx, path, []string{"--request-timeout=5s", "get", "clusterversion", "version", "-o", clusterVersionJSONPath}, nil)
	if err != nil {
		return liveContainerDiff{Cluster: name, Installed: true, Reachable: false, Note: "unreachable: " + firstErrorLine(err)}
	}
	container := liveContainerDiff{Cluster: name, Installed: true, Reachable: true}
	parts := strings.SplitN(strings.TrimSpace(string(out)), "|", 2)
	if len(parts) > 0 {
		container.Available = parts[0] == "True"
	}
	if len(parts) > 1 {
		container.Version = strings.TrimSpace(parts[1])
	}
	if !container.Available {
		container.Note = "ClusterVersion not Available"
	}
	return container
}

// printLiveDiff renders the live report as a git-style diff plus status lines.
func printLiveDiff(stdout io.Writer, live liveDiffReport) {
	p := cliout.New(stdout)
	p.Command("diff")
	p.Section("Desired vs real (live)")
	if live.InSync && len(live.Storage) == 0 && len(live.Container) == 0 && len(live.Absent) == 0 && len(live.Infrastructure) == 0 {
		p.Status(cliout.StatusOK, "scope", "no selected resources to check")
	} else if live.InSync {
		p.Status(cliout.StatusOK, "state", "desired state matches the live clusters")
	}

	for _, storage := range live.Storage {
		label := "StorageCluster/" + storage.Cluster
		switch {
		case !storage.Probed && storage.Note != "":
			p.Status(cliout.StatusWarn, label, storage.Note)
		case storage.InSync:
			p.Status(cliout.StatusOK, label, "in sync with the live cluster")
		default:
			renderStorageDiff(p, label, storage.Report)
		}
	}

	for _, container := range live.Container {
		label := "ContainerCluster/" + container.Cluster
		switch {
		case !container.Installed, !container.Reachable:
			p.Status(cliout.StatusWarn, label, container.Note)
		case container.Available:
			detail := "reachable, ClusterVersion Available"
			if container.Version != "" {
				detail += " (" + container.Version + ")"
			}
			p.Status(cliout.StatusOK, label, detail)
		default:
			p.Status(cliout.StatusFail, label, "reachable but "+container.Note)
		}
	}

	for _, absent := range live.Absent {
		p.Status(cliout.StatusWarn, absent, "absent (never applied)")
	}

	if len(live.Infrastructure) > 0 {
		p.Section("Infrastructure (from last recorded apply)")
		for _, resource := range live.Infrastructure {
			p.Status(stateCheckResourceStatus(resource.Classification), resource.Label, string(resource.Classification))
		}
	}

	printStateCheckOrphans(p, live.Undeclared)
	if len(live.Warnings) > 0 {
		p.Section("Warnings")
		for _, warning := range live.Warnings {
			p.Status(cliout.StatusWarn, "warning", warning)
		}
	}
	printStateCheckLoadWarnings(p, live.LoadWarnings)
}

// renderStorageDiff prints one storage cluster's facet diffs as a git-style
// unified diff: one object header, a hunk per differing object, and -/+ lines
// per field.
func renderStorageDiff(p *cliout.Printer, label string, report cephdiff.Report) {
	p.DiffObjectHeader(label, "differs from the live cluster")
	for _, facet := range report.Facets {
		for _, object := range facet.Objects {
			p.DiffHunk(facet.Name + " " + object.Key + " (" + string(object.State) + ")")
			var lines []cliout.DiffLine
			for _, field := range object.Fields {
				if field.HasDesired {
					lines = append(lines, cliout.DiffLine{Kind: cliout.DiffDel, Text: field.Name + ": " + field.Desired})
				}
				if field.HasReal {
					lines = append(lines, cliout.DiffLine{Kind: cliout.DiffAdd, Text: field.Name + ": " + field.Real})
				}
			}
			p.DiffLines(lines)
		}
	}
}

// firstErrorLine returns the first line of an error message, so a multi-line
// Ansible failure collapses to a single status/warning line.
func firstErrorLine(err error) string {
	if err == nil {
		return ""
	}
	line := strings.SplitN(err.Error(), "\n", 2)[0]
	return strings.TrimSpace(line)
}
