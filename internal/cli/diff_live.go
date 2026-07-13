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
	"github.com/crmarques/bootwright/internal/storage/cephadopt"
	"github.com/crmarques/bootwright/internal/storage/cephdiff"
	"github.com/crmarques/bootwright/internal/storage/cephstate"
	"github.com/crmarques/bootwright/internal/workspace"
)

const clusterVersionJSONPath = `jsonpath={.status.conditions[?(@.type=="Available")].status}|{.status.desired.version}`

type liveDiffReport struct {
	InSync         bool                          `json:"inSync"`
	Storage        []liveStorageDiff             `json:"storage,omitempty"`
	Container      []liveContainerDiff           `json:"container,omitempty"`
	Infrastructure []liveInfraResource           `json:"infrastructure,omitempty"`
	Absent         []string                      `json:"absent,omitempty"`
	Undeclared     []workflow.UndeclaredResource `json:"undeclared,omitempty"`
	LoadWarnings   []string                      `json:"loadWarnings,omitempty"`
	Warnings       []string                      `json:"warnings,omitempty"`
	Adopt          *cephadopt.Summary            `json:"adopt,omitempty"`
}

type liveStorageDiff struct {
	Cluster string          `json:"cluster"`
	Probed  bool            `json:"probed"`
	InSync  bool            `json:"inSync"`
	Note    string          `json:"note,omitempty"`
	Report  cephdiff.Report `json:"report"`
}

type liveContainerDiff struct {
	Cluster   string `json:"cluster"`
	Installed bool   `json:"installed"`
	Reachable bool   `json:"reachable"`
	Available bool   `json:"available"`
	Version   string `json:"version,omitempty"`
	Note      string `json:"note,omitempty"`
}

type liveInfraResource struct {
	Label          string                                `json:"label"`
	Classification workflow.ConvergeSafetyClassification `json:"classification"`
}

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

func runCephDiscovery(ctx context.Context, cf *commonFlags, executable string, state v1alpha1.State, streamAnsible bool, stderr io.Writer) (map[string]cephstate.Discovery, string) {
	clustersDir := workspace.ControllerClustersDir(cf.ctx.Name)
	reporter := newWorkflowReporter(stderr)
	bundle, err := prepareWorkflowBundle(false)
	if err != nil {
		return nil, "live Ceph discovery skipped: could not prepare the Ansible bundle: " + firstErrorLine(err)
	}
	discos, err := converge.RunCephStateDiscovery(ctx, stderr, stderr, cf.ctx, clustersDir, executable, bundle.Dir, state, streamAnsible, reporter)
	if err != nil {
		return discos, "live Ceph discovery incomplete: " + firstErrorLine(err)
	}
	return discos, ""
}

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
		renderOSDAdvisories(p, storage.Report)
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
	printAdoptSummary(p, live.Adopt)
	if len(live.Warnings) > 0 {
		p.Section("Warnings")
		for _, warning := range live.Warnings {
			p.Status(cliout.StatusWarn, "warning", warning)
		}
	}
	printStateCheckLoadWarnings(p, live.LoadWarnings)
}

func printAdoptSummary(p *cliout.Printer, adopt *cephadopt.Summary) {
	if adopt == nil {
		return
	}
	p.Section("Adopted into desired state")
	if adopt.Empty() {
		p.Status(cliout.StatusOK, "adopt", "nothing to fold in; desired state already matches the adoptable facets")
	}
	for _, applied := range adopt.Applied {
		p.Status(cliout.StatusOK, "updated", applied)
	}
	for _, file := range adopt.NewFiles {
		p.Status(cliout.StatusOK, "created", file)
	}
	if adopt.Snapshot != "" {
		p.Status(cliout.StatusOK, "previous input", "saved to history: "+adopt.Snapshot)
	}
	for _, detected := range adopt.Detected {
		p.Status(cliout.StatusWarn, "not adopted", detected)
	}
}

func renderOSDAdvisories(p *cliout.Printer, report cephdiff.Report) {
	for _, adv := range report.UnpinnedOSDHosts {
		p.Status(cliout.StatusWarn, "unpinned OSDs", adv.Host+" on ["+strings.Join(adv.Devices, " ")+"] via a filter/all selection — pin osd.dataDevices.paths for exact reconstruction")
	}
}

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

func firstErrorLine(err error) string {
	if err == nil {
		return ""
	}
	line := strings.SplitN(err.Error(), "\n", 2)[0]
	return strings.TrimSpace(line)
}
