package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/status"
)

func TestNextStepSpineHintsResolveToRegisteredCommands(t *testing.T) {
	secretHints := status.ContextSecretSetHints("matrix", []string{"bmc-credentials", v1alpha1.DefaultPullSecretName})
	cases := []struct {
		name  string
		hints []string
	}{
		{"no state", renderNextStepHints(status.NextStepHints(false, v1alpha1.State{}, "", "", "matrix", nil, false, false))},
		{"state loaded", renderNextStepHints(status.NextStepHints(true, v1alpha1.State{}, "", "", "matrix", nil, false, false))},
		{"state loaded and applied", renderNextStepHints(status.NextStepHints(true, v1alpha1.State{}, "", "", "matrix", nil, false, true))},
		{"host trust missing", renderNextStepHints(status.NextStepHints(true, v1alpha1.State{}, "", "", "matrix", secretHints, true, false))},
		{"generated secrets missing", renderNextStepHints(status.NextStepHints(true, v1alpha1.State{}, "", "", "matrix", []status.NextStepHint{{Args: []string{"bootwright", "secret", "generate", "--context", "matrix"}}}, false, false))},
		{"failed apply ledger", status.LedgerNextSteps(workflow.RunLedger{InvocationArgs: []string{"bootwright", "apply", "--mode", "reconcile", "--stage", "base", "--through", "add-ons", "--clusters", "dc1", "--context", "matrix"}, Status: workflow.RunStatusFailed}, workflow.RunActivity{}, nil)},
		{"failed destroy ledger", status.LedgerNextSteps(workflow.RunLedger{InvocationArgs: []string{"bootwright", "destroy", "--stage", "clusters", "--clusters", "dc1", "--context", "matrix"}, Status: workflow.RunStatusFailed}, workflow.RunActivity{}, nil)},
		{"failed machine-scoped apply ledger", status.LedgerNextSteps(workflow.RunLedger{InvocationArgs: []string{"bootwright", "apply", "--mode", "reconcile", "--machines", "dc1-worker-1", "--context", "matrix"}, Status: workflow.RunStatusFailed}, workflow.RunActivity{}, nil)},
		{"failed machine-scoped destroy ledger", status.LedgerNextSteps(workflow.RunLedger{InvocationArgs: []string{"bootwright", "destroy", "--machines", "dc1-worker-1", "--context", "matrix"}, Status: workflow.RunStatusFailed}, workflow.RunActivity{}, nil)},
		{"running ledger", status.LedgerNextSteps(workflow.RunLedger{Status: workflow.RunStatusRunning}, workflow.RunActivity{}, nil)},
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

func TestFailedRunRetryHintReproducesTheRecordedInvocation(t *testing.T) {
	cases := []struct {
		name   string
		ledger workflow.RunLedger
		want   string
	}{
		{
			name:   "cluster-scoped apply",
			ledger: workflow.RunLedger{Target: "all destroy", Scope: "wider", InvocationArgs: []string{"bootwright", "apply", "--mode", "reconcile", "--stage", "base", "--through", "add-ons", "--clusters", "dc1-ocp", "--context", "matrix"}, Status: workflow.RunStatusFailed},
			want:   "bootwright apply --mode reconcile --stage base --through add-ons --clusters dc1-ocp --context matrix",
		},
		{
			name:   "cluster-scoped destroy",
			ledger: workflow.RunLedger{Target: "all", Scope: "wider", InvocationArgs: []string{"bootwright", "destroy", "--stage", "clusters", "--clusters", "dc1-ocp", "--context", "matrix"}, Status: workflow.RunStatusFailed},
			want:   "bootwright destroy --stage clusters --clusters dc1-ocp --context matrix",
		},
		{
			name:   "machine-scoped apply",
			ledger: workflow.RunLedger{Target: "all destroy", Machines: []string{"wider-machine"}, InvocationArgs: []string{"bootwright", "apply", "--mode", "reconcile", "--machines", "dc1-worker-1", "--context", "matrix"}, Status: workflow.RunStatusFailed},
			want:   "bootwright apply --mode reconcile --machines dc1-worker-1 --context matrix",
		},
		{
			name:   "machine-scoped destroy",
			ledger: workflow.RunLedger{Target: "all", Machines: []string{"wider-machine"}, InvocationArgs: []string{"bootwright", "destroy", "--machines", "dc1-worker-2,dc1-worker-1", "--context", "matrix"}, Status: workflow.RunStatusFailed},
			want:   "bootwright destroy --machines dc1-worker-2,dc1-worker-1 --context matrix",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hints := status.LedgerNextSteps(tc.ledger, workflow.RunActivity{}, nil)
			if len(hints) == 0 {
				t.Fatal("a failed run must emit a retry hint")
			}
			if hints[0] != tc.want {
				t.Fatalf("retry hint is %q, want %q; the hint must reproduce the exact recorded invocation rather than reconstructing it from lossy ledger labels", hints[0], tc.want)
			}
		})
	}
}

func TestNextStepSpineEndsOnClusterInfo(t *testing.T) {
	hints := renderNextStepHints(status.NextStepHints(true, v1alpha1.State{}, "", "", "matrix", nil, false, true))
	if got := hints[len(hints)-1]; got != "bootwright cluster info --context matrix" {
		t.Fatalf("the spine ends on %q, want the post-apply access verb `bootwright cluster info` for the resolved context", got)
	}
}

func TestTypedApplySpineActionPreservesResolvedGlobalIntent(t *testing.T) {
	identity := filepath.Join(t.TempDir(), "operator's key")
	if err := os.WriteFile(identity, []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	previousContext := contextOverride
	previousIdentity := sshIDFile
	previousUser := sshUserOverride
	previousAsk := sshAskSudoPassword
	previousProvisioned := sshUserForProvisioned
	contextOverride = "matrix; $(touch /tmp/not-run)"
	sshIDFile = identity
	sshUserOverride = "operator"
	sshAskSudoPassword = true
	sshUserForProvisioned = true
	t.Cleanup(func() {
		contextOverride = previousContext
		sshIDFile = previousIdentity
		sshUserOverride = previousUser
		sshAskSudoPassword = previousAsk
		sshUserForProvisioned = previousProvisioned
	})

	hints := renderNextStepHints([]status.NextStepHint{{Action: status.NextStepActionApply, ContextName: contextOverride}})
	if len(hints) != 1 {
		t.Fatalf("typed action rendered %v", hints)
	}
	args := shellParseWords(t, hints[0])
	for flag, value := range map[string]string{
		"--mode":        string(workflow.ApplyModeReconcile),
		"--context":     contextOverride,
		"--ssh-id-file": identity,
		"--ssh-user":    "operator",
	} {
		if !parsedWordsHaveFlagValue(args, flag, value) {
			t.Fatalf("typed apply action lost %s=%q: %#v", flag, value, args)
		}
	}
	for _, want := range []string{fmt.Sprintf("--ask-become-pass=%t", askBecomePassDefault()), "--trust-on-first-use=true", "--ssh-ask-sudo-password", "--ssh-user-for-provisioned"} {
		if !slices.Contains(args, want) {
			t.Fatalf("typed apply action lost %q: %#v", want, args)
		}
	}
	if slices.Contains(args, "--yes") {
		t.Fatalf("normal status action invented confirmation: %#v", args)
	}
}

func parsedWordsHaveFlagValue(args []string, flag, value string) bool {
	for index := 0; index+1 < len(args); index++ {
		if args[index] == flag && args[index+1] == value {
			return true
		}
	}
	return false
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
