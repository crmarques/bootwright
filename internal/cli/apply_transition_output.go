package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func printApplyTransitionLedger(stdout io.Writer, tasks []workflow.ApplyTask, runsDir string, mode workflow.ApplyMode) {
	objects, err := workflow.ClassifyApplyObjects(tasks, runsDir)
	if err != nil {
		cliout.NewContinuation(stdout).Warning("change plan", "could not classify objects against run records: "+err.Error()+"; the real run recomputes this and fails closed on the same error")
		return
	}
	if len(objects) == 0 {
		return
	}
	byAction := map[workflow.ApplyTransitionAction][]string{}
	unchanged := 0
	for _, tr := range workflow.ClassifyApplyTransitions(objects, mode) {
		if tr.Action == workflow.ApplyTransitionUnchanged {
			unchanged++
			continue
		}
		byAction[tr.Action] = append(byAction[tr.Action], tr.Label)
	}
	ordered := []struct {
		action workflow.ApplyTransitionAction
		label  string
	}{
		{workflow.ApplyTransitionRebuild, "DESTROY & rebuild (data loss)"},
		{workflow.ApplyTransitionReconcile, "reconcile in place"},
		{workflow.ApplyTransitionCreate, "create"},
		{workflow.ApplyTransitionRefuse, "refused — blocks this run"},
	}
	var items []cliout.Item
	for _, entry := range ordered {
		names := byAction[entry.action]
		if len(names) == 0 {
			continue
		}
		sort.Strings(names)
		items = append(items, cliout.Item{Label: entry.label, Detail: strings.Join(names, ", ")})
	}
	if unchanged > 0 {
		items = append(items, cliout.Item{Label: "unchanged", Detail: fmt.Sprintf("%d object(s) already match desired state", unchanged)})
	}
	if len(items) == 0 {
		return
	}
	p := cliout.New(stdout)
	p.Section("Change plan")
	p.List(items)
}
