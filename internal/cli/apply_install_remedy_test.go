package cli

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/remedy"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/preflight"
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

func TestHostClusterReconcileRemedyKeepsTargetAndOriginalIntentSeparate(t *testing.T) {
	invocation := resolvedInvocation{
		verb:                  invocationApply,
		contextName:           "prod; $(touch /tmp/not-run)",
		sshIdentityFile:       "/keys/operator's key",
		sshUser:               "operator",
		sshAskSudoPassword:    true,
		sshUserForProvisioned: true,
		flags: invocationFlags{
			mode:                 workflow.ApplyModeRebuild,
			selection:            runSelection{stage: "deps", through: "base", clusters: "child-a,child-b"},
			reclaimDevices:       "/dev/disk/by-id/original",
			recoverCephOwnership: "ceph=2088ddee-875b-11f1-9b98-303ea72d7724",
			purgeHistory:         true,
			authorizations:       []string{authorizeDataLoss, authorizeForeignDaemons},
			dryRun:               true,
			output:               outputJSON,
			yes:                  true,
			askBecomePass:        false,
			trustOnFirstUse:      true,
			verbose:              true,
		},
	}
	request := clusterInstallRemedyRequest(remedy.ActionReconcileContainerClusterThenRetrySameSelection, "host; $(touch /tmp/not-run)")
	guidance, err := applyRemedialGuidance(request, invocation)
	if err != nil {
		t.Fatal(err)
	}
	commands := backtickedBootwrightCommands(guidance)
	if len(commands) != 2 {
		t.Fatalf("commands = %v, want one host reconcile and one exact original retry", commands)
	}
	wantPrepare, err := invocation.reconcileContainerClusterRetry("host; $(touch /tmp/not-run)")
	if err != nil {
		t.Fatal(err)
	}
	wantResume, err := invocation.retry(retryIntent{})
	if err != nil {
		t.Fatal(err)
	}
	if got := shellParseWords(t, commands[0]); !slices.Equal(got, wantPrepare.Args()) {
		t.Fatalf("host reconcile argv = %#v, want %#v", got, wantPrepare.Args())
	}
	if got := shellParseWords(t, commands[1]); !slices.Equal(got, wantResume.Args()) {
		t.Fatalf("original retry argv = %#v, want %#v", got, wantResume.Args())
	}
	for _, forbidden := range []string{"child-a,child-b", "--reclaim-devices", "--recover-ceph-ownership", "--purge-history"} {
		if strings.Contains(commands[0], forbidden) {
			t.Fatalf("host reconcile inherited original-only selection or effect %q: %s", forbidden, commands[0])
		}
	}
}

func TestReadOnlyPreflightRemedyNeverInfersAnOriginalApply(t *testing.T) {
	invocation := resolvedInvocation{
		verb:        invocationApply,
		contextName: "prod",
		flags: invocationFlags{
			mode:            workflow.ApplyModeReconcile,
			askBecomePass:   false,
			trustOnFirstUse: false,
		},
	}
	checks := []preflight.Check{{
		Status: preflight.StatusFail,
		Remedy: clusterInstallRemedyRequest(remedy.ActionReconcileContainerClusterThenRetrySameSelection, "host-ocp"),
	}}
	rendered := preflightChecksToOutputForPreflight(checks, invocation)
	if len(rendered) != 1 {
		t.Fatalf("rendered checks = %v", rendered)
	}
	commands := backtickedBootwrightCommands(rendered[0].Remediation)
	if len(commands) != 1 {
		t.Fatalf("read-only preflight inferred a second mutating apply: %v", commands)
	}
	if !commandHasFlagValue(commands[0], "--clusters", "host-ocp") || !commandHasFlagValue(commands[0], "--stage", "clusters") || strings.Contains(commands[0], "--yes") {
		t.Fatalf("preflight remedy is not the exact interactive host reconcile: %s", commands[0])
	}
	assertRetryParses(t, retryCommand{args: shellParseWords(t, commands[0])}, func(*cobra.Command) {})
}

func TestProtectedLayerRemedyFormatsMachineClusterAndMixedSelectionsExactly(t *testing.T) {
	tests := []struct {
		name         string
		selection    runSelection
		machineLayer []string
		clusterLayer []string
		wantStages   []string
	}{
		{name: "machine selection", selection: runSelection{stage: "deps", through: "base", machines: "worker-0"}, machineLayer: []string{"managedMachineOS/worker-0"}, wantStages: []string{"infra"}},
		{name: "cluster selection", selection: runSelection{stage: "clusters", clusters: "ceph"}, clusterLayer: []string{"StorageCluster/ceph"}, wantStages: []string{"clusters"}},
		{name: "mixed selection", selection: runSelection{through: "base", clusters: "ceph"}, machineLayer: []string{"managedMachineOS/ceph-0"}, clusterLayer: []string{"StorageCluster/ceph"}, wantStages: []string{"clusters", "infra"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			invocation := resolvedInvocation{
				verb:                  invocationApply,
				contextName:           "prod",
				sshIdentityFile:       "/tmp/operator-key",
				sshUser:               "operator",
				sshAskSudoPassword:    true,
				sshUserForProvisioned: true,
				flags: invocationFlags{
					mode:            workflow.ApplyModeRebuild,
					selection:       tc.selection,
					reclaimDevices:  "all",
					authorizations:  []string{authorizeForeignDaemons, authorizeUnownedDevices},
					dryRun:          true,
					output:          outputJSON,
					yes:             true,
					askBecomePass:   false,
					trustOnFirstUse: true,
					verbose:         true,
				},
			}
			typed := &converge.ApplyOverrideDestroyProtectionError{
				Reason:       converge.ApplyOverrideProtectionEnvironment,
				Destructive:  []string{"typed object"},
				MachineLayer: tc.machineLayer,
				ClusterLayer: tc.clusterLayer,
			}
			formatted := applyInstallRemedialError(typed, invocation)
			commands := backtickedBootwrightCommands(formatted.Error())
			if len(commands) != len(tc.wantStages)+1 {
				t.Fatalf("commands = %v, want %d teardown(s) plus resume: %v", commands, len(tc.wantStages), formatted)
			}
			for i, stage := range tc.wantStages {
				command := commands[i]
				for _, want := range []string{"bootwright destroy", "--stage " + stage, "--authorize ", "protected", "--dry-run", "--output json", "--yes", "--context prod", "--ssh-user operator"} {
					if !strings.Contains(command, want) {
						t.Fatalf("destroy alternative missing %q: %s", want, command)
					}
				}
				if tc.selection.machines != "" && !commandHasFlagValue(command, "--machines", tc.selection.machines) {
					t.Fatalf("destroy widened machine selection: %s", command)
				}
				if tc.selection.clusters != "" && !commandHasFlagValue(command, "--clusters", tc.selection.clusters) {
					t.Fatalf("destroy widened cluster selection: %s", command)
				}
				for _, deny := range []string{"--mode", "--through", "--reclaim-devices", "--trust-on-first-use", authorizeForeignDaemons} {
					if strings.Contains(command, deny) {
						t.Fatalf("destroy alternative retained apply-only effect %q: %s", deny, command)
					}
				}
				if stage == "clusters" && !strings.Contains(command, "data-loss") {
					t.Fatalf("cluster teardown lacks data-loss authorization: %s", command)
				}
				assertRetryParses(t, retryCommand{args: strings.Fields(command)}, func(*cobra.Command) {})
			}
			resume := commands[len(commands)-1]
			for _, want := range []string{"bootwright apply", "--mode rebuild", "data-loss", "--reclaim-devices all", "--dry-run", "--output json", "--context prod"} {
				if !strings.Contains(resume, want) {
					t.Fatalf("resume missing %q: %s", want, resume)
				}
			}
			if tc.selection.stage != "" && !commandHasFlagValue(resume, "--stage", tc.selection.stage) {
				t.Fatalf("resume lost original stage: %s", resume)
			}
			if tc.selection.through != "" && !commandHasFlagValue(resume, "--through", tc.selection.through) {
				t.Fatalf("resume lost original range: %s", resume)
			}
			if tc.selection.machines != "" && !commandHasFlagValue(resume, "--machines", tc.selection.machines) {
				t.Fatalf("resume lost original machine selection: %s", resume)
			}
			if tc.selection.clusters != "" && !commandHasFlagValue(resume, "--clusters", tc.selection.clusters) {
				t.Fatalf("resume lost original cluster selection: %s", resume)
			}
			assertRetryParses(t, retryCommand{args: strings.Fields(resume)}, func(*cobra.Command) {})
		})
	}
}

func TestProtectedLayerRemedyCommandsReexecuteHermetically(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	tests := []struct {
		name         string
		selection    runSelection
		machineLayer []string
		clusterLayer []string
	}{
		{name: "machine", selection: runSelection{machines: "master-0"}, machineLayer: []string{"managedMachineOS/master-0"}},
		{name: "cluster", selection: runSelection{clusters: "sno-libvirt"}, clusterLayer: []string{"ContainerCluster/sno-libvirt"}},
		{name: "mixed", selection: runSelection{clusters: "sno-libvirt"}, machineLayer: []string{"managedMachineOS/master-0"}, clusterLayer: []string{"ContainerCluster/sno-libvirt"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			invocation := resolvedInvocation{
				verb:        invocationApply,
				contextName: "test",
				flags: invocationFlags{
					mode:            workflow.ApplyModeRebuild,
					selection:       tc.selection,
					dryRun:          true,
					askBecomePass:   false,
					trustOnFirstUse: false,
				},
			}
			formatted := applyInstallRemedialError(&converge.ApplyOverrideDestroyProtectionError{
				Reason:       converge.ApplyOverrideProtectionEnvironment,
				Destructive:  []string{"typed object"},
				MachineLayer: tc.machineLayer,
				ClusterLayer: tc.clusterLayer,
			}, invocation)
			commands := backtickedBootwrightCommands(formatted.Error())
			if len(commands) == 0 {
				t.Fatalf("typed protection action emitted no commands: %v", formatted)
			}
			for _, command := range commands {
				reexecuteHermeticCommand(t, command, "typed object", false)
			}
		})
	}
}

func TestProtectedLayerTargetNamesNeverBecomeSelection(t *testing.T) {
	invocation := resolvedInvocation{
		verb:        invocationApply,
		contextName: "prod",
		flags: invocationFlags{
			mode:            workflow.ApplyModeRebuild,
			selection:       runSelection{clusters: "selected-cluster"},
			dryRun:          true,
			askBecomePass:   false,
			trustOnFirstUse: false,
		},
	}
	typed := &workflow.ClusterInstallStateError{
		Message: "typed protection refusal",
		Request: remedy.Request{
			Action: remedy.ActionDestroyProtectedLayersThenRebuildSameSelection,
			Targets: []remedy.Target{
				{Role: remedy.TargetRoleMachineLayer, Name: "backend-machine-cluster"},
				{Role: remedy.TargetRoleClusterLayer, Name: "backend-cluster"},
			},
		},
	}
	formatted := applyInstallRemedialError(typed, invocation)
	commands := backtickedBootwrightCommands(formatted.Error())
	if len(commands) != 3 {
		t.Fatalf("commands = %v, want both destroy layers and one resume", commands)
	}
	for _, command := range commands {
		if !commandHasFlagValue(command, "--clusters", "selected-cluster") {
			t.Fatalf("command lost resolved selection: %s", command)
		}
		for _, forbidden := range []string{"backend-machine-cluster", "backend-cluster"} {
			if strings.Contains(command, forbidden) {
				t.Fatalf("backend evidence became executable selection argv %q: %s", forbidden, command)
			}
		}
	}
}

func TestLegacyConvergenceEvidenceRemedyPreservesStageRangeAndMachineSelections(t *testing.T) {
	tests := []struct {
		name      string
		selection runSelection
	}{
		{name: "stage", selection: runSelection{stage: "clusters", clusters: "ceph"}},
		{name: "range", selection: runSelection{stage: "deps", through: "base", clusters: "ocp"}},
		{name: "machine", selection: runSelection{machines: "worker-0"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			invocation := resolvedInvocation{
				verb:        invocationApply,
				contextName: "prod",
				flags: invocationFlags{
					mode:            workflow.ApplyModeReconcile,
					selection:       tc.selection,
					reclaimDevices:  "all",
					authorizations:  []string{authorizeForeignDaemons},
					dryRun:          true,
					yes:             true,
					askBecomePass:   false,
					trustOnFirstUse: false,
				},
			}
			typed := &workflow.LegacyConvergenceEvidenceError{ResourceID: "StoragePool/ceph.rbd", Cause: fmt.Errorf("snapshot is missing")}
			formatted := applyInstallRemedialError(typed, invocation)
			commands := backtickedBootwrightCommands(formatted.Error())
			if len(commands) != 1 {
				t.Fatalf("legacy evidence commands = %v, want one exact rebuild: %v", commands, formatted)
			}
			command := commands[0]
			for _, want := range []string{"--mode rebuild", "--authorize foreign-daemons,data-loss", "--reclaim-devices all", "--dry-run", "--yes", "--context prod"} {
				if !strings.Contains(command, want) {
					t.Fatalf("legacy evidence remedy missing %q: %s", want, command)
				}
			}
			for flag, value := range map[string]string{"--stage": tc.selection.stage, "--through": tc.selection.through, "--clusters": tc.selection.clusters, "--machines": tc.selection.machines} {
				if value != "" && !commandHasFlagValue(command, flag, value) {
					t.Fatalf("legacy evidence remedy lost %s=%s: %s", flag, value, command)
				}
			}
			assertRetryParses(t, retryCommand{args: strings.Fields(command)}, func(*cobra.Command) {})
		})
	}
}

func TestLegacyConvergenceEvidenceSelectionsReexecuteHermetically(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	selections := []runSelection{
		{stage: "clusters", clusters: "sno-libvirt"},
		{stage: "deps", through: "base", clusters: "sno-libvirt"},
		{machines: "master-0"},
	}
	for _, selection := range selections {
		invocation := resolvedInvocation{
			verb:        invocationApply,
			contextName: "test",
			flags: invocationFlags{
				mode:            workflow.ApplyModeReconcile,
				selection:       selection,
				dryRun:          true,
				askBecomePass:   false,
				trustOnFirstUse: false,
			},
		}
		formatted := applyInstallRemedialError(&workflow.LegacyConvergenceEvidenceError{ResourceID: "StoragePool/ceph.rbd", Cause: fmt.Errorf("snapshot is missing")}, invocation)
		commands := backtickedBootwrightCommands(formatted.Error())
		if len(commands) != 1 {
			t.Fatalf("legacy evidence selection %#v emitted %v", selection, commands)
		}
		reexecuteHermeticCommand(t, commands[0], "cannot verify legacy safety evidence", false)
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
			request := clusterInstallRemedyRequest(action, "ocp")
			if action == remedy.ActionDestroyProtectedLayersThenRebuildSameSelection {
				request = remedy.Request{Action: action, Targets: []remedy.Target{{Role: remedy.TargetRoleMachineLayer}}}
			}
			typed := &workflow.ClusterInstallStateError{
				Message: "typed refusal",
				Request: request,
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
		{Action: remedy.ActionDestroyProtectedLayersThenRebuildSameSelection},
		{Action: remedy.ActionDestroyProtectedLayersThenRebuildSameSelection, Targets: []remedy.Target{{Role: remedy.TargetRole("future-layer")}}},
		{Action: remedy.ActionDestroyProtectedLayersThenRebuildSameSelection, Targets: []remedy.Target{{Role: remedy.TargetRoleMachineLayer}, {Role: remedy.TargetRoleMachineLayer}}},
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
