package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/environment/inspection"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const narrowWidth = 40

func renderNarrow(t *testing.T, text *display) []string {
	t.Helper()
	var out bytes.Buffer
	if err := text.writeTo(WithPresentation(&out, PresentationOptions{Width: narrowWidth, Color: "never"})); err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
}

func requireWholeLine(t *testing.T, lines []string, want string) {
	t.Helper()
	for _, line := range lines {
		if strings.Contains(line, want) {
			return
		}
	}
	t.Fatalf("%q is not whole on one line in:\n%s", want, strings.Join(lines, "\n"))
}

func TestLifecycleOperationKeepsLogsAndNextOnOneLine(t *testing.T) {
	logs := "/var/lib/example/contexts/lab/state/operations/op-abc/logs"
	next := "bootwright apply --context lab --stage controller --name cluster-one"
	var out bytes.Buffer
	result := &lifecycle.OperationResult{
		Context: lifecycle.ContextIdentity{Name: "lab"}, Verb: "apply",
		LogLocation: logs, NextCommand: next,
		Receipt: lifecycle.Receipt{Operation: "op-abc", Verb: "apply", State: "done"},
	}
	if err := writeLifecycleOperation(WithPresentation(&out, PresentationOptions{Width: narrowWidth, Color: "never"}), result); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out.String(), "\n")
	requireWholeLine(t, lines, logs)
	requireWholeLine(t, lines, next)
}

func TestStatusLifecycleKeepsNextAndLogsOnOneLine(t *testing.T) {
	logs := "/var/lib/example/contexts/lab/state/operations/op-abc/logs"
	next := "bootwright apply --context lab --stage controller --name cluster-one"
	var text display
	writeStatusLifecycle(&text, &lifecycle.StatusResult{
		Lifecycle: &lifecycle.LifecycleSummary{
			Operation: "op-abc", Verb: "apply", State: "failed", Next: next,
			Executable: "/opt/example/bootwright-build-0123456789abcdef", AnotherBuild: true,
		},
		LogLocation: logs,
	})
	lines := renderNarrow(t, &text)
	requireWholeLine(t, lines, logs)
	requireWholeLine(t, lines, next)
	requireWholeLine(t, lines, "/opt/example/bootwright-build-0123456789abcdef, another build than this one")
}

func TestAccessCommandsKeepEachTemplateOnOneLine(t *testing.T) {
	cluster := "cluster-with-a-long-name"
	commands := []inspection.AccessCommand{
		{Command: inspection.CommandOC, Status: inspection.StatusReady},
		{Command: inspection.CommandExec, Status: inspection.StatusUnavailable, Node: "node-one", Reason: "no session"},
	}
	var text display
	writeAccessCommands(&text, "lab", cluster, commands, true)
	lines := renderNarrow(t, &text)
	requireWholeLine(t, lines, accessTemplate("lab", cluster, commands[0]))
	requireWholeLine(t, lines, "template (unavailable): "+accessTemplate("lab", cluster, commands[1]))
}
