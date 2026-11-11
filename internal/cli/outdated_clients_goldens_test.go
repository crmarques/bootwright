package cli

import (
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

var outdatedHelm = lifecycle.OutdatedClient{Kind: "helm", Release: "3.14.0", Minimum: "4.3.1", Apply: additiveRoot}

func outdatedHelmWarning() diagnostics.Diagnostic {
	return diagnostics.Diagnostic{
		Severity: "warning", Code: "controller.outdated-client",
		Message:     "apply " + additiveRoot + " proved helm 3.14.0 on this controller, below this build's minimum 4.3.1; a settling apply and a removal keep running it",
		Remediation: "run bootwright destroy --context lab and then bootwright apply --context lab to install this build's default helm, or keep the build that applied it",
	}
}

func outdatedStatus() *lifecycle.StatusResult {
	result := additiveStatus()
	result.Lifecycle.Operation, result.Lifecycle.Lineage = additiveRoot, nil
	result.Lifecycle.Logs = []string{"contexts/lab/operations/" + additiveRoot + "/operation.jsonl"}
	result.LogLocation = "/var/log/bootwright/contexts/lab/operations/" + additiveRoot
	result.OutdatedClients = []lifecycle.OutdatedClient{outdatedHelm}
	result.Warnings = []diagnostics.Diagnostic{outdatedHelmWarning()}
	return result
}

func outdatedDiff() *lifecycle.DiffResult {
	result := diffNone()
	result.OutdatedClients = []lifecycle.OutdatedClient{outdatedHelm}
	result.Warnings = []diagnostics.Diagnostic{outdatedHelmWarning()}
	return result
}

const outdatedHelmStderr = "  warning  controller.outdated-client\n" +
	"    apply op-6f1c2a9e0b7d4c3f8a5e2d1b0c9f8e7a proved helm 3.14.0 on this controller, below this\n" +
	"    build's minimum 4.3.1; a settling apply and a removal keep running it\n" +
	"    Next: run bootwright destroy --context lab and then bootwright apply --context lab to\n" +
	"    install this build's default helm, or keep the build that applied it\n"

func outdatedClientGoldens() []cliGolden {
	return []cliGolden{
		{golden: "cli-status-outdated-client", args: "status", stderr: outdatedHelmStderr, record: func(r *dispatchRecord) { r.result.lifecycleStatus = outdatedStatus() }},
		{golden: "cli-status-outdated-client-json", args: "status --output json", record: func(r *dispatchRecord) { r.result.lifecycleStatus = outdatedStatus() }},
		{golden: "cli-diff-outdated-client", args: "diff", stderr: outdatedHelmStderr, record: func(r *dispatchRecord) { r.result.lifecycleDiff = outdatedDiff() }},
		{golden: "cli-diff-outdated-client-json", args: "diff --output json", record: func(r *dispatchRecord) { r.result.lifecycleDiff = outdatedDiff() }},
	}
}
