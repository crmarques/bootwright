package cli

import (
	"bytes"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/host/shellquote"
)

func TestRuntimeReclaimRetryTemplateRoundTripsHostilePathsAndEveryResolvedFlag(t *testing.T) {
	invocation := resolvedInvocation{
		verb:                  invocationApply,
		contextName:           "prod west",
		sshIdentityFile:       "/keys/operator id",
		sshUser:               "admin user",
		sshAskSudoPassword:    true,
		sshUserForProvisioned: true,
		flags: invocationFlags{
			mode:            workflow.ApplyModeCreate,
			selection:       runSelection{stage: "deps", through: "base", clusters: "ceph-a,ceph-b"},
			reclaimDevices:  "all",
			authorizations:  []string{authorizeForeignDaemons},
			dryRun:          true,
			output:          outputJSON,
			yes:             true,
			askBecomePass:   true,
			trustOnFirstUse: false,
			verbose:         true,
		},
	}
	template, preserved, err := invocation.applyRuntimeReclaimRetryTemplate("/dev/sdb,/dev/disk/by-id/existing")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(preserved, []string{"/dev/sdb", "/dev/disk/by-id/existing"}) {
		t.Fatalf("preserved devices = %v", preserved)
	}
	if got := strings.Count(template.String(), converge.ApplyReclaimInvocationSentinel); got != 1 {
		t.Fatalf("template sentinel count = %d in %q", got, template.String())
	}
	hostile := "/dev/disk/by-id/osd '$(printf injected);$HOME`printf x`\\ end"
	operand := strings.Join(append(append([]string(nil), preserved...), hostile), ",")
	rendered := strings.Replace(template.String(), converge.ApplyReclaimInvocationSentinel, shellquote.QuoteWord(operand), 1)
	parsed := shellParseWords(t, rendered)
	want := append([]string(nil), template.Args()...)
	for index := range want {
		if want[index] == converge.ApplyReclaimInvocationSentinel {
			want[index] = operand
		}
	}
	if !reflect.DeepEqual(parsed, want) {
		t.Fatalf("rendered argv = %#v\nwant %#v\ncommand %s", parsed, want, rendered)
	}
	command := retryCommand{args: parsed}
	assertRetryParses(t, command, func(cmd *cobra.Command) {
		assertParsedFlag(t, cmd, "mode", "create")
		assertParsedFlag(t, cmd, "stage", "deps")
		assertParsedFlag(t, cmd, "through", "base")
		assertParsedFlag(t, cmd, "clusters", "ceph-a,ceph-b")
		assertParsedFlag(t, cmd, "reclaim-devices", operand)
		assertParsedFlag(t, cmd, "dry-run", "true")
		assertParsedFlag(t, cmd, "output", outputJSON)
		assertParsedFlag(t, cmd, "context", "prod west")
		assertParsedFlag(t, cmd, "ssh-id-file", "/keys/operator id")
		assertParsedFlag(t, cmd, "ssh-user", "admin user")
		assertParsedFlag(t, cmd, "ssh-ask-sudo-password", "true")
		assertParsedFlag(t, cmd, "ssh-user-for-provisioned", "true")
		assertParsedFlag(t, cmd, "yes", "true")
		assertParsedFlag(t, cmd, "ask-become-pass", "true")
		assertParsedFlag(t, cmd, "trust-on-first-use", "false")
		assertParsedFlag(t, cmd, "verbose", "true")
		got, authErr := cmd.Flags().GetStringSlice("authorize")
		if authErr != nil {
			t.Fatal(authErr)
		}
		wantAuth := []string{authorizeForeignDaemons, authorizeDataLoss, authorizeUnownedDevices}
		if !reflect.DeepEqual(got, wantAuth) {
			t.Fatalf("parsed authorizations = %v, want %v", got, wantAuth)
		}
	})
}

func TestRuntimeReclaimRetryTemplatePublishesAnEmptyPreservedPathList(t *testing.T) {
	invocation := resolvedInvocation{
		verb: invocationApply,
		flags: invocationFlags{
			mode: workflow.ApplyModeReconcile,
		},
	}
	template, preserved, err := invocation.applyRuntimeReclaimRetryTemplate("")
	if err != nil {
		t.Fatal(err)
	}
	if preserved == nil || len(preserved) != 0 {
		t.Fatalf("preserved devices = %#v, want a non-nil empty list for the Ansible fact", preserved)
	}
	if got := strings.Count(template.String(), converge.ApplyReclaimInvocationSentinel); got != 1 {
		t.Fatalf("template sentinel count = %d in %q", got, template.String())
	}
}

func TestReclaimResolutionRefusalRendersTheExactIntentionalRetry(t *testing.T) {
	invocation := resolvedInvocation{
		verb:                  invocationApply,
		contextName:           "prod west",
		sshIdentityFile:       "/keys/operator id",
		sshUser:               "admin user",
		sshAskSudoPassword:    true,
		sshUserForProvisioned: true,
		flags: invocationFlags{
			mode:            workflow.ApplyModeCreate,
			selection:       runSelection{through: "base", clusters: "ceph-a"},
			reclaimDevices:  "all",
			authorizations:  []string{authorizeForeignDaemons},
			dryRun:          true,
			output:          outputJSON,
			yes:             true,
			askBecomePass:   true,
			trustOnFirstUse: false,
			verbose:         true,
		},
	}
	evidence := &converge.ReclaimAllNoDeclaredDevicesError{
		Clusters:                   []string{"ceph-a"},
		EffectiveUnboundedClusters: []string{"ceph-a"},
	}
	command, err := invocation.applyUnboundedOSDReclaimRetry()
	if err != nil {
		t.Fatal(err)
	}
	message := reclaimResolutionRefusal(evidence, &invocation).Error()
	if !strings.Contains(message, command.String()) {
		t.Fatalf("refusal does not carry exact retry %q: %s", command.String(), message)
	}
	for _, want := range []string{"--mode rebuild", "--authorize foreign-daemons,data-loss", "--through base", "--clusters ceph-a", "--dry-run", "--output json", "--context 'prod west'", "--ssh-id-file '/keys/operator id'", "--ssh-user 'admin user'"} {
		if !strings.Contains(message, want) {
			t.Fatalf("refusal retry dropped %q: %s", want, message)
		}
	}
	if strings.Contains(command.String(), "--reclaim-devices") {
		t.Fatalf("unbounded auto-reclaim retry retained the incompatible explicit reclaim request: %s", command.String())
	}

	narrowing := &converge.ReclaimAllNoDeclaredDevicesError{Clusters: []string{"ceph-a"}}
	same, err := invocation.retry(retryIntent{})
	if err != nil {
		t.Fatal(err)
	}
	message = reclaimResolutionRefusal(narrowing, &invocation).Error()
	if !strings.Contains(message, same.String()) || !strings.Contains(message, "pin the intended disk") {
		t.Fatalf("narrowing refusal must preserve the exact same invocation after desired correction: %s", message)
	}

	mixed := &converge.ReclaimAllNoDeclaredDevicesError{
		Clusters:                   []string{"ceph-a", "ceph-b"},
		EffectiveUnboundedClusters: []string{"ceph-a"},
	}
	message = reclaimResolutionRefusal(mixed, &invocation).Error()
	if !strings.Contains(message, same.String()) || strings.Contains(message, command.String()) {
		t.Fatalf("mixed narrowing/unbounded selection must not advertise rebuild as if it cleared every selected cluster: %s", message)
	}
}

func shellParseWords(t *testing.T, command string) []string {
	t.Helper()
	process := exec.Command("sh", "-c", `eval "set -- $1"; for value do printf '%s\000' "$value"; done`, "sh", command)
	output, err := process.Output()
	if err != nil {
		t.Fatalf("shell-parse %q: %v", command, err)
	}
	parts := bytes.Split(output, []byte{0})
	out := make([]string, 0, len(parts)-1)
	for _, part := range parts[:len(parts)-1] {
		out = append(out, string(part))
	}
	return out
}
