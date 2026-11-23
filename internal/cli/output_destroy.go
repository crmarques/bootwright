package cli

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func cleanupPreviewBlocked(plan lifecycle.PlanResult) bool {
	return slices.ContainsFunc(plan.Checks, func(check lifecycle.PlanCheck) bool {
		return check.ID == "destroy-admission" && check.Status == "failed"
	})
}

func cleanupPreviewAction(step lifecycle.PlanStep) string {
	switch {
	case step.State == "unknown" || step.State == "running":
		return "resolve before cleanup"
	case step.PreviousFailure != nil || step.State == "failed":
		return "will retry"
	default:
		return "would attempt"
	}
}

func retryDependents(steps []lifecycle.PlanStep) map[int][]string {
	var blocked map[int][]string
	for failed, step := range steps {
		if step.PreviousFailure == nil {
			continue
		}
		if blocked == nil {
			blocked = map[int][]string{}
		}
		waits := map[int]bool{failed + 1: true}
		for index := failed + 1; index < len(steps); index++ {
			if steps[index].State != "done" && slices.ContainsFunc(steps[index].After, func(position int) bool { return waits[position] }) {
				waits[index+1] = true
				blocked[failed+1] = append(blocked[failed+1], strconv.Itoa(index+1))
			}
		}
	}
	return blocked
}

func writeCleanupPreview(text *display, plan lifecycle.PlanResult) {
	text.section("Keep going")
	if cleanupPreviewBlocked(plan) {
		text.lines([]string{"No cleanup would start under the current checks."})
	} else {
		text.lines([]string{"Independent cleanup can continue after a recorded target-local failure. Steps that depend on a failed step wait for its cleanup."})
	}
	for _, check := range plan.Checks {
		switch check.ID {
		case "initial-power":
			text.lines([]string{check.Summary + "."})
		case "removal-evidence":
			text.lines([]string{"Bootwright checks removal evidence and quiescence during execution."})
		}
	}
}

func writeRemainingCleanup(text *display, result *lifecycle.OperationResult) {
	steps := make(map[string]lifecycle.PlanStep, len(result.Steps))
	for _, step := range result.Steps {
		steps[step.ID] = step
	}
	text.section("Remaining cleanup")
	completed := 0
	for _, block := range result.Blocks {
		if block.State == "done" {
			completed++
			continue
		}
		step := steps[block.ID]
		label := step.Kind + "/" + step.Object + " (" + block.ID + ")"
		detail := "Pending"
		switch {
		case block.Unresolved != nil:
			detail = "Outcome unverified. " + block.Unresolved.Reason
		case block.Failure != nil:
			detail = "Cleanup failed. " + block.Failure.Message
		case len(block.WaitsFor) != 0:
			detail = "Waiting for " + strings.Join(block.WaitsFor, ", ")
		case block.State == "unknown" || block.State == "running":
			detail = "Outcome unverified"
		case block.State == "failed":
			detail = "Cleanup failed"
		}
		text.fields(field{Label: label, Value: detail})
	}
	if len(result.RetiringWithHosts) != 0 {
		text.fields(field{Label: "Retirement awaiting host cleanup", Value: strings.Join(result.RetiringWithHosts, ", ")})
	}
	text.lines([]string{fmt.Sprintf("%d of %d cleanup steps recorded as complete.", completed, len(result.Blocks))})
}
