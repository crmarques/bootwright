package cli

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/clusteraccess"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func TestWarnUnusedAuthorizations(t *testing.T) {
	cases := []struct {
		name   string
		tokens []string
		noted  []string
		dryRun bool
		want   string
	}{
		{"real no-op names the token", []string{authorizeDataLoss}, nil, false, "--authorize data-loss had no effect"},
		{"dry-run", []string{authorizeDataLoss}, nil, true, "not consumed by a dry-run"},
		{"consumed stays silent", []string{authorizeDataLoss}, []string{authorizeDataLoss}, false, ""},
		{"none given stays silent", nil, nil, false, ""},
		{"all discloses the token it stood in for", []string{authorizeAll}, []string{authorizeUnownedVMs}, false, "stood in for unowned-vms"},
		{"all consumed by no gate names itself", []string{authorizeAll}, nil, false, "--authorize all had no effect"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			auth, err := parseAuthorizations(tc.tokens, authorizeVerbDestroy)
			if err != nil {
				t.Fatal(err)
			}
			for _, token := range tc.noted {
				auth.note(token)
			}
			var out bytes.Buffer
			warnUnusedAuthorizations(&out, auth, tc.dryRun)
			if tc.want == "" {
				if out.Len() != 0 {
					t.Fatalf("expected no notice, got %q", out.String())
				}
				return
			}
			if !strings.Contains(out.String(), tc.want) {
				t.Fatalf("notice must contain %q, got %q", tc.want, out.String())
			}
		})
	}
}

func TestParseAuthorizationsRejectsUnknownToken(t *testing.T) {
	if _, err := parseAuthorizations([]string{"force"}, authorizeVerbDestroy); err == nil {
		t.Fatal("an unknown --authorize token must be rejected")
	} else if !strings.Contains(err.Error(), authorizeDataLoss) {
		t.Fatalf("the rejection must list the valid tokens, got %q", err.Error())
	}
	auth, err := parseAuthorizations([]string{authorizeDataLoss, authorizeProtected, authorizeDataLoss}, authorizeVerbDestroy)
	if err != nil {
		t.Fatal(err)
	}
	if !auth.has(authorizeDataLoss) || !auth.has(authorizeProtected) || auth.has(authorizeSharedInfra) {
		t.Fatalf("parsed set must hold exactly the given tokens, got %v", auth.all())
	}
	if len(auth.all()) != 2 {
		t.Fatalf("a repeated token must be recorded once, got %v", auth.all())
	}
}

func TestBlanketAuthorizationExpandsOnlyWithinItsVerb(t *testing.T) {
	for _, verb := range []string{authorizeVerbApply, authorizeVerbDestroy} {
		auth, err := parseAuthorizations([]string{authorizeAll}, verb)
		if err != nil {
			t.Fatal(err)
		}
		accepted := authorizationTokenNamesForVerb(verb)
		for _, name := range authorizationTokenNames() {
			want := slices.Contains(accepted, name)
			if got := auth.has(name); got != want {
				t.Errorf("--authorize all on %s answers %v for %s, want %v; the blanket token is exactly the set the verb accepts, never wider and never narrower", verb, got, name, want)
			}
		}
		if auth.has("force") {
			t.Errorf("--authorize all on %s must not answer for a name that is not a registered token", verb)
		}
	}
}

func TestBlanketAuthorizationCreditsOnlyTheTokensItSupplied(t *testing.T) {
	auth, err := parseAuthorizations([]string{authorizeAll, authorizeDataLoss}, authorizeVerbDestroy)
	if err != nil {
		t.Fatal(err)
	}
	auth.note(authorizeDataLoss)
	if got := auth.expandedByAll(); len(got) != 0 {
		t.Fatalf("a token named on the command line is not one all stood in for, got %v", got)
	}
	if len(auth.unused()) != 1 || auth.unused()[0] != authorizeAll {
		t.Fatalf("all must still report having had no effect when every consumed gate was named explicitly, got %v", auth.unused())
	}
	auth.note(authorizeSharedInfra)
	if got := auth.expandedByAll(); len(got) != 1 || got[0] != authorizeSharedInfra {
		t.Fatalf("all must be credited with exactly the unnamed gates it answered, got %v", got)
	}
	if len(auth.unused()) != 0 {
		t.Fatalf("all became effective, so it must no longer be reported as unused, got %v", auth.unused())
	}
}

func TestPrintApplyTransitionLedgerPromotesReinstall(t *testing.T) {
	state := loadFixtureState(t, "001-sno-libvirt")
	tasks := planApplyTasks(t, converge.AllScope.ApplyTarget(), state)
	runsDir := t.TempDir()

	var out bytes.Buffer
	printApplyTransitionLedger(&out, tasks, runsDir, workflow.ApplyModeRebuild, []string{"sno-libvirt"})
	got := out.String()
	if !strings.Contains(got, "DESTROY & rebuild") || !strings.Contains(got, "ContainerCluster/sno-libvirt") {
		t.Fatalf("a reinstall-input-drifted cluster must show under DESTROY & rebuild, got %q", got)
	}

	out.Reset()
	printApplyTransitionLedger(&out, tasks, runsDir, workflow.ApplyModeRebuild, nil)
	if strings.Contains(out.String(), "DESTROY & rebuild") {
		t.Fatalf("without a reinstall flag the cluster must not show DESTROY & rebuild, got %q", out.String())
	}
}

func TestPrintApplyGateForecastReinstallRefusal(t *testing.T) {
	state := loadFixtureState(t, "001-sno-libvirt")
	state.Environments = append(state.Environments, v1alpha1.Environment{
		Metadata: v1alpha1.Metadata{Name: "guard"},
		Spec:     v1alpha1.EnvironmentSpec{Safety: v1alpha1.EnvironmentSafetySpec{ProtectedKinds: []string{v1alpha1.KindContainerCluster}}},
	})
	tasks := planApplyTasks(t, converge.AllScope.ApplyTarget(), state)
	runsDir := t.TempDir()

	var out bytes.Buffer
	printApplyGateForecast(&out, state, state, tasks, runsDir, t.TempDir(), workflow.ApplyModeRebuild, false, "", clusteraccess.Selection{}, []string{"sno-libvirt"}, t.TempDir(), nil, nil)
	got := out.String()
	if !strings.Contains(got, "a real run refuses before any prompt") || !strings.Contains(got, "ContainerCluster/sno-libvirt") {
		t.Fatalf("forecast must reproduce the protectedKinds reinstall refusal for a drifted cluster, got %q", got)
	}

	out.Reset()
	printApplyGateForecast(&out, state, state, tasks, runsDir, t.TempDir(), workflow.ApplyModeRebuild, false, "", clusteraccess.Selection{}, nil, t.TempDir(), nil, nil)
	if strings.Contains(out.String(), "a real run refuses before any prompt") {
		t.Fatalf("with no reinstall drift the forecast must not raise the protection refusal, got %q", out.String())
	}
}

func TestPrintApplyGateForecastNamesUnreadableOwnershipRecords(t *testing.T) {
	state := loadFixtureState(t, "001-sno-libvirt")
	tasks := planApplyTasks(t, converge.AllScope.ApplyTarget(), state)
	ownershipDir := t.TempDir()

	var out bytes.Buffer
	printApplyGateForecast(&out, state, state, tasks, t.TempDir(), t.TempDir(), workflow.ApplyModeReconcile, false, "", clusteraccess.Selection{}, nil, ownershipDir, nil, []error{errors.New("decode resources/corrupt.json: invalid character 'n'")})
	got := out.String()
	if !strings.Contains(got, "a real run refuses before any prompt") || !strings.Contains(got, "could not be read") {
		t.Fatalf("forecast must name the unreadable-record refusal its real run makes, got %q", got)
	}
	if !strings.Contains(got, "corrupt.json") {
		t.Fatalf("forecast must name the record file to repair, got %q", got)
	}
}

func TestCheckKubeVirtTenantRebuildScopeGatesEverySelectionAxis(t *testing.T) {
	state := loadFixtureState(t, "001-sno-libvirt")
	state.ContainerClusters = append(state.ContainerClusters, v1alpha1.ContainerCluster{Metadata: v1alpha1.Metadata{Name: "child"}})
	clustersDir := t.TempDir()
	now := time.Now().UTC()
	if err := workflow.SaveClusterInstallRecord(clustersDir, workflow.ClusterInstallRecord{
		Cluster: "child", Status: workflow.ClusterInstallStatusInstalled,
		Phase: workflow.ClusterInstallPhaseComplete, UpdatedAt: now, InstalledAt: &now,
	}); err != nil {
		t.Fatalf("SaveClusterInstallRecord: %v", err)
	}
	host := "sno-libvirt"
	rebuilt := []string{host}
	tenantState := kubeVirtTenantState(state, host)

	cases := []struct {
		name       string
		sel        clusteraccess.Selection
		wantRefuse bool
	}{
		{name: "cluster selection excluding the guest refuses", sel: clusteraccess.Selection{Active: true, ContainerRoots: []string{host}, AllRoots: []string{host}}, wantRefuse: true},
		{name: "machine selection excluding the guest refuses", sel: clusteraccess.Selection{Active: true, MachineSelection: true, ContainerRoots: []string{host}, AllRoots: []string{host}}, wantRefuse: true},
		{name: "selection including the guest proceeds", sel: clusteraccess.Selection{Active: true, ContainerRoots: []string{host, "child"}, AllRoots: []string{host, "child"}}},
		{name: "unscoped run covers every cluster and proceeds", sel: clusteraccess.Selection{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkKubeVirtTenantRebuildScope(tenantState, clustersDir, tc.sel, rebuilt, nil)
			if tc.wantRefuse {
				if err == nil {
					t.Fatal("a scoped rebuild of the host must refuse while the nested cluster is left out of the selection")
				}
				if !strings.Contains(err.Error(), "child") {
					t.Fatalf("refusal must name the stranded nested cluster, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("selection covering the nested cluster must proceed: %v", err)
			}
		})
	}
}

func kubeVirtTenantState(state v1alpha1.State, host string) v1alpha1.State {
	out := state
	out.InfraProviders = append(append([]v1alpha1.InfraProvider(nil), state.InfraProviders...), v1alpha1.InfraProvider{
		Metadata: v1alpha1.Metadata{Name: "kubevirt-host"},
		Spec: v1alpha1.InfraProviderSpec{
			Type:     v1alpha1.ProvisionerKubeVirt,
			KubeVirt: &v1alpha1.InfraProviderKubeVirt{HostClusterRef: &v1alpha1.LocalObjectReference{Name: host}},
		},
	})
	out.Machines = append(append([]v1alpha1.Machine(nil), state.Machines...), v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: "child-node-0"},
		Spec: v1alpha1.MachineSpec{
			Substrate: v1alpha1.MachineSubstrate{ProviderRef: v1alpha1.LocalObjectReference{Name: "kubevirt-host"}},
		},
	})
	clusters := append([]v1alpha1.ContainerCluster(nil), state.ContainerClusters...)
	for i := range clusters {
		if clusters[i].Metadata.Name != "child" {
			continue
		}
		clusters[i].Spec.Nodes = []v1alpha1.OCPNodeSpec{{Name: "master-0", MachineRef: v1alpha1.LocalObjectReference{Name: "child-node-0"}}}
	}
	out.ContainerClusters = clusters
	return out
}

func TestPrintArtifactServerReclaimNotice(t *testing.T) {
	var out bytes.Buffer
	printArtifactServerReclaimNotice(&out, []string{"InfraComponent-artifact"})
	got := out.String()
	if !strings.Contains(got, "reclaim install-only artifact server") || !strings.Contains(got, "InfraComponent-artifact") {
		t.Fatalf("reclaim notice must name the server, got %q", got)
	}
	out.Reset()
	printArtifactServerReclaimNotice(&out, nil)
	if out.Len() != 0 {
		t.Fatalf("no reclaim targets must print nothing, got %q", out.String())
	}
}
