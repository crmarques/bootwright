package workflow

import (
	"fmt"
	"strings"
)

func blockedApplyTaskReason(ledger RunLedger, task TaskLedgerEntry) string {
	unresolved := []string{}
	for _, dep := range task.Dependencies {
		depTask, ok := ledger.Task(dep)
		if !ok {
			unresolved = append(unresolved, dep+" (missing)")
			continue
		}
		switch depTask.Status {
		case TaskStatusOK, TaskStatusSkipped:
		default:
			unresolved = append(unresolved, fmt.Sprintf("%s (%s)", dep, depTask.Status))
		}
	}
	if len(unresolved) > 0 {
		return "dependencies did not complete: " + strings.Join(unresolved, ", ")
	}
	return "apply task graph could not make progress"
}

func conciseApplyTaskFailure(message string) string {
	lines := strings.Split(message, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "failure:") {
			return trimApplyTaskFailure(strings.TrimSpace(strings.TrimPrefix(line, "failure:")))
		}
	}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "last ") || strings.HasPrefix(line, "underlying error:") {
			continue
		}
		return trimApplyTaskFailure(line)
	}
	return "task failed"
}

func trimApplyTaskFailure(value string) string {
	const limit = 180
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit-3] + "..."
}
