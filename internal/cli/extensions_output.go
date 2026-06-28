package cli

import (
	"fmt"
	"io"
	"strings"

	extensionplan "github.com/crmarques/bootwright/internal/addons/plan"
	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func printExtensionDryRun(stdout io.Writer, tasks []workflow.ApplyTask) {
	var lines []cliout.TaskLine
	for _, task := range tasks {
		if task.Entry.Kind != workflow.ApplyTaskKindClusterAddon || task.Extension == nil {
			continue
		}
		summary := extensionplan.ResourceSummaries(task.Extension.Extension)
		lines = append(lines, cliout.TaskLine{
			Status: cliout.StatusPending,
			Label:  task.Extension.Cluster + "/" + task.Extension.Name,
			Detail: fmt.Sprintf("%s resources: %s", task.Extension.Extension.Spec.Type, resourceSummaryText(summary)),
		})
	}
	if len(lines) == 0 {
		return
	}
	p := cliout.NewContinuation(stdout)
	p.Section("Add-ons")
	p.Tasks(lines)
}

func resourceSummaryText(resources []extensionplan.ResourceSummary) string {
	if len(resources) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(resources))
	for _, resource := range resources {
		if resource.Namespace == "" {
			parts = append(parts, resource.Kind+"/"+resource.Name)
			continue
		}
		parts = append(parts, resource.Kind+"/"+resource.Namespace+"/"+resource.Name)
	}
	return strings.Join(parts, ", ")
}
