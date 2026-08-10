package workflow

import (
	"fmt"
	"strings"
)

func blockedApplyTaskReason(ledger RunLedger, task TaskLedgerEntry) string {
	unresolved := []string{}
	for _, dependency := range taskDependencyRefs(task) {
		depTask, ok := ledger.Task(dependency.id)
		if !ok {
			unresolved = append(unresolved, dependency.id+" (missing)")
			continue
		}
		if !taskDependencySatisfied(depTask.Status, dependency.policy) {
			unresolved = append(unresolved, fmt.Sprintf("%s (%s)", dependency.id, depTask.Status))
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
	return middleEllipsis(strings.TrimSpace(value), 180)
}

func middleEllipsis(value string, limit int) string {
	r := []rune(value)
	if len(r) <= limit {
		return value
	}
	const tail = 44
	head := limit - tail - 1
	if head < 1 {
		return string(r[:limit])
	}
	return string(r[:head]) + "…" + string(r[len(r)-tail:])
}
