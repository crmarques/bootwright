package converge

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/workspace"
)

const (
	InfraComponentApplySkipExtraVar = "bootwright_infra_component_apply_skip_records"
	InfraComponentReclaimExtraVar   = "bootwright_infra_component_reclaim_records"
)

type ArtifactServerReclaimTarget struct {
	RecordName  string
	RefClusters []string
}

func ArtifactServerRecordName(componentName string) string {
	return v1alpha1.KindInfraComponent + "-" + componentName
}

func ArtifactServerProvisionSkipRecords(targets []ArtifactServerReclaimTarget, clustersDir string, mode workflow.ApplyMode) ([]string, error) {
	if mode == workflow.ApplyModeRebuild {
		return nil, nil
	}
	var out []string
	for _, target := range targets {
		installed, err := AllReferencingClustersInstalled(clustersDir, target.RefClusters)
		if err != nil {
			return nil, err
		}
		if installed {
			out = append(out, target.RecordName)
		}
	}
	sort.Strings(out)
	return out, nil
}

func ApplyArtifactServerSkipExtraVar(plan *WorkflowPlan, names []string) {
	if len(names) == 0 {
		return
	}
	plan.ExtraVarPairs = append(plan.ExtraVarPairs, InfraComponentApplySkipExtraVar+"="+strings.Join(names, ","))
}

func ApplyInfraComponentReclaimExtraVar(plan *WorkflowPlan, names []string) {
	if len(names) == 0 {
		return
	}
	plan.ExtraVarPairs = append(plan.ExtraVarPairs, InfraComponentReclaimExtraVar+"="+strings.Join(names, ","))
}

func AllReferencingClustersInstalled(clustersDir string, clusters []string) (bool, error) {
	if len(clusters) == 0 {
		return false, nil
	}
	for _, cluster := range clusters {
		record, found, err := workflow.LoadClusterInstallRecord(clustersDir, cluster)
		if err != nil {
			return false, err
		}
		if !found || record.Status != workflow.ClusterInstallStatusInstalled {
			return false, nil
		}
	}
	return true, nil
}

func ownedInfraComponentRecords(records []ownership.ResourceRecord) map[string]bool {
	out := map[string]bool{}
	for _, record := range records {
		if strings.TrimSpace(record.Kind) != ownershipInfraComponentKind || record.IsReference() {
			continue
		}
		out[record.Name] = true
	}
	return out
}

func selectArtifactServerReclaims(targets []ArtifactServerReclaimTarget, owned, blocked map[string]bool, clustersDir string) ([]string, error) {
	var out []string
	for _, target := range targets {
		if len(target.RefClusters) == 0 || blocked[target.RecordName] || !owned[target.RecordName] {
			continue
		}
		installed, err := AllReferencingClustersInstalled(clustersDir, target.RefClusters)
		if err != nil {
			return nil, err
		}
		if installed {
			out = append(out, target.RecordName)
		}
	}
	sort.Strings(out)
	return out, nil
}

func ArtifactServerReclaimPreview(ownershipDir, contextName, clustersDir string, targets []ArtifactServerReclaimTarget) ([]string, error) {
	if len(targets) == 0 {
		return nil, nil
	}
	records, _, err := LoadContextOwnershipRecordsWithWarnings(ownershipDir, contextName)
	if err != nil {
		return nil, err
	}
	decision, err := PlanInfraComponentDestroyBlocks(contextName, nil, records, true)
	if err != nil {
		return nil, err
	}
	blocked := map[string]bool{}
	for _, block := range decision.Blocks {
		blocked[block.Name] = true
	}
	return selectArtifactServerReclaims(targets, ownedInfraComponentRecords(records), blocked, clustersDir)
}

func ReclaimInstallOnlyArtifactServers(cmdCtx context.Context, stdout, stderr io.Writer, ctx workspace.Context, clustersDir, executable, bundleDir, becomePasswordFile string, state v1alpha1.State, targets []ArtifactServerReclaimTarget, reporter workflow.Reporter, runLease *workflow.CommandRunLease) error {
	if len(targets) == 0 {
		return nil
	}
	records, _, err := LoadContextOwnershipRecordsWithWarnings(ctx.OwnershipDir, ctx.Name)
	if err != nil {
		return fmt.Errorf("load ownership records for artifact-server reclaim: %w", err)
	}
	decision, err := PlanInfraComponentDestroyBlocks(ctx.Name, nil, records, true)
	if err != nil {
		return fmt.Errorf("verify cross-context references before artifact-server reclaim: %w", err)
	}
	blocked := map[string]bool{}
	for _, block := range decision.Blocks {
		blocked[block.Name] = true
	}
	reclaim, err := selectArtifactServerReclaims(targets, ownedInfraComponentRecords(records), blocked, clustersDir)
	if err != nil {
		return err
	}
	if len(reclaim) == 0 {
		return nil
	}
	plan := PrepareInfraArtifactServerDestroyWorkflow(state, false, false, records)
	ApplyInfraComponentReclaimExtraVar(&plan, reclaim)
	if plan.NoRemoteWork {
		return nil
	}
	_, _, err = ExecuteDestroy(cmdCtx, stdout, stderr, ctx, clustersDir, executable, bundleDir, InfraDestroyArtifactServerPlaybook, plan, InfraDestroyArtifactServerArtifactsBaseName, false, becomePasswordFile, false, false, "infra reclaim artifact-server", reporter, runLease, false, nil)
	return err
}
