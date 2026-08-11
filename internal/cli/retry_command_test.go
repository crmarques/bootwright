package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func TestRetryCommandPreservesResolvedInvocationAndAddsOnlyTheRequiredIntent(t *testing.T) {
	identityDir := filepath.Join(t.TempDir(), "identity dir")
	if err := os.MkdirAll(identityDir, 0o700); err != nil {
		t.Fatal(err)
	}
	identityFile := filepath.Join(identityDir, "operator key")
	if err := os.WriteFile(identityFile, []byte("test identity"), 0o600); err != nil {
		t.Fatal(err)
	}
	sshIDFile = identityFile
	sshUserOverride = "operator"
	sshAskSudoPassword = true
	sshUserForProvisioned = true
	contextOverride = "matrix"
	t.Cleanup(func() {
		sshIDFile = ""
		sshUserOverride = ""
		sshAskSudoPassword = false
		sshUserForProvisioned = false
		contextOverride = ""
	})

	invocation, err := newResolvedInvocation(invocationApply, "matrix", invocationFlags{
		mode:                      workflow.ApplyModeReconcile,
		selection:                 runSelection{stage: "deps", through: "base", clusters: "dc1-ocp,ceph-storage"},
		reclaimDevices:            "all",
		authorizations:            []string{authorizeForeignDaemons},
		yes:                       true,
		askBecomePass:             false,
		trustOnFirstUse:           false,
		verbose:                   true,
		clusterInstallParallelism: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	command := mustRetry(t, invocation, retryIntent{
		mode:                   workflow.ApplyModeRebuild,
		requiredAuthorizations: []string{authorizeDataLoss},
	})
	want := []string{
		"bootwright", "apply",
		"--mode", "rebuild",
		"--cluster-install-parallelism", "2",
		"--authorize", "foreign-daemons,data-loss",
		"--yes",
		"--stage", "deps",
		"--through", "base",
		"--clusters", "dc1-ocp,ceph-storage",
		"--reclaim-devices", "all",
		"--ask-become-pass=false",
		"--trust-on-first-use=false",
		"--verbose",
		"--context", "matrix",
		"--ssh-id-file", identityFile,
		"--ssh-user", "operator",
		"--ssh-ask-sudo-password",
		"--ssh-user-for-provisioned",
	}
	if got := command.Args(); !reflect.DeepEqual(got, want) {
		t.Fatalf("retry args = %#v\nwant %#v", got, want)
	}
	if rendered := command.String(); !strings.Contains(rendered, "--ssh-id-file '"+identityFile+"'") {
		t.Fatalf("shell command must quote the identity path exactly, got %q", rendered)
	}

	assertRetryParses(t, command, func(cmd *cobra.Command) {
		assertParsedFlag(t, cmd, "mode", "rebuild")
		assertParsedFlag(t, cmd, "cluster-install-parallelism", "2")
		assertParsedFlag(t, cmd, "stage", "deps")
		assertParsedFlag(t, cmd, "through", "base")
		assertParsedFlag(t, cmd, "clusters", "dc1-ocp,ceph-storage")
		assertParsedFlag(t, cmd, "machines", "")
		assertParsedFlag(t, cmd, "reclaim-devices", "all")
		assertParsedFlag(t, cmd, "context", "matrix")
		assertParsedFlag(t, cmd, "ssh-id-file", identityFile)
		assertParsedFlag(t, cmd, "ssh-user", "operator")
		assertParsedFlag(t, cmd, "ssh-ask-sudo-password", "true")
		assertParsedFlag(t, cmd, "ssh-user-for-provisioned", "true")
		authorize, err := cmd.Flags().GetStringSlice("authorize")
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(authorize, []string{authorizeForeignDaemons, authorizeDataLoss}) {
			t.Fatalf("parsed --authorize = %v", authorize)
		}
	})
}

func TestRetryCommandExcludesOnlyTheNamedAuthorization(t *testing.T) {
	invocation := resolvedInvocation{
		verb:                  invocationDestroy,
		contextName:           "prod context",
		sshIdentityFile:       "/tmp/operator key",
		sshUser:               "operator",
		sshAskSudoPassword:    true,
		sshUserForProvisioned: true,
		flags: invocationFlags{
			selection:            runSelection{stage: "infra", machines: "worker-0,worker-1"},
			recoverCephOwnership: "ceph=2088ddee-875b-11f1-9b98-303ea72d7724",
			purgeHistory:         true,
			authorizations:       []string{authorizeProtected, authorizeUnreachableNodes, authorizeDataLoss},
			yes:                  true,
			askBecomePass:        false,
			verbose:              true,
		},
	}
	command := mustRetry(t, invocation, retryIntent{excludedAuthorization: authorizeUnreachableNodes})
	joined := strings.Join(command.Args(), " ")
	for _, want := range []string{
		"--authorize protected,data-loss",
		"--yes",
		"--stage infra",
		"--machines worker-0,worker-1",
		"--recover-ceph-ownership ceph=2088ddee-875b-11f1-9b98-303ea72d7724",
		"--purge-history",
		"--context prod context",
		"--ssh-id-file /tmp/operator key",
		"--ssh-user operator",
		"--ssh-ask-sudo-password",
		"--ssh-user-for-provisioned",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("authorization-removal retry missing %q: %s", want, joined)
		}
	}
	if strings.Contains(joined, authorizeUnreachableNodes) || strings.Contains(joined, authorizeAll) {
		t.Fatalf("authorization-removal retry retained the excluded or blanket token: %s", joined)
	}
	assertRetryParses(t, command, func(cmd *cobra.Command) {
		authorize, err := cmd.Flags().GetStringSlice("authorize")
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(authorize, []string{authorizeProtected, authorizeDataLoss}) {
			t.Fatalf("parsed --authorize = %v", authorize)
		}
	})
}

func TestRetryCommandExpandsAllBeforeExcludingOneAuthorization(t *testing.T) {
	invocation := resolvedInvocation{
		verb:        invocationDestroy,
		contextName: "matrix",
		flags: invocationFlags{
			selection:      runSelection{clusters: "ceph-storage"},
			purgeHistory:   true,
			authorizations: []string{authorizeAll},
			yes:            true,
		},
	}
	command := mustRetry(t, invocation, retryIntent{excludedAuthorization: authorizeUnreachableNodes})
	want := []string{
		authorizeDataLoss,
		authorizeProtected,
		authorizeInstalledClusterNode,
		authorizeUnownedVMs,
		authorizeUnownedNetworks,
		authorizeUnownedDevices,
		authorizeUnreadableRecords,
		authorizeSharedInfra,
		authorizeStaleInput,
	}
	if got := retryAuthorizations(command.Args()); !reflect.DeepEqual(got, want) {
		t.Fatalf("all-minus-unreachable authorizations = %v, want %v; command=%s", got, want, command.String())
	}
	if got := retryAuthorizations(command.Args()); slices.Contains(got, authorizeAll) || slices.Contains(got, authorizeUnreachableNodes) {
		t.Fatalf("all-minus-unreachable retry must carry neither blanket nor excluded authorization: %s", command.String())
	}
	for _, preserved := range []string{"--clusters ceph-storage", "--purge-history", "--yes", "--context matrix"} {
		if !strings.Contains(command.String(), preserved) {
			t.Fatalf("all-minus-unreachable retry missing %q: %s", preserved, command.String())
		}
	}
}

func TestRetryCommandRefusesInvalidAuthorizationExclusions(t *testing.T) {
	invocation := resolvedInvocation{verb: invocationDestroy, flags: invocationFlags{authorizations: []string{authorizeProtected}}}
	for _, intent := range []retryIntent{
		{excludedAuthorization: authorizeAll},
		{excludedAuthorization: authorizeForeignDaemons},
		{excludedAuthorization: authorizeUnreachableNodes},
		{requiredAuthorizations: []string{authorizeProtected}, excludedAuthorization: authorizeProtected},
	} {
		if _, err := invocation.retry(intent); err == nil {
			t.Fatalf("invalid exclusion %+v unexpectedly produced a retry", intent)
		}
	}
}

func TestDestroyContextRetryQuotesTheExactContext(t *testing.T) {
	command, err := destroyContextRetry("prod context;east")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := command.Args(), []string{"bootwright", "destroy", "--context", "prod context;east"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("context destroy args = %v, want %v", got, want)
	}
	if got := command.String(); got != "bootwright destroy --context 'prod context;east'" {
		t.Fatalf("context destroy command = %q", got)
	}
	if _, err := destroyContextRetry(" "); err == nil {
		t.Fatal("blank context unexpectedly produced a destroy retry")
	}
}

func TestProtectedLayerDestroyRetriesRetainExactSelectionAndClearApplyEffects(t *testing.T) {
	base := resolvedInvocation{
		verb:                  invocationApply,
		contextName:           "prod",
		sshIdentityFile:       "/tmp/operator-key",
		sshUser:               "operator",
		sshAskSudoPassword:    true,
		sshUserForProvisioned: true,
		flags: invocationFlags{
			mode:                 workflow.ApplyModeRebuild,
			selection:            runSelection{stage: "deps", through: "base", machines: "worker-0,worker-1"},
			reclaimDevices:       "all",
			recoverCephOwnership: "ceph=fsid",
			purgeHistory:         true,
			authorizations:       []string{authorizeForeignDaemons, authorizeUnownedDevices},
			dryRun:               true,
			output:               outputJSON,
			yes:                  true,
			askBecomePass:        false,
			trustOnFirstUse:      true,
			verbose:              true,
		},
	}
	tests := []struct {
		name      string
		make      func() (retryCommand, error)
		stage     string
		authorize string
	}{
		{name: "machine layer", make: base.destroySelectedMachineLayerRetry, stage: "infra", authorize: "unowned-devices,protected"},
		{name: "cluster layer", make: base.destroySelectedClusterLayerRetry, stage: "clusters", authorize: "unowned-devices,protected,data-loss"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			command, err := tc.make()
			if err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(command.Args(), " ")
			for _, want := range []string{
				"bootwright destroy", "--authorize " + tc.authorize, "--stage " + tc.stage,
				"--machines worker-0,worker-1", "--dry-run", "--output json", "--yes",
				"--ask-become-pass=false", "--verbose", "--context prod", "--ssh-id-file /tmp/operator-key",
				"--ssh-user operator", "--ssh-ask-sudo-password", "--ssh-user-for-provisioned",
			} {
				if !strings.Contains(joined, want) {
					t.Fatalf("protected-layer retry missing %q: %s", want, joined)
				}
			}
			for _, deny := range []string{"--mode", "--through", "--clusters", "--reclaim-devices", "--recover-ceph-ownership", "--purge-history", "--trust-on-first-use", authorizeForeignDaemons} {
				if strings.Contains(joined, deny) {
					t.Fatalf("protected-layer retry retained inapplicable effect %q: %s", deny, joined)
				}
			}
			assertRetryParses(t, command, func(cmd *cobra.Command) {
				assertParsedFlag(t, cmd, "stage", tc.stage)
				assertParsedFlag(t, cmd, "machines", "worker-0,worker-1")
				assertParsedFlag(t, cmd, "clusters", "")
			})
		})
	}
}

func TestProtectedLayerDestroyRetriesRejectImplicitWholeContext(t *testing.T) {
	invocation := resolvedInvocation{
		verb:        invocationApply,
		contextName: "prod",
		flags: invocationFlags{
			mode: workflow.ApplyModeRebuild,
		},
	}
	for _, build := range []func() (retryCommand, error){
		invocation.destroySelectedMachineLayerRetry,
		invocation.destroySelectedClusterLayerRetry,
	} {
		if command, err := build(); err == nil || len(command.Args()) != 0 {
			t.Fatalf("implicit whole-context protected destroy = %#v, err=%v", command.Args(), err)
		}
	}
}

func TestDestroyRetryPreservesClusterScopeRecoveryPurgeAndAuthorizations(t *testing.T) {
	invocation := resolvedInvocation{
		verb:        invocationDestroy,
		contextName: "matrix",
		flags: invocationFlags{
			selection:            runSelection{stage: "clusters", clusters: "ceph"},
			recoverCephOwnership: "ceph=2088ddee-875b-11f1-9b98-303ea72d7724",
			purgeHistory:         true,
			authorizations:       []string{authorizeProtected, authorizeInstalledClusterNode},
			yes:                  true,
			askBecomePass:        false,
			verbose:              true,
		},
	}
	command := mustRetry(t, invocation, retryIntent{requiredAuthorizations: []string{authorizeDataLoss}})
	want := []string{
		"bootwright", "destroy",
		"--authorize", "protected,installed-cluster-node,data-loss",
		"--yes",
		"--stage", "clusters",
		"--clusters", "ceph",
		"--recover-ceph-ownership", "ceph=2088ddee-875b-11f1-9b98-303ea72d7724",
		"--purge-history",
		"--ask-become-pass=false",
		"--verbose",
		"--context", "matrix",
	}
	if got := command.Args(); !reflect.DeepEqual(got, want) {
		t.Fatalf("retry args = %#v\nwant %#v", got, want)
	}
	assertRetryParses(t, command, func(cmd *cobra.Command) {
		assertParsedFlag(t, cmd, "stage", "clusters")
		assertParsedFlag(t, cmd, "machines", "")
		assertParsedFlag(t, cmd, "clusters", "ceph")
		assertParsedFlag(t, cmd, "recover-ceph-ownership", "ceph=2088ddee-875b-11f1-9b98-303ea72d7724")
		assertParsedFlag(t, cmd, "purge-history", "true")
		assertParsedFlag(t, cmd, "context", "matrix")
	})
}

func TestCrossVerbRetryExpandsAllUnderTheSourceAndCarriesOnlyTargetIntersection(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "must-not-exist")
	hostileCluster := "cluster west;$(touch " + marker + ")"
	hostileContext := "context west;$(touch " + marker + ")"
	hostileIdentity := "/tmp/operator key;$(touch " + marker + ")"
	hostileUser := "operator;$(touch " + marker + ")"
	apply := resolvedInvocation{
		verb:                  invocationApply,
		contextName:           hostileContext,
		sshIdentityFile:       hostileIdentity,
		sshUser:               hostileUser,
		sshAskSudoPassword:    true,
		sshUserForProvisioned: true,
		flags: invocationFlags{
			mode:            workflow.ApplyModeReconcile,
			selection:       runSelection{stage: "clusters", clusters: "original"},
			authorizations:  []string{authorizeAll},
			dryRun:          true,
			yes:             true,
			askBecomePass:   false,
			trustOnFirstUse: true,
			verbose:         true,
		},
	}
	destroy := apply
	destroy.verb = invocationDestroy
	replace := apply
	replace.verb = invocationReplaceArbiter
	applyMachine := apply
	applyMachine.flags.selection = runSelection{machines: hostileCluster}
	applyCluster := apply
	applyCluster.flags.selection = runSelection{clusters: hostileCluster}

	tests := []struct {
		name      string
		build     func() (retryCommand, error)
		wantVerb  invocationVerb
		wantAuth  []string
		scopeFlag string
		wantScope string
	}{
		{
			name: "apply to destroy selected clusters",
			build: func() (retryCommand, error) {
				return apply.destroyClustersRetry([]string{hostileCluster})
			},
			wantVerb:  invocationDestroy,
			wantAuth:  []string{authorizeDataLoss, authorizeUnownedDevices},
			scopeFlag: "clusters",
			wantScope: hostileCluster,
		},
		{
			name: "apply to destroy incomplete cluster",
			build: func() (retryCommand, error) {
				return apply.destroyIncompleteClusterRetry(hostileCluster)
			},
			wantVerb:  invocationDestroy,
			wantAuth:  []string{authorizeDataLoss, authorizeUnownedDevices, authorizeProtected},
			scopeFlag: "clusters",
			wantScope: hostileCluster,
		},
		{
			name: "apply to destroy protected machine layer",
			build: func() (retryCommand, error) {
				return applyMachine.destroySelectedMachineLayerRetry()
			},
			wantVerb:  invocationDestroy,
			wantAuth:  []string{authorizeDataLoss, authorizeUnownedDevices, authorizeProtected},
			scopeFlag: "machines",
			wantScope: hostileCluster,
		},
		{
			name: "apply to destroy protected cluster layer",
			build: func() (retryCommand, error) {
				return applyCluster.destroySelectedClusterLayerRetry()
			},
			wantVerb:  invocationDestroy,
			wantAuth:  []string{authorizeDataLoss, authorizeUnownedDevices, authorizeProtected},
			scopeFlag: "clusters",
			wantScope: hostileCluster,
		},
		{
			name: "apply to replace arbiter",
			build: func() (retryCommand, error) {
				return apply.replaceArbiterRetry(hostileCluster)
			},
			wantVerb:  invocationReplaceArbiter,
			scopeFlag: "name",
			wantScope: hostileCluster,
		},
		{
			name: "replace arbiter to destroy selected machine",
			build: func() (retryCommand, error) {
				return replace.destroyMachinesRetry([]string{hostileCluster}, authorizeInstalledClusterNode)
			},
			wantVerb:  invocationDestroy,
			wantAuth:  []string{authorizeUnreachableNodes, authorizeInstalledClusterNode},
			scopeFlag: "machines",
			wantScope: hostileCluster,
		},
		{
			name: "destroy to apply selected clusters",
			build: func() (retryCommand, error) {
				return destroy.applyClustersRetry([]string{hostileCluster}, authorizeForeignDaemons)
			},
			wantVerb:  invocationApply,
			wantAuth:  []string{authorizeDataLoss, authorizeUnownedDevices, authorizeForeignDaemons},
			scopeFlag: "clusters",
			wantScope: hostileCluster,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			command, err := tc.build()
			if err != nil {
				t.Fatal(err)
			}
			wantPrefix := []string{"bootwright", string(tc.wantVerb)}
			if tc.wantVerb == invocationReplaceArbiter {
				wantPrefix = []string{"bootwright", "storage-cluster", "replace-arbiter"}
			}
			if args := command.Args(); len(args) < len(wantPrefix) || !slices.Equal(args[:len(wantPrefix)], wantPrefix) {
				t.Fatalf("cross-verb retry prefix = %v, want %v", args, wantPrefix)
			}
			if got := shellParseWords(t, command.String()); !reflect.DeepEqual(got, command.Args()) {
				t.Fatalf("shell round trip = %#v\nwant %#v", got, command.Args())
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("hostile retry operand escaped shell quoting: %v", err)
			}
			if got := retryAuthorizations(command.Args()); !reflect.DeepEqual(got, tc.wantAuth) {
				t.Fatalf("projected authorizations = %v, want %v", got, tc.wantAuth)
			}
			for _, forbidden := range []string{authorizeAll, authorizeProtected, authorizeInstalledClusterNode, authorizeUnownedVMs, authorizeUnownedNetworks, authorizeUnreachableNodes, authorizeUnreadableRecords, authorizeSharedInfra, authorizeStaleInput, authorizeSameSiteArbiter, authorizeDegradedQuorum} {
				if slices.Contains(tc.wantAuth, forbidden) {
					continue
				}
				if slices.Contains(retryAuthorizations(command.Args()), forbidden) {
					t.Fatalf("cross-verb retry invented unrelated target authorization %q: %s", forbidden, command.String())
				}
			}
			assertRetryParses(t, command, func(cmd *cobra.Command) {
				assertParsedFlag(t, cmd, "context", hostileContext)
				assertParsedFlag(t, cmd, "ssh-id-file", hostileIdentity)
				assertParsedFlag(t, cmd, "ssh-user", hostileUser)
				assertParsedFlag(t, cmd, "ssh-ask-sudo-password", "true")
				assertParsedFlag(t, cmd, "ssh-user-for-provisioned", "true")
				assertParsedFlag(t, cmd, tc.scopeFlag, tc.wantScope)
			})
		})
	}
}

func TestSameVerbRetryPreservesLiteralAll(t *testing.T) {
	tests := []struct {
		name  string
		build func() (retryCommand, error)
	}{
		{
			name: "apply",
			build: func() (retryCommand, error) {
				invocation := resolvedInvocation{verb: invocationApply, flags: invocationFlags{mode: workflow.ApplyModeReconcile, authorizations: []string{authorizeAll}}}
				return invocation.applyClustersRetry([]string{"ocp"}, authorizeDataLoss)
			},
		},
		{
			name: "destroy",
			build: func() (retryCommand, error) {
				invocation := resolvedInvocation{verb: invocationDestroy, flags: invocationFlags{authorizations: []string{authorizeAll}}}
				return invocation.clusterLifecycleRetry(invocationDestroy, "ocp", "clusters", "", authorizeProtected)
			},
		},
		{
			name: "replace-arbiter",
			build: func() (retryCommand, error) {
				invocation := resolvedInvocation{verb: invocationReplaceArbiter, flags: invocationFlags{authorizations: []string{authorizeAll}}}
				return invocation.replaceArbiterRetry("ceph")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			command, err := tc.build()
			if err != nil {
				t.Fatal(err)
			}
			if got := retryAuthorizations(command.Args()); !reflect.DeepEqual(got, []string{authorizeAll}) {
				t.Fatalf("same-verb retry authorizations = %v, want literal all: %s", got, command.String())
			}
		})
	}
}

func retryAuthorizations(args []string) []string {
	for i, arg := range args {
		if arg == "--authorize" && i+1 < len(args) {
			return strings.Split(args[i+1], ",")
		}
		if value, found := strings.CutPrefix(arg, "--authorize="); found {
			return strings.Split(value, ",")
		}
	}
	return nil
}

func TestResolvedInvocationKeepsTheExplicitContextIdentity(t *testing.T) {
	contextOverride = "operator-selected"
	sshIDFile = ""
	t.Cleanup(func() { contextOverride = "" })
	invocation, err := newResolvedInvocation(invocationDestroy, "canonical-context", invocationFlags{askBecomePass: false})
	if err != nil {
		t.Fatal(err)
	}
	command := mustRetry(t, invocation, retryIntent{})
	if got := strings.Join(command.Args(), " "); !strings.Contains(got, "--context operator-selected") || strings.Contains(got, "canonical-context") {
		t.Fatalf("retry must preserve the explicitly resolved --context identity: %s", got)
	}
}

func TestRetrySerializationPreservesInvalidCombinedSelectorsAndCLIRejectsThem(t *testing.T) {
	invocation := resolvedInvocation{
		verb: invocationDestroy,
		flags: invocationFlags{
			selection:      runSelection{stage: "infra", clusters: "ocp", machines: "worker-1,worker-2"},
			authorizations: []string{authorizeProtected},
			yes:            true,
			askBecomePass:  false,
		},
	}
	command := mustRetry(t, invocation, retryIntent{requiredAuthorizations: []string{authorizeInstalledClusterNode}})
	joined := strings.Join(command.Args(), " ")
	for _, want := range []string{"--clusters ocp", "--machines worker-1,worker-2", "--stage infra", "--authorize protected,installed-cluster-node"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("lossless serialization missing %q: %s", want, joined)
		}
	}

	root := newRootCmd(bytes.NewReader(nil), &bytes.Buffer{}, &bytes.Buffer{})
	root.PersistentPreRunE = nil
	root.SetArgs(command.Args()[1:])
	err := root.Execute()
	var exit *exitError
	if !errors.As(err, &exit) || exit.code != 2 || !strings.Contains(err.Error(), "--machines and --clusters are mutually exclusive") {
		t.Fatalf("combined selector retry error = %v, want exit 2 mutually-exclusive refusal", err)
	}
}

func TestDestroyPreviewRetryCannotTurnIntoARealDestroy(t *testing.T) {
	invocation := resolvedInvocation{
		verb:        invocationDestroy,
		contextName: "matrix",
		flags: invocationFlags{
			selection:      runSelection{stage: "clusters", clusters: "ceph"},
			authorizations: []string{authorizeProtected},
			dryRun:         true,
			output:         outputJSON,
			askBecomePass:  false,
		},
	}
	command := mustRetry(t, invocation, retryIntent{requiredAuthorizations: []string{authorizeStaleInput}})
	joined := strings.Join(command.Args(), " ")
	for _, want := range []string{"--authorize protected,stale-input", "--stage clusters", "--clusters ceph", "--dry-run", "--output json", "--context matrix"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("preview retry missing %q: %s", want, joined)
		}
	}
	if strings.Contains(joined, "--yes") {
		t.Fatalf("preview retry invented confirmation bypass: %s", joined)
	}
}

func TestRetryCommandShellQuotesEveryNonWordValue(t *testing.T) {
	invocation := resolvedInvocation{
		verb:            invocationDestroy,
		contextName:     "matrix;other",
		sshIdentityFile: "/tmp/operator;key",
		flags:           invocationFlags{selection: runSelection{clusters: "ceph;other"}},
	}
	rendered := mustRetry(t, invocation, retryIntent{}).String()
	for _, want := range []string{"'matrix;other'", "'/tmp/operator;key'", "'ceph;other'"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("retry command must shell-quote %q: %s", want, rendered)
		}
	}
}

func TestEveryApplyDestroyFlagIsPreservedByTheRetryBuilder(t *testing.T) {
	rootFields := resolvedInvocation{
		contextName:           "matrix",
		sshIdentityFile:       "/tmp/id",
		sshUser:               "operator",
		sshAskSudoPassword:    true,
		sshUserForProvisioned: true,
	}
	applyCluster := rootFields
	applyCluster.verb = invocationApply
	applyCluster.flags = invocationFlags{
		mode:                      workflow.ApplyModeRebuild,
		selection:                 runSelection{stage: "deps", through: "base", clusters: "ocp"},
		reclaimDevices:            "/dev/sdb",
		authorizations:            []string{authorizeDataLoss},
		dryRun:                    true,
		output:                    outputJSON,
		yes:                       true,
		askBecomePass:             true,
		trustOnFirstUse:           false,
		verbose:                   true,
		clusterInstallParallelism: 2,
	}
	applyMachine := applyCluster
	applyMachine.flags.selection = runSelection{stage: "machines", machines: "worker-1"}
	destroyCluster := rootFields
	destroyCluster.verb = invocationDestroy
	destroyCluster.flags = invocationFlags{
		selection:            runSelection{stage: "clusters", clusters: "ceph"},
		recoverCephOwnership: "ceph=2088ddee-875b-11f1-9b98-303ea72d7724",
		purgeHistory:         true,
		authorizations:       []string{authorizeDataLoss},
		dryRun:               true,
		output:               outputJSON,
		yes:                  true,
		askBecomePass:        true,
		verbose:              true,
	}
	destroyMachine := destroyCluster
	destroyMachine.flags.selection = runSelection{stage: "infra", machines: "worker-1"}
	destroyMachine.flags.recoverCephOwnership = ""

	generated := map[string][]retryCommand{
		"apply": {
			mustRetry(t, applyCluster, retryIntent{}),
			mustRetry(t, applyMachine, retryIntent{}),
		},
		"destroy": {
			mustRetry(t, destroyCluster, retryIntent{}),
			mustRetry(t, destroyMachine, retryIntent{}),
		},
	}
	for verb, command := range mutatingVerbCommands(t) {
		preserved := map[string]bool{}
		for _, retry := range generated[verb] {
			for _, name := range retryFlagNames(retry.Args()) {
				preserved[name] = true
			}
		}
		visit := func(flag *pflag.Flag) {
			if !preserved[flag.Name] {
				t.Errorf("`bootwright %s --%s` has no representation in resolvedInvocation; a refusal could drop it and change the retry's effect or target", verb, flag.Name)
			}
		}
		command.Flags().VisitAll(visit)
		command.InheritedFlags().VisitAll(visit)
	}
}

func retryFlagNames(args []string) []string {
	var out []string
	for _, arg := range args {
		if !strings.HasPrefix(arg, "--") {
			continue
		}
		name := strings.TrimPrefix(arg, "--")
		if before, _, found := strings.Cut(name, "="); found {
			name = before
		}
		out = append(out, name)
	}
	return out
}

func TestApplyModeRetryClearsOnlyTheNamedGateWithoutWidening(t *testing.T) {
	task := workflow.ApplyTask{Entry: workflow.TaskLedgerEntry{ID: "storage.ceph", Kind: workflow.ApplyTaskKindStorageCluster, Label: "storage.ceph", Cluster: "ceph"}}
	runsDir := t.TempDir()
	if err := workflow.SaveConvergeSafetyRecord(runsDir, workflow.ConvergeSafetyRecord{
		APIVersion:   workflow.ConvergeSafetyAPIVersion,
		ResourceID:   workflow.ApplyTaskKindStorageCluster + "/storage.ceph",
		ResourceKind: workflow.ApplyTaskKindStorageCluster,
		TaskID:       task.Entry.ID,
		TaskKind:     task.Entry.Kind,
		DesiredHash:  "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		HashSchema:   workflow.ConvergeHashSchema,
		Owner:        workflow.ConvergeSafetyOwnerIdentity{Manager: workflow.ConvergeSafetyOwner, Context: "test"},
		Status:       workflow.ConvergeSafetyStatusReconciled,
	}); err != nil {
		t.Fatal(err)
	}
	objects, err := workflow.ClassifyApplyObjects([]workflow.ApplyTask{task}, runsDir, "test")
	if err != nil {
		t.Fatal(err)
	}
	preflightErr := workflow.EvaluateApplyModePreflight(workflow.ApplyModeReconcile, objects)
	if preflightErr == nil {
		t.Fatal("reconcile must refuse structural drift")
	}
	invocation := resolvedInvocation{
		verb:        invocationApply,
		contextName: "matrix",
		flags: invocationFlags{
			mode:            workflow.ApplyModeReconcile,
			selection:       runSelection{stage: "clusters", clusters: "ceph"},
			authorizations:  []string{authorizeForeignDaemons},
			yes:             true,
			askBecomePass:   false,
			trustOnFirstUse: true,
		},
	}
	formatted := applyModePreflightRefusal(preflightErr, invocation).Error()
	command := mustRetry(t, invocation, retryIntent{mode: workflow.ApplyModeRebuild, requiredAuthorizations: []string{authorizeDataLoss}})
	if !strings.Contains(formatted, "`"+command.String()+"`") {
		t.Fatalf("refusal must name its exact typed retry command:\n%s", formatted)
	}
	for _, forbidden := range []string{"--machines", "--clusters dc1-ocp", "bootwright apply --mode rebuild`"} {
		if strings.Contains(formatted, forbidden) {
			t.Fatalf("scoped retry widened or dropped its selection via %q:\n%s", forbidden, formatted)
		}
	}
	if err := workflow.EvaluateApplyModePreflight(workflow.ApplyModeRebuild, objects); err != nil {
		t.Fatalf("the retry mode must clear the named structural-drift gate: %v", err)
	}
	auth, err := parseAuthorizations([]string{authorizeForeignDaemons, authorizeDataLoss}, authorizeVerbApply)
	if err != nil {
		t.Fatal(err)
	}
	if err := destructiveOverrideYesGuard([]string{"StorageCluster/ceph"}, true, auth.has(authorizeDataLoss), invocation); err != nil {
		t.Fatalf("the retry token must clear the next data-loss gate: %v", err)
	}
}

func TestCreateModeRetryChangesOnlyModeAndKeepsTheSelectedWorkSet(t *testing.T) {
	task := workflow.ApplyTask{Entry: workflow.TaskLedgerEntry{ID: "addon.ocp", Kind: workflow.ApplyTaskKindClusterAddon, Label: "addon.ocp", Cluster: "ocp"}}
	desiredHash, err := task.DesiredHash()
	if err != nil {
		t.Fatal(err)
	}
	runsDir := t.TempDir()
	if err := workflow.SaveConvergeSafetyRecord(runsDir, workflow.ConvergeSafetyRecord{
		APIVersion:   workflow.ConvergeSafetyAPIVersion,
		ResourceID:   workflow.ApplyTaskKindClusterAddon + "/addon.ocp",
		ResourceKind: workflow.ApplyTaskKindClusterAddon,
		TaskID:       task.Entry.ID,
		TaskKind:     task.Entry.Kind,
		DesiredHash:  desiredHash,
		HashSchema:   workflow.ConvergeHashSchema,
		Owner:        workflow.ConvergeSafetyOwnerIdentity{Manager: workflow.ConvergeSafetyOwner, Context: "test"},
		Status:       workflow.ConvergeSafetyStatusReconciled,
	}); err != nil {
		t.Fatal(err)
	}
	objects, err := workflow.ClassifyApplyObjects([]workflow.ApplyTask{task}, runsDir, "test")
	if err != nil {
		t.Fatal(err)
	}
	preflightErr := workflow.EvaluateApplyModePreflight(workflow.ApplyModeCreate, objects)
	if preflightErr == nil {
		t.Fatal("create must refuse an existing matching object")
	}
	invocation := resolvedInvocation{
		verb:        invocationApply,
		contextName: "matrix",
		flags: invocationFlags{
			mode:            workflow.ApplyModeCreate,
			selection:       runSelection{stage: "add-ons", clusters: "ocp"},
			authorizations:  []string{authorizeForeignDaemons},
			yes:             true,
			askBecomePass:   false,
			trustOnFirstUse: true,
		},
	}
	command := mustRetry(t, invocation, retryIntent{mode: workflow.ApplyModeReconcile})
	message := applyModePreflightRefusal(preflightErr, invocation).Error()
	if !strings.Contains(message, "`"+command.String()+"`") {
		t.Fatalf("create refusal must name its exact reconcile retry:\n%s", message)
	}
	for _, want := range []string{"--mode reconcile", "--stage add-ons", "--clusters ocp", "--authorize foreign-daemons", "--context matrix"} {
		if !strings.Contains(command.String(), want) {
			t.Fatalf("create retry missing %q: %s", want, command.String())
		}
	}
	if err := workflow.EvaluateApplyModePreflight(workflow.ApplyModeReconcile, objects); err != nil {
		t.Fatalf("the exact retry mode must clear create's greenfield assertion: %v", err)
	}
}

func TestForeignApplyRefusalNamesOnlyTheSanctionedExternalRemedy(t *testing.T) {
	task := workflow.ApplyTask{Entry: workflow.TaskLedgerEntry{ID: "provider.foreign", Kind: workflow.ApplyTaskKindProvider, Label: "provider.foreign"}}
	runsDir := t.TempDir()
	if err := workflow.SaveConvergeSafetyRecord(runsDir, workflow.ConvergeSafetyRecord{
		APIVersion:   workflow.ConvergeSafetyAPIVersion,
		ResourceID:   workflow.ApplyTaskKindProvider + "/provider.foreign",
		ResourceKind: workflow.ApplyTaskKindProvider,
		TaskID:       task.Entry.ID,
		TaskKind:     task.Entry.Kind,
		DesiredHash:  "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		HashSchema:   workflow.ConvergeHashSchema,
		Owner:        workflow.ConvergeSafetyOwnerIdentity{Manager: "another-manager", Context: "test"},
		Status:       workflow.ConvergeSafetyStatusReconciled,
	}); err != nil {
		t.Fatal(err)
	}
	objects, err := workflow.ClassifyApplyObjects([]workflow.ApplyTask{task}, runsDir, "test")
	if err != nil {
		t.Fatal(err)
	}
	err = workflow.EvaluateApplyModePreflight(workflow.ApplyModeReconcile, objects)
	if err == nil {
		t.Fatal("foreign ownership must refuse")
	}
	invocation := resolvedInvocation{verb: invocationApply, contextName: "matrix", flags: invocationFlags{mode: workflow.ApplyModeReconcile, selection: runSelection{stage: "infra", clusters: "dc1-ocp"}}}
	message := applyModePreflightRefusal(err, invocation).Error()
	for _, want := range []string{"another manager", "use the recorded manager", "No bootwright apply mode or authorization token adopts"} {
		if !strings.Contains(message, want) {
			t.Fatalf("foreign refusal missing %q:\n%s", want, message)
		}
	}
	if strings.Contains(message, "re-run `bootwright apply") {
		t.Fatalf("foreign ownership has no sanctioned Bootwright bypass, got:\n%s", message)
	}
}

func mustRetry(t *testing.T, invocation resolvedInvocation, intent retryIntent) retryCommand {
	t.Helper()
	command, err := invocation.retry(intent)
	if err != nil {
		t.Fatalf("build retry command: %v", err)
	}
	return command
}

func assertRetryParses(t *testing.T, command retryCommand, inspect func(*cobra.Command)) {
	t.Helper()
	root := newRootCmd(bytes.NewReader(nil), &bytes.Buffer{}, &bytes.Buffer{})
	root.PersistentPreRunE = nil
	verb, _, err := root.Find(command.Args()[1:])
	if err != nil {
		t.Fatalf("find retry command: %v", err)
	}
	verb.RunE = func(cmd *cobra.Command, _ []string) error {
		inspect(cmd)
		return nil
	}
	root.SetArgs(command.Args()[1:])
	if err := root.Execute(); err != nil {
		t.Fatalf("parse exact retry command %q: %v", command.String(), err)
	}
}

func assertParsedFlag(t *testing.T, cmd *cobra.Command, name, want string) {
	t.Helper()
	flag := cmd.Flag(name)
	if flag == nil {
		t.Fatalf("parsed command %s has no --%s", cmd.CommandPath(), name)
	}
	if got := flag.Value.String(); got != want {
		t.Fatalf("parsed --%s = %q, want %q", name, got, want)
	}
}
