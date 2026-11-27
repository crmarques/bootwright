package cli

import (
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func contextInstallCheckGolden(r *dispatchRecord) {
	const next = "bootwright apply --stage controller --context lab"
	report := controllerReport("ready", false)
	report.Outcome = "not-ready"
	report.ContextName, report.Machine, report.Next = "lab", "controller", next
	report.Tools = []prerequisites.ToolCheck{
		{Executable: "helm", Version: "v4.2.3", SelectedBy: []string{"OpenShiftCluster/cluster"}, Status: "ready", Observed: "present in the managed tool area"},
		{Executable: "oc", Version: "4.21.15", Compatibility: "openshift", SelectedBy: []string{"OpenShiftCluster/cluster"}, Status: "not-ready", Observed: "managed executable missing"},
	}
	r.result.controller = report
	r.err = diagnostics.NewFailureWithRemediation("preflight.failed", "required controller prerequisites are not ready", "", "run "+next)
}
