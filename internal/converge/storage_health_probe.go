package converge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/roles"
	"github.com/crmarques/bootwright/internal/workspace"
)

// storageHostsGroup is the Ansible inventory group of every managed storage node
// (mirrors render/inventory.GroupStorageHosts, inlined because the import matrix
// keeps internal/converge off internal/render).
const storageHostsGroup = "bootwright_storage_hosts"

// StorageOSDExpectation is what the caller (which owns the render layer) computed a
// cluster's OSD count to be: "exact" with Count OSDs, "atLeastOne", or "skip". The
// health overlay compares the live observation to it to decide degradation, so
// internal/converge needs no render/ceph dependency.
type StorageOSDExpectation struct {
	Mode  string
	Count int
}

// StorageHealthObservation is one managed Ceph cluster's live OSD health as read by
// the check_storage_health probe on the seed. Probed is false when the seed could
// not report (unreachable, or no ceph CLI/keyring yet); such a cluster keeps its
// offline classification rather than being flagged degraded on a failed probe.
type StorageHealthObservation struct {
	Cluster   string `json:"cluster"`
	Probed    bool   `json:"probed"`
	NumInOSDs int    `json:"numInOSDs"`
	NumUpOSDs int    `json:"numUpOSDs"`
	Health    string `json:"health"`
}

// RunStorageHealthProbe runs the read-only check_storage_health playbook against the
// managed Ceph seeds and returns each cluster's observed OSD health, keyed by
// cluster name. It writes no convergence records and mutates nothing — state-check
// --probe only adds this live read on top of its offline report. A cluster whose
// seed does not answer is simply absent from the returned map.
func RunStorageHealthProbe(cmdCtx context.Context, stdout, stderr io.Writer, ctx workspace.Context, clustersDir, executable, bundleDir string, state v1alpha1.State, streamAnsible bool, reporter workflow.Reporter) (map[string]StorageHealthObservation, error) {
	healthDir, err := os.MkdirTemp(ctx.RunsDir, "storage-health-")
	if err != nil {
		return nil, fmt.Errorf("create storage health result dir: %w", err)
	}
	defer os.RemoveAll(healthDir)
	runner := preflightRunner(stdout, stderr, streamAnsible)
	opts := runOptionsForContext(ctx, clustersDir, executable, state)
	opts.BundleDir = bundleDir
	opts.Playbook = roles.PlaybookCheckStorageHealth
	opts.Limit = storageHostsGroup
	opts.ArtifactsBaseName = "storage-health"
	opts.OutputLogPath = workflow.PreflightLogPath(ctx.RunsDir, "storage-health")
	opts.Label = "storage health probe"
	opts.ExtraVarPairs = []string{"bootwright_storage_health_dir=" + healthDir}
	if _, err := workflow.Run(cmdCtx, opts, runner, reporter); err != nil {
		return nil, err
	}
	return readStorageHealthObservations(healthDir)
}

func readStorageHealthObservations(dir string) (map[string]StorageHealthObservation, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]StorageHealthObservation{}, nil
		}
		return nil, err
	}
	out := map[string]StorageHealthObservation{}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var obs StorageHealthObservation
		if err := json.Unmarshal(data, &obs); err != nil {
			continue // skip a malformed result rather than brick the whole probe
		}
		if obs.Cluster != "" {
			out[obs.Cluster] = obs
		}
	}
	return out, nil
}

// storageHealthDegraded reports whether a live observation means the cluster came up
// short of its declared OSD expectation, and a human reason. HEALTH_ERR is always
// degraded; otherwise, an exact expectation fails when fewer OSDs are 'in' than
// declared, and a filter/all expectation fails only when zero OSDs are 'in'. A skip
// expectation (no managed OSD service) or a transient HEALTH_WARN is not degraded.
func storageHealthDegraded(mode string, expected int, o StorageHealthObservation) (bool, string) {
	if o.Health == "HEALTH_ERR" {
		return true, fmt.Sprintf("HEALTH_ERR (%d/%d OSDs up/in)", o.NumUpOSDs, o.NumInOSDs)
	}
	switch mode {
	case "exact":
		if o.NumInOSDs < expected {
			return true, fmt.Sprintf("%d of %d declared OSDs in (%s)", o.NumInOSDs, expected, o.Health)
		}
	case "atLeastOne":
		if o.NumInOSDs < 1 {
			return true, fmt.Sprintf("no OSDs in (%s)", o.Health)
		}
	}
	return false, ""
}

// OverlayStorageHealthProbe reclassifies each managed StorageCluster root as drift
// when the live probe shows it degraded (fewer OSDs 'in' than the declared
// expectation, or HEALTH_ERR). A cluster the probe could not reach keeps its offline
// classification. The finding is appended as a synthetic osd-health resource so the
// existing renderer and JSON output surface it, and report.InSync is cleared so
// state-check exits non-zero. Offline records are never rewritten.
func OverlayStorageHealthProbe(report *workflow.StateCheckReport, obs map[string]StorageHealthObservation, expectations map[string]StorageOSDExpectation) {
	for i := range report.Roots {
		root := &report.Roots[i]
		if root.Kind != "StorageCluster" || root.Absent {
			continue
		}
		o, ok := obs[root.Name]
		if !ok || !o.Probed {
			continue
		}
		exp := expectations[root.Name]
		degraded, reason := storageHealthDegraded(exp.Mode, exp.Count, o)
		if !degraded {
			continue
		}
		root.Total++
		root.Resources = append(root.Resources, workflow.StateCheckResource{
			ResourceID:     "StorageCluster/" + root.Name + "/osd-health",
			Kind:           "StorageOSDHealth",
			Label:          "osd-health: " + reason,
			Classification: workflow.ConvergeSafetyDrift,
		})
		report.InSync = false
	}
}
