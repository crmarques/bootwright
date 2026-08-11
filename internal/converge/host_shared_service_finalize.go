package converge

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/crmarques/bootwright/internal/converge/ansible"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/roles"
	"github.com/crmarques/bootwright/internal/workspace"
)

const hostSharedServiceFinalizeArtifactsBaseName = "host-shared-service-operation-finalize"

const hostSharedServiceFinalizeTimeout = 2 * time.Minute

func FinalizeHostSharedServiceOperations(cmdCtx context.Context, stdout, stderr io.Writer, ctx workspace.Context, clustersDir, executable, bundleDir, becomePasswordFile string, plan WorkflowPlan, manifest HostSharedServiceManifest, preRunOwnershipRecords []ownership.ResourceRecord, reporter workflow.Reporter, runLease *workflow.CommandRunLease, invocationArgs []string) error {
	if len(manifest) == 0 {
		return nil
	}
	if runLease == nil {
		return fmt.Errorf("host-wide shared-service operation finalization requires the caller-owned mutating run lease")
	}
	if err := runLease.RequireOwned(); err != nil {
		return fmt.Errorf("host-wide shared-service operation finalization requires the original mutating run lease: %w", err)
	}
	opts, err := hostSharedServiceFinalizeOptions(ctx, clustersDir, executable, bundleDir, becomePasswordFile, plan, manifest, preRunOwnershipRecords, runLease, invocationArgs)
	if err != nil {
		return err
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(cmdCtx), hostSharedServiceFinalizeTimeout)
	defer cancel()
	result, err := workflow.Run(cleanupCtx, opts, preflightRunner(stdout, stderr, false), reporter)
	if err != nil {
		return fmt.Errorf("release the exact host-wide shared-service operation after selected work became terminal: %w", err)
	}
	if result.Skipped {
		return fmt.Errorf("release the exact host-wide shared-service operation after selected work became terminal: none of the exact selected hosts %s were present in the pre-run inventory", strings.Join(manifest.Hosts(), ", "))
	}
	if err := runLease.RequireOwned(); err != nil {
		return fmt.Errorf("host-wide shared-service operation was finalized remotely but the controller mutating run lease was lost: %w", err)
	}
	return nil
}

func hostSharedServiceFinalizeOptions(ctx workspace.Context, clustersDir, executable, bundleDir, becomePasswordFile string, plan WorkflowPlan, manifest HostSharedServiceManifest, preRunOwnershipRecords []ownership.ResourceRecord, runLease *workflow.CommandRunLease, invocationArgs []string) (workflow.RunOptions, error) {
	expectedManifest, err := manifest.ExtraVarPair()
	if err != nil {
		return workflow.RunOptions{}, err
	}
	manifestCount := 0
	for _, pair := range plan.ExtraVarPairs {
		if pair == expectedManifest {
			manifestCount++
		}
	}
	if manifestCount != 1 {
		return workflow.RunOptions{}, fmt.Errorf("host-wide shared-service operation finalization requires exactly one unchanged selected-host manifest, found %d", manifestCount)
	}
	hosts := manifest.Hosts()
	if len(hosts) == 0 {
		return workflow.RunOptions{}, fmt.Errorf("host-wide shared-service operation finalization has no exact selected hosts")
	}
	opts := runOptionsForContext(ctx, clustersDir, executable, plan.State)
	opts.BundleDir = bundleDir
	opts.Playbook = roles.PlaybookTaskHostSharedServiceFinalize
	opts.Limit = strings.Join(hosts, ":")
	opts.Forks = workflow.AnsibleForksForLimit(plan.State, opts.Limit)
	opts.ExtraVarPairs = append([]string(nil), plan.ExtraVarPairs...)
	opts.ArtifactsBaseName = hostSharedServiceFinalizeArtifactsBaseName
	opts.OutputLogPath = workflow.PreflightLogPath(ctx.RunsDir, hostSharedServiceFinalizeArtifactsBaseName)
	opts.AskBecomePass = plan.AskBecomePass && becomePasswordFile == ""
	opts.BecomePasswordFile = becomePasswordFile
	opts.UseControllingTTY = UseControllingTTYForWorkflow(plan.Selected, plan.AskBecomePass && becomePasswordFile == "")
	opts.Label = "finalize host-wide shared-service operation"
	opts.RunLease = runLease
	opts.HostMutationLeaseFinalizer = true
	opts.OwnershipRecordsSnapshot = append([]ownership.ResourceRecord(nil), preRunOwnershipRecords...)
	opts.UseOwnershipRecordsSnapshot = true
	opts.ClassifyUnreachable = true
	opts.PostRunFinalizer = func(result workflow.RunResult) error {
		proofPath := filepath.Join(result.Render.ArtifactsDir, hostSharedServiceFinalizeArtifactsBaseName, ansible.RunResultName)
		return workflow.RequireDestroyCompletionEvidence(proofPath, "host.shared-service-operation.finalize", hosts)
	}
	opts.InvocationArgs = append([]string(nil), invocationArgs...)
	return opts, nil
}
