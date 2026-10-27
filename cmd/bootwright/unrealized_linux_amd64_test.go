//go:build linux && amd64

package main

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const unrealizedSpec = "../../specs/api.md"

// patchedSources rewrites the one source file whose path ends with suffix,
// replacing old, which it must hold exactly once, and adds files under the
// first root.
func patchedSources(t *testing.T, sources desiredstate.Sources, suffix, old, replacement string, added map[string]string) desiredstate.Sources {
	t.Helper()
	if suffix != "" {
		index := slices.IndexFunc(sources.Files, func(file desiredstate.SourceFile) bool { return strings.HasSuffix(file.Path(), suffix) })
		if index < 0 || strings.Count(string(sources.Files[index].Bytes()), old) != 1 {
			t.Fatalf("%s does not hold %q exactly once", suffix, old)
		}
		sources.Files[index] = desiredstate.NewSourceFile(sources.Files[index].Path(), []byte(strings.Replace(string(sources.Files[index].Bytes()), old, replacement, 1)))
	}
	for name, body := range added {
		sources.Files = append(sources.Files, desiredstate.NewSourceFile(filepath.Join(sources.Roots[0], name), []byte(body)))
	}
	return sources
}

// unrealizedFixtures are admitted graphs that, between them, declare every
// row of the table: the admissible projection declares most, and an edited
// example each of the rest.
func unrealizedFixtures(t *testing.T) map[string]desiredstate.Sources {
	const profile, server, cluster = "infra/os/rhel-9-8.yaml", "infra/components/artifact-server.yaml", "clusters/sno/cluster.yaml"
	hostedTree := "      packageSource:\n        hostedTree:\n          fromMedia: local-media:rhel-9.8-x86_64-dvd.iso\n\n          artifactServerEndpoint:\n            serverRef: lab-artifacts\n            endpointRef: ip-http\n"
	secret := func(name, kind string) string {
		return "apiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata:\n  name: " + name + "\n\nspec:\n  type: " + kind + "\n"
	}
	return map[string]desiredstate.Sources{
		"the admissible projection": admissibleExampleSources(t, true),
		"an enabled playbook": patchedSources(t, admissibleExampleSources(t, true), "extras.yaml",
			"      - ceph-multidc-01\n\n  enabled: false\n", "      - ceph-multidc-01\n\n  enabled: true\n", nil),
		"a package mirror": patchedSources(t, exampleDirectory(t, "lab-rhel"), profile, hostedTree,
			"      packageSource:\n        mirror:\n          baseURL: https://mirror.lab.example.test/rhel\n", nil),
		"packages from a subscription": patchedSources(t, exampleDirectory(t, "lab-rhel"), profile, hostedTree,
			"      packageSource:\n        fromSubscription:\n          entitlementRef: rhel\n",
			map[string]string{"rhel-entitlement.yaml": "apiVersion: bootwright.io/v1alpha1\nkind: Entitlement\nmetadata:\n  name: rhel\n\nspec:\n  type: redhatRhel\n\n  rhsm:\n    management: external\n"}),
		"an initial password": patchedSources(t, exampleDirectory(t, "lab-rhel"), profile, "      passwordAuthentication: false\n",
			"      passwordAuthentication: false\n\n      initialPassword:\n        secretRef: root-password\n",
			map[string]string{"root-password.yaml": secret("root-password", "usernamePassword")}),
		"installOnly retention": patchedSources(t, exampleDirectory(t, "lab-rhel"), server, "retention: persistent", "retention: installOnly", nil),
		"a disconnected cluster": patchedSources(t, patchedSources(t, exampleDirectory(t, "lab-features"), "clusters/features/cluster.yaml",
			"    mode: connected\n", "    mode: disconnected\n", nil), "clusters/features/cluster.yaml", "          endpointRef: ip-https\n\n    # pullSecretRef",
			"          endpointRef: ip-https\n\n      bootArtifacts:\n        artifactServerEndpoint:\n          serverRef: lab-artifacts\n          endpointRef: ip-https\n\n    # pullSecretRef", nil),
		"serving certificates": patchedSources(t, exampleDirectory(t, "lab-sno"), cluster, "    mode: connected\n",
			"    mode: connected\n\n    servingCertificates:\n      ingress:\n        defaultCertificateRef: artifact-server-tls\n", nil),
	}
}

// refusesDeclaration says whether a plan refusal names the declaring object,
// or the Machine an unrealized install profile would install.
func refusesDeclaration(refusal lifecycle.Refusal, object api.Object, catalog api.Catalog) bool {
	if refusal.Kind == string(object.Kind()) && refusal.Name == object.Name() {
		return true
	}
	machine, found := catalog.Find(api.Machine, refusal.Name)
	return refusal.Kind == string(api.Machine) && found && object.Kind() == api.MachineInstallProfile &&
		machine.Spec().Get("os", "installProfileRef").Text() == object.Name()
}

// TestUnrealizedTableMatchesUnsupported holds the Admitted but unrealized
// table of specs/api.md to the data validate warns from and to what planning
// refuses: every row is the data, every declaration a fixture makes is warned
// api.unrealized at its path and refused lifecycle.unsupported before
// registration, and every row is declared by some fixture.
func TestUnrealizedTableMatchesUnsupported(t *testing.T) {
	var want, got [][]string
	for _, declaration := range api.UnrealizedDeclarations() {
		item := "none tracked"
		if declaration.Item != "" {
			item = "[" + declaration.Item + "](milestones/" + milestonePage(t, declaration.Item) + "#" + strings.ToLower(declaration.Item) + ")"
		}
		want = append(want, []string{string(declaration.Kind), "`" + declaration.Declaration() + "`", item})
	}
	for _, row := range markdownTable(t, unrealizedSpec, "### Admitted but unrealized", []string{"Kind", "Declaration", "Tracked by"}) {
		got = append(got, []string{row["Kind"], row["Declaration"], row["Tracked by"]})
	}
	if !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("api.md Admitted but unrealized rows = %q, want %q", got, want)
	}
	resolver := buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{})
	var claimed []string
	for _, binding := range resolver.Bindings() {
		claimed = append(claimed, binding.Kind)
	}
	declared := map[string]bool{}
	for name, sources := range unrealizedFixtures(t) {
		state, report := compileAcceptance(t, sources)
		refusals := append(productionUnsupported(t, state), lifecycle.Unrealizable(state.Effective(), claimed, effectBearing)...)
		for _, declaration := range api.UnrealizedDeclarations() {
			for _, object := range state.Effective().Objects() {
				if !declaration.DeclaredBy(object) {
					continue
				}
				declared[declaration.Declaration()+string(declaration.Kind)] = true
				if !slices.ContainsFunc(report.Advisories, func(d diagnostics.Diagnostic) bool {
					return d.Code == "api.unrealized" && d.Severity == "warning" && d.Field == declaration.Path && d.Object != nil && d.Object.Name == object.Name()
				}) {
					t.Errorf("%s: %s declares %s without an api.unrealized advisory", name, object.Identity(), declaration.Declaration())
				}
				if !slices.ContainsFunc(refusals, func(refusal lifecycle.Refusal) bool { return refusesDeclaration(refusal, object, state.Effective()) }) {
					t.Errorf("%s: planning does not refuse %s, which declares %s", name, object.Identity(), declaration.Declaration())
				}
			}
		}
	}
	for _, declaration := range api.UnrealizedDeclarations() {
		if !declared[declaration.Declaration()+string(declaration.Kind)] {
			t.Errorf("no fixture declares %s %s", declaration.Kind, declaration.Declaration())
		}
	}
}

// milestonePage is the milestone page whose item table names item.
func milestonePage(t *testing.T, item string) string {
	t.Helper()
	for _, page := range []string{"m1.md", "m2.md", "m3.md", "m4.md", "m5.md", "backlog.md"} {
		for _, line := range markdownLines(t, filepath.Join("..", "..", "specs", "milestones", page)) {
			if line == "### "+item {
				return page
			}
		}
	}
	t.Fatalf("no milestone page defines %s", item)
	return ""
}
