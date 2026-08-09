package cli

import (
	"io"

	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/bundle"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/workspace"
)

func printScopedApplyResult(stdout io.Writer, ctx workspace.Context, clustersDir string, usesAnsible bool, bundleResult bundle.AnsibleBundleResult, plan converge.WorkflowPlan, renderResult render.Result, ledger workflow.RunLedger) {
	printRenderResult(stdout, renderResult)
	if usesAnsible {
		printBundlePath(stdout, bundleResult.Dir)
	}
	if plan.TargetsClusters {
		printClusterAccess(stdout, plan.State, renderResult, ledger, ctx.Name, clustersDir)
	}
}
