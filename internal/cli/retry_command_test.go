package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
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
		mode:            workflow.ApplyModeReconcile,
		selection:       runSelection{stage: "deps", through: "base", clusters: "dc1-ocp,ceph-storage"},
		reclaimDevices:  "all",
		authorizations:  []string{authorizeForeignDaemons},
		yes:             true,
		askBecomePass:   false,
		trustOnFirstUse: false,
		verbose:         true,
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
		mode:            workflow.ApplyModeRebuild,
		selection:       runSelection{stage: "deps", through: "base", clusters: "ocp"},
		reclaimDevices:  "/dev/sdb",
		authorizations:  []string{authorizeDataLoss},
		dryRun:          true,
		output:          outputJSON,
		yes:             true,
		askBecomePass:   true,
		trustOnFirstUse: false,
		verbose:         true,
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
		DesiredHash:  "sha256:stale",
		HashSchema:   workflow.ConvergeHashSchema,
		Owner:        workflow.ConvergeSafetyOwnerIdentity{Manager: workflow.ConvergeSafetyOwner},
	}); err != nil {
		t.Fatal(err)
	}
	objects, err := workflow.ClassifyApplyObjects([]workflow.ApplyTask{task}, runsDir)
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
		Owner:        workflow.ConvergeSafetyOwnerIdentity{Manager: workflow.ConvergeSafetyOwner},
	}); err != nil {
		t.Fatal(err)
	}
	objects, err := workflow.ClassifyApplyObjects([]workflow.ApplyTask{task}, runsDir)
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
		DesiredHash:  "sha256:foreign",
		HashSchema:   workflow.ConvergeHashSchema,
		Owner:        workflow.ConvergeSafetyOwnerIdentity{Manager: "another-manager"},
	}); err != nil {
		t.Fatal(err)
	}
	objects, err := workflow.ClassifyApplyObjects([]workflow.ApplyTask{task}, runsDir)
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
