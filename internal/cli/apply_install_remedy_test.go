package cli

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/internal/converge/remedy"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func TestClusterLifecycleRetryCommandsKeepIdentityAndReplaceUnsafeEffects(t *testing.T) {
	invocation := resolvedInvocation{
		verb:                  invocationApply,
		contextName:           "prod",
		sshIdentityFile:       "/tmp/operator key",
		sshUser:               "operator",
		sshAskSudoPassword:    true,
		sshUserForProvisioned: true,
		flags: invocationFlags{
			mode:                 workflow.ApplyModeReconcile,
			selection:            runSelection{stage: "machines", machines: "worker-0"},
			reclaimDevices:       "all",
			recoverCephOwnership: "ceph=fsid",
			purgeHistory:         true,
			authorizations:       []string{authorizeForeignDaemons, authorizeUnownedDevices},
			dryRun:               true,
			output:               outputJSON,
			yes:                  true,
			askBecomePass:        false,
			trustOnFirstUse:      false,
			verbose:              true,
		},
	}
	tests := []struct {
		name string
		make func() (retryCommand, error)
		want []string
		deny []string
	}{
		{
			name: "regenerate ISO",
			make: func() (retryCommand, error) { return invocation.regenerateClusterISORetry("ocp") },
			want: []string{"bootwright apply", "--mode rebuild", "--authorize foreign-daemons,unowned-devices", "--stage deps", "--clusters ocp"},
			deny: []string{"--authorize data-loss"},
		},
		{
			name: "destroy incomplete install",
			make: func() (retryCommand, error) { return invocation.destroyIncompleteClusterRetry("ocp") },
			want: []string{"bootwright destroy", "--authorize unowned-devices,protected,data-loss", "--stage clusters", "--clusters ocp"},
			deny: []string{"--mode", "--trust-on-first-use"},
		},
		{
			name: "reapply destroyed install",
			make: func() (retryCommand, error) { return invocation.reapplyDestroyedClusterRetry("ocp") },
			want: []string{"bootwright apply", "--mode reconcile", "--authorize foreign-daemons,unowned-devices,data-loss", "--stage clusters", "--clusters ocp"},
		},
		{
			name: "rebuild installed cluster",
			make: func() (retryCommand, error) { return invocation.rebuildInstalledClusterRetry("ocp") },
			want: []string{"bootwright apply", "--mode rebuild", "--authorize foreign-daemons,unowned-devices,data-loss", "--stage clusters", "--clusters ocp"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			command, err := tc.make()
			if err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(command.Args(), " ")
			for _, want := range append(tc.want,
				"--dry-run", "--output json", "--yes", "--ask-become-pass=false", "--verbose", "--context prod",
				"--ssh-id-file /tmp/operator key", "--ssh-user operator", "--ssh-ask-sudo-password", "--ssh-user-for-provisioned") {
				if !strings.Contains(joined, want) {
					t.Fatalf("retry missing %q: %s", want, joined)
				}
			}
			for _, deny := range append(tc.deny, "--machines", "--reclaim-devices", "--recover-ceph-ownership", "--purge-history") {
				if strings.Contains(joined, deny) {
					t.Fatalf("retry retained unsafe or inapplicable effect %q: %s", deny, joined)
				}
			}
		})
	}
}

func TestApplyInstallRemedialErrorsNameCompleteExactSequences(t *testing.T) {
	invocation := resolvedInvocation{
		verb:        invocationApply,
		contextName: "prod",
		flags: invocationFlags{
			mode:            workflow.ApplyModeCreate,
			selection:       runSelection{stage: "base", machines: "worker-0"},
			reclaimDevices:  "all",
			authorizations:  []string{authorizeForeignDaemons},
			yes:             true,
			askBecomePass:   false,
			trustOnFirstUse: false,
		},
	}
	now := time.Now().UTC()
	tests := []struct {
		name          string
		err           error
		commandCount  int
		wantFragments []string
	}{
		{
			name: "create reconciles exact original selection",
			err: &workflow.ClusterInstallStateError{
				Message: "existing install",
				Request: clusterInstallRemedyRequest(remedy.ActionReconcileSameSelection, "ocp"),
			},
			commandCount:  1,
			wantFragments: []string{"--mode reconcile", "--stage base", "--machines worker-0", "--reclaim-devices all"},
		},
		{
			name: "external repair retries exact original selection and intent",
			err: &workflow.ClusterInstallStateError{
				Message: "restore API reachability",
				Request: clusterInstallRemedyRequest(remedy.ActionRetrySameInvocation, "ocp"),
			},
			commandCount:  1,
			wantFragments: []string{"--mode create", "--stage base", "--machines worker-0", "--reclaim-devices all", "--authorize foreign-daemons"},
		},
		{
			name: "execution-time change reconfirms exact original selection",
			err: &workflow.ClusterInstallStateError{
				Message: "availability changed after confirmation",
				Request: clusterInstallRemedyRequest(remedy.ActionRebuildSameSelection, "ocp"),
			},
			commandCount:  1,
			wantFragments: []string{"--mode rebuild", "--stage base", "--machines worker-0", "--reclaim-devices all", "--authorize foreign-daemons,data-loss"},
		},
		{
			name: "preboot skew regenerates then resumes",
			err: &workflow.ClusterInstallVersionError{
				Cluster: "ocp", Phase: workflow.ClusterInstallPhaseISOCreated, InstallerVersion: "4.20.0", DeclaredVersion: "4.21.0",
			},
			commandCount:  2,
			wantFragments: []string{"--mode rebuild", "--stage deps", "--clusters ocp", "then resume the original selected work", "--machines worker-0"},
		},
		{
			name: "stale ISO regenerates then resumes",
			err: &workflow.ClusterInstallISOAgeError{
				Cluster: "ocp", PublishedAt: now.Add(-25 * time.Hour), ObservedAt: now, FreshWindow: 24 * time.Hour,
			},
			commandCount:  2,
			wantFragments: []string{"outside the 24h0m0s freshness window", "--mode rebuild", "--stage deps", "--clusters ocp", "then resume the original selected work", "--machines worker-0"},
		},
		{
			name: "expired resume destroys then reapplies",
			err: &workflow.ClusterInstallResumeExpiredError{
				Cluster: "ocp", Phase: workflow.ClusterInstallPhaseNodesBooted, StartedAt: now.Add(-4 * time.Hour), Deadline: now.Add(-time.Hour),
			},
			commandCount:  2,
			wantFragments: []string{"bootwright destroy", "--authorize protected,data-loss", "bootwright apply", "--mode reconcile", "--authorize foreign-daemons,data-loss", "--stage clusters", "--clusters ocp"},
		},
		{
			name: "completed skew rebuilds",
			err: &workflow.ClusterInstallVersionError{
				Cluster: "ocp", Phase: workflow.ClusterInstallPhaseComplete, InstallerVersion: "4.20.0", DeclaredVersion: "4.21.0", NodesMayHaveBooted: true, InstallCompleted: true,
			},
			commandCount:  1,
			wantFragments: []string{"--mode rebuild", "--authorize foreign-daemons,data-loss", "--stage clusters", "--clusters ocp"},
		},
		{
			name: "uncertain boot rebuilds",
			err: &workflow.ClusterInstallStateError{
				Message: "node boot completion is uncertain",
				Request: clusterInstallRemedyRequest(remedy.ActionRebuildCluster, "ocp"),
			},
			commandCount:  1,
			wantFragments: []string{"--mode rebuild", "--authorize foreign-daemons,data-loss", "--stage clusters", "--clusters ocp"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := applyInstallRemedialError(fmt.Errorf("task boundary: %w", tc.err), invocation)
			if !hasApplyInstallRemedy(err) {
				t.Fatalf("wrapped result lost typed install remedy: %v", err)
			}
			commands := backtickedBootwrightCommands(err.Error())
			if len(commands) != tc.commandCount {
				t.Fatalf("commands = %v, want %d in %v", commands, tc.commandCount, err)
			}
			for _, command := range commands {
				if !commandHasFlagValue(command, "--context", "prod") {
					t.Fatalf("command lost context: %s", command)
				}
			}
			for _, want := range tc.wantFragments {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("remedy missing %q: %v", want, err)
				}
			}
		})
	}
}

func TestEveryRegisteredRemedyActionHasAnExactCLIFormatter(t *testing.T) {
	invocation := resolvedInvocation{
		verb:        invocationApply,
		contextName: "matrix",
		flags: invocationFlags{
			mode:            workflow.ApplyModeReconcile,
			selection:       runSelection{stage: "deps", through: "base", clusters: "ocp"},
			authorizations:  []string{authorizeForeignDaemons},
			yes:             true,
			askBecomePass:   false,
			trustOnFirstUse: true,
		},
	}
	for _, action := range remedy.RegisteredActions() {
		t.Run(string(action), func(t *testing.T) {
			typed := &workflow.ClusterInstallStateError{
				Message: "typed refusal",
				Request: clusterInstallRemedyRequest(action, "ocp"),
			}
			formatted := applyInstallRemedialError(typed, invocation)
			if strings.Contains(formatted.Error(), "has no CLI formatter") {
				t.Fatalf("registered action %q is not formatted: %v", action, formatted)
			}
			commands := backtickedBootwrightCommands(formatted.Error())
			if len(commands) == 0 {
				t.Fatalf("registered action %q emitted no exact command: %v", action, formatted)
			}
			for _, command := range commands {
				assertRetryParses(t, retryCommand{args: strings.Fields(command)}, func(*cobra.Command) {})
			}
		})
	}
}

func TestUnknownOrMalformedRemedyFailsClosedWithoutACommand(t *testing.T) {
	invocation := resolvedInvocation{verb: invocationApply, contextName: "matrix", flags: invocationFlags{mode: workflow.ApplyModeReconcile}}
	tests := []remedy.Request{
		{Action: remedy.Action("future-action"), Targets: []remedy.Target{{Role: remedy.TargetRoleContainerCluster, Name: "ocp"}}},
		{Action: remedy.ActionRegenerateClusterISO},
		{Action: remedy.ActionRegenerateClusterISO, Targets: []remedy.Target{{Role: remedy.TargetRole("future-role"), Name: "ocp"}}},
	}
	for _, request := range tests {
		err := applyInstallRemedialError(&workflow.ClusterInstallStateError{Message: "typed refusal", Request: request}, invocation)
		if len(backtickedBootwrightCommands(err.Error())) != 0 {
			t.Fatalf("invalid remedy request emitted executable argv: request=%#v error=%v", request, err)
		}
		if !strings.Contains(err.Error(), "cannot construct") && !strings.Contains(err.Error(), "has no CLI formatter") {
			t.Fatalf("invalid remedy request did not explain why it stayed command-free: request=%#v error=%v", request, err)
		}
	}
}

func clusterInstallRemedyRequest(action remedy.Action, cluster string) remedy.Request {
	return remedy.Request{
		Action: action,
		Targets: []remedy.Target{{
			Role: remedy.TargetRoleContainerCluster,
			Name: cluster,
		}},
	}
}
