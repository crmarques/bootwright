package cli

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/clusteraccess"
	"github.com/crmarques/bootwright/internal/converge"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
)

func TestDestroyStorageConsumerGateUsesConsequencesNotScopeName(t *testing.T) {
	ctx := initSafetyBaselineContext(t, safetyBaselineAdvanced)
	state, err := desiredstate.LoadNormalizeValidateInputFiles(ctx.InputPaths)
	if err != nil {
		t.Fatal(err)
	}
	sel, err := clusteraccess.Resolve(state, converge.InfraScope.Name, safetyAdvancedCephCluster)
	if err != nil {
		t.Fatal(err)
	}
	invocation := resolvedInvocation{
		verb:        invocationDestroy,
		contextName: ctx.Name,
		flags: invocationFlags{
			selection:     runSelection{stage: "infra", clusters: safetyAdvancedCephCluster},
			yes:           true,
			askBecomePass: false,
		},
	}
	type outcome struct {
		reached bool
		notice  string
		err     string
	}
	run := func(scope converge.Scope, tokens ...string) (outcome, *authorizations) {
		t.Helper()
		auth, parseErr := parseAuthorizations(tokens, authorizeVerbDestroy)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		reached, notice, gateErr := destroyStorageConsumerGate(auth, state, sel, scope, false, invocation)
		got := outcome{reached: reached, notice: notice}
		if gateErr != nil {
			got.err = gateErr.Error()
		}
		return got, auth
	}

	machine := converge.InfraScope
	futureMachine := machine
	futureMachine.Name = "future-machine-layer"
	wantMachine, _ := run(machine)
	gotMachine, _ := run(futureMachine)
	if gotMachine != wantMachine || !gotMachine.reached || !strings.Contains(gotMachine.err, "--authorize shared-infra") {
		t.Fatalf("future machine-layer scope outcome = %#v, want canonical consequence gate %#v", gotMachine, wantMachine)
	}
	wantAuthorizedMachine, _ := run(machine, authorizeSharedInfra)
	gotAuthorizedMachine, _ := run(futureMachine, authorizeSharedInfra)
	if gotAuthorizedMachine != wantAuthorizedMachine || gotAuthorizedMachine.err != "" || !strings.Contains(gotAuthorizedMachine.notice, "proceeding because --authorize shared-infra") {
		t.Fatalf("authorized future machine-layer scope outcome = %#v, want %#v", gotAuthorizedMachine, wantAuthorizedMachine)
	}

	cluster := converge.ClustersScope
	futureCluster := cluster
	futureCluster.Name = "future-cluster-layer"
	wantCluster, _ := run(cluster)
	gotCluster, _ := run(futureCluster)
	if gotCluster != wantCluster || gotCluster.reached || !strings.Contains(gotCluster.err, "still consumed") {
		t.Fatalf("future cluster-layer scope outcome = %#v, want canonical hard conflict %#v", gotCluster, wantCluster)
	}
	wantAuthorizedCluster, canonicalAuth := run(cluster, authorizeSharedInfra)
	gotAuthorizedCluster, futureAuth := run(futureCluster, authorizeSharedInfra)
	if gotAuthorizedCluster != wantAuthorizedCluster || gotAuthorizedCluster.err == "" {
		t.Fatalf("authorized future cluster-layer scope outcome = %#v, want hard conflict %#v", gotAuthorizedCluster, wantAuthorizedCluster)
	}
	if len(canonicalAuth.unused()) != 1 || len(futureAuth.unused()) != 1 {
		t.Fatalf("cluster-layer conflict must not consume shared-infra: canonical=%v future=%v", canonicalAuth.unused(), futureAuth.unused())
	}
}
