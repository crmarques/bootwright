package cli

import (
	"fmt"
	"strings"
	"testing"
	"time"

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
			want: []string{"bootwright apply", "--mode rebuild", "--stage deps", "--clusters ocp"},
			deny: []string{"--authorize data-loss"},
		},
		{
			name: "destroy incomplete install",
			make: func() (retryCommand, error) { return invocation.destroyIncompleteClusterRetry("ocp") },
			want: []string{"bootwright destroy", "--authorize protected,data-loss", "--stage clusters", "--clusters ocp"},
			deny: []string{"--mode", "--trust-on-first-use"},
		},
		{
			name: "reapply destroyed install",
			make: func() (retryCommand, error) { return invocation.reapplyDestroyedClusterRetry("ocp") },
			want: []string{"bootwright apply", "--mode reconcile", "--authorize data-loss", "--stage clusters", "--clusters ocp"},
		},
		{
			name: "rebuild installed cluster",
			make: func() (retryCommand, error) { return invocation.rebuildInstalledClusterRetry("ocp") },
			want: []string{"bootwright apply", "--mode rebuild", "--authorize data-loss", "--stage clusters", "--clusters ocp"},
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
				Remedy:  workflow.ClusterInstallRemedy{Action: workflow.ClusterInstallRemedyReconcile, Cluster: "ocp"},
			},
			commandCount:  1,
			wantFragments: []string{"--mode reconcile", "--stage base", "--machines worker-0", "--reclaim-devices all"},
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
				Remedy:  workflow.ClusterInstallRemedy{Action: workflow.ClusterInstallRemedyFutureRebuild, Cluster: "ocp"},
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
