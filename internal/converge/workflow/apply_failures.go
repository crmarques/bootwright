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
	return middleEllipsis(strings.TrimSpace(value), 180)
}

// middleEllipsis shortens an over-long single-line reason to limit runes by eliding
// the MIDDLE, so a trailing actionable clause (e.g. "rerun with --override to
// rebuild it") survives next to the leading description instead of being cut off by
// tail truncation. Rune-based so a multibyte character is never split.
func middleEllipsis(value string, limit int) string {
	r := []rune(value)
	if len(r) <= limit {
		return value
	}
	const tail = 44
	head := limit - tail - 1 // room for the ellipsis rune
	if head < 1 {
		return string(r[:limit])
	}
	return string(r[:head]) + "…" + string(r[len(r)-tail:])
}
