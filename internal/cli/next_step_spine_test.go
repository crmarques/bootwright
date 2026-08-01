package cli

import (
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/status"
)

func TestNextStepSpineHintsResolveToRegisteredCommands(t *testing.T) {
	secretHints := status.ContextSecretSetHints([]string{"bmc-credentials", v1alpha1.DefaultPullSecretName})
	cases := []struct {
		name  string
		hints []string
	}{
		{"no state", status.NextStepHints(false, v1alpha1.State{}, "", "", nil, false, false)},
		{"state loaded", status.NextStepHints(true, v1alpha1.State{}, "", "", nil, false, false)},
		{"state loaded and applied", status.NextStepHints(true, v1alpha1.State{}, "", "", nil, false, true)},
		{"host trust missing", status.NextStepHints(true, v1alpha1.State{}, "", "", secretHints, true, false)},
		{"generated secrets missing", status.NextStepHints(true, v1alpha1.State{}, "", "", []string{"bootwright secret generate"}, false, false)},
		{"failed apply ledger", status.LedgerNextSteps(workflow.RunLedger{Target: "range-base-add-ons", Scope: "dc1", Status: workflow.RunStatusFailed}, workflow.RunActivity{}, nil)},
		{"failed destroy ledger", status.LedgerNextSteps(workflow.RunLedger{Target: "clusters destroy", Scope: "dc1", Status: workflow.RunStatusFailed}, workflow.RunActivity{}, nil)},
		{"running ledger", status.LedgerNextSteps(workflow.RunLedger{Target: "all", Status: workflow.RunStatusRunning}, workflow.RunActivity{}, nil)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if len(tc.hints) == 0 {
				t.Fatal("a next-step spine branch must emit at least one hint")
			}
			for _, hint := range tc.hints {
				assertSpineHintIsAcceptedCommand(t, hint)
			}
		})
	}
}

func TestNextStepSpineEndsOnClusterInfo(t *testing.T) {
	hints := status.NextStepHints(true, v1alpha1.State{}, "", "", nil, false, true)
	if got := hints[len(hints)-1]; got != "bootwright cluster info" {
		t.Fatalf("the spine ends on %q, want the post-apply access verb `bootwright cluster info`", got)
	}
}

func assertSpineHintIsAcceptedCommand(t *testing.T, hint string) {
	t.Helper()
	fields := strings.Fields(hint)
	if len(fields) == 0 || fields[0] != "bootwright" {
		return
	}
	path, flags := splitSpineHint(fields[1:])
	root := newRootCmd(strings.NewReader(""), io.Discard, io.Discard)
	cmd, rest, err := root.Find(path)
	if err != nil {
		t.Fatalf("hint %q names no registered command: %v", hint, err)
	}
	if len(rest) > 0 {
		t.Fatalf("hint %q names no registered command: %q is not a subcommand of %q", hint, rest[0], cmd.CommandPath())
	}
	if cmd.HasSubCommands() {
		t.Fatalf("hint %q stops on %q, which requires a subcommand", hint, cmd.CommandPath())
	}
	for _, name := range flags {
		if !spineFlagAccepted(cmd, name) {
			t.Fatalf("hint %q passes --%s, which %q does not accept", hint, name, cmd.CommandPath())
		}
	}
}

func splitSpineHint(fields []string) ([]string, []string) {
	var path, flags []string
	for _, field := range fields {
		if strings.HasPrefix(field, "-") {
			flags = append(flags, strings.SplitN(strings.TrimLeft(field, "-"), "=", 2)[0])
			continue
		}
		if len(flags) == 0 {
			path = append(path, field)
		}
	}
	return path, flags
}

func spineFlagAccepted(cmd *cobra.Command, name string) bool {
	return cmd.Flags().Lookup(name) != nil || cmd.InheritedFlags().Lookup(name) != nil
}
