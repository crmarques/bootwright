package converge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/roles"
	"github.com/crmarques/bootwright/internal/storage/arbiter"
	"github.com/crmarques/bootwright/internal/workspace"
)

type ArbiterRunOptions struct {
	Plan               arbiter.Plan
	Address            string
	MonLocations       map[string][]string
	MonLocationsAfter  map[string][]string
	AllowSameSite      bool
	AllowDegraded      bool
	OldHostOffline     bool
	BecomePasswordFile string
	Verbose            bool
	ExtraVarPairs      []string
	RunLease           *workflow.CommandRunLease
}

type ArbiterRetirement struct {
	Host        string `json:"host"`
	Authorized  bool   `json:"authorized"`
	Corroborate bool   `json:"corroborated"`
	Offline     bool   `json:"offline"`
}

func ArbiterArtifactsRoot(runsDir string) string {
	return filepath.Join(runsDir, "preflight", "storage-replace-arbiter", "artifacts")
}

func ReadArbiterRetirement(runsDir string) (ArbiterRetirement, bool, error) {
	path := filepath.Join(ArbiterArtifactsRoot(runsDir), "storage-replace-arbiter", "arbiter-retire-result.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ArbiterRetirement{}, false, nil
	}
	if err != nil {
		return ArbiterRetirement{}, false, fmt.Errorf("read the arbiter retirement result: %w", err)
	}
	var out ArbiterRetirement
	if err := json.Unmarshal(data, &out); err != nil {
		return ArbiterRetirement{}, false, fmt.Errorf("decode the arbiter retirement result at %s: %w", path, err)
	}
	return out, true, nil
}

func RunArbiterReplacement(cmdCtx context.Context, stdout, stderr io.Writer, ctx workspace.Context, clustersDir, executable, bundleDir string, state v1alpha1.State, opts ArbiterRunOptions, reporter workflow.Reporter) error {
	during, err := json.Marshal(opts.MonLocations)
	if err != nil {
		return fmt.Errorf("encode arbiter mon locations: %w", err)
	}
	after, err := json.Marshal(opts.MonLocationsAfter)
	if err != nil {
		return fmt.Errorf("encode arbiter mon locations: %w", err)
	}
	hostsDuring, err := json.Marshal(opts.Plan.MonHostsDuring)
	if err != nil {
		return fmt.Errorf("encode arbiter mon placement: %w", err)
	}
	hostsAfter, err := json.Marshal(opts.Plan.MonHostsAfter)
	if err != nil {
		return fmt.Errorf("encode arbiter mon placement: %w", err)
	}
	runOpts := runOptionsForContext(ctx, clustersDir, executable, state)
	runOpts.BundleDir = bundleDir
	runOpts.Playbook = roles.PlaybookTaskStorageClusterReplaceArbiter
	runOpts.Limit = render.StorageClusterGroupName(opts.Plan.Cluster)
	runOpts.ArtifactsBaseName = "storage-replace-arbiter"
	runOpts.ArtifactsRoot = ArbiterArtifactsRoot(ctx.RunsDir)
	runOpts.OutputLogPath = workflow.PreflightLogPath(ctx.RunsDir, "storage-replace-arbiter")
	runOpts.Label = "replace arbiter " + opts.Plan.Cluster
	runOpts.AcquireRunLease = opts.RunLease == nil
	runOpts.RecordRunLedger = true
	runOpts.RunLease = opts.RunLease
	runOpts.BecomePasswordFile = opts.BecomePasswordFile
	runOpts.ExtraVarPairs = append([]string{
		"bootwright_arbiter_cluster_name=" + opts.Plan.Cluster,
		"bootwright_arbiter_failure_domain=" + opts.Plan.FailureDomain,
		"bootwright_arbiter_desired_node=" + opts.Plan.DesiredNode,
		"bootwright_arbiter_desired_mon=" + opts.Plan.DesiredMon,
		"bootwright_arbiter_desired_site=" + opts.Plan.DesiredSite,
		"bootwright_arbiter_desired_addr=" + opts.Address,
		"bootwright_arbiter_live_mon=" + opts.Plan.LiveMon,
		"bootwright_arbiter_live_node=" + opts.Plan.LiveNode,
		"bootwright_arbiter_tiebreaker_mon=" + opts.Plan.TiebreakerMon,
		"bootwright_arbiter_mon_hosts_during=" + string(hostsDuring),
		"bootwright_arbiter_mon_hosts_after=" + string(hostsAfter),
		"bootwright_arbiter_mon_locations=" + string(during),
		"bootwright_arbiter_mon_locations_after=" + string(after),
		"bootwright_arbiter_allow_same_site=" + strconv.FormatBool(opts.AllowSameSite),
		"bootwright_arbiter_allow_degraded=" + strconv.FormatBool(opts.AllowDegraded),
		"bootwright_arbiter_old_host_offline=" + strconv.FormatBool(opts.OldHostOffline),
	}, opts.ExtraVarPairs...)
	runOpts.ExtraVarPairs = append(runOpts.ExtraVarPairs, VerboseNoLogExtraVarPairs(opts.Verbose)...)
	runner := preflightRunner(stdout, stderr, false)
	if _, err := workflow.Run(cmdCtx, runOpts, runner, reporter); err != nil {
		return err
	}
	return nil
}
