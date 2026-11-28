//go:build linux && amd64

package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate/kubevirt"
)

const (
	unrealizedSpec = "../../specs/api.md"
	hostVariants   = "libvirt-3-nodes-ceph-host-variants"
)

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

const unrealizedAddOnHead = `apiVersion: bootwright.io/v1alpha1
kind: ClusterAddon
metadata:
  name: extra
spec:
`

const unrealizedAddOnOLM = `  olm:
    namespace:
      name: extra-operator
%s
    operatorGroup: {}
    catalogSource:
      name: extra-catalog
      image: registry.example.test/extra/catalog:v1
      priority: -200
    subscription:
      package: extra
      channel: stable
      startingCSV: extra.v1.0.0
`

// addOnSources binds one ClusterAddon, written beside the lab-sno-nmstate
// example, to that example's cluster.
func addOnSources(t *testing.T, definition string, files map[string]string) desiredstate.Sources {
	t.Helper()
	files["add-ons/extra/add-on.yaml"] = definition
	return patchedSources(t, exampleDirectory(t, "lab-sno-nmstate"), "add-ons/binding.yaml", "  packages:\n", "  addonRefs:\n    - extra\n\n  packages:\n", files)
}

// unrealizedFixtures are admitted graphs that, between them, declare every
// row of the table: the admissible projection declares most, and an edited
// example each of the rest.
func unrealizedFixtures(t *testing.T) map[string]desiredstate.Sources {
	const cluster = "clusters/sno/cluster.yaml"
	return map[string]desiredstate.Sources{
		"the admissible projection": admissibleExampleSources(t, true),
		"an enabled playbook": patchedSources(t, admissibleExampleSources(t, true), "extras.yaml",
			"      - ceph-multidc-01\n\n  enabled: false\n", "      - ceph-multidc-01\n\n  enabled: true\n", nil),
		"a disconnected cluster": patchedSources(t, patchedSources(t, exampleDirectory(t, "lab-features"), "clusters/features/cluster.yaml",
			"    mode: connected\n", "    mode: disconnected\n", nil), "clusters/features/cluster.yaml", "          endpointRef: ip-https\n\n    # pullSecretRef",
			"          endpointRef: ip-https\n\n      bootArtifacts:\n        artifactServerEndpoint:\n          serverRef: lab-artifacts\n          endpointRef: ip-https\n\n    # pullSecretRef", nil),
		"a pinned libvirt release": patchedSources(t, exampleDirectory(t, hostVariants), "environment.yaml",
			"  controller:\n    machineRef: controller\n", "  controller:\n    machineRef: controller\n\n  dependencyVersions:\n    libvirt: 10.0.0\n", nil),
		"a manifest-set add-on": addOnSources(t, unrealizedAddOnHead+"  manifestSet:\n    manifests:\n      - path: manifests/extra.yaml\n", map[string]string{}),
		"an external namespace": addOnSources(t, unrealizedAddOnHead+fmt.Sprintf(unrealizedAddOnOLM, "      management: external"), map[string]string{}),
		"an add-on with steps":  addOnSources(t, unrealizedAddOnHead+fmt.Sprintf(unrealizedAddOnOLM, "")+"  steps:\n    - name: extra\n      gates: apply\n      manifests:\n        - path: manifests/extra.yaml\n", map[string]string{}),
		"serving certificates": patchedSources(t, exampleDirectory(t, "lab-sno"), cluster, "    mode: connected\n",
			"    mode: connected\n\n    servingCertificates:\n      ingress:\n        defaultCertificateRef: artifact-server-tls\n", nil),
	}
}

// refusesDeclaration says whether a plan refusal names the declaring object,
// the Machine whose install profile is, or subscribes to, the object, or the
// binding that selects the ClusterAddon.
func refusesDeclaration(refusal lifecycle.Refusal, object api.Object, catalog api.Catalog) bool {
	if refusal.Kind == string(object.Kind()) && refusal.Name == object.Name() {
		return true
	}
	if object.Kind() == api.ClusterAddon {
		binding, found := catalog.Find(api.ClusterAddonBinding, refusal.Name)
		if refusal.Kind != string(api.ClusterAddonBinding) || !found {
			return false
		}
		selected, _ := addons.ExpandBinding(binding, catalog)
		return slices.ContainsFunc(selected, func(addon api.Object) bool { return addon.Name() == object.Name() })
	}
	machine, found := catalog.Find(api.Machine, refusal.Name)
	if refusal.Kind != string(api.Machine) || !found {
		return false
	}
	profileName := api.MachineInstallProfileRef(machine)
	switch object.Kind() {
	case api.MachineInstallProfile:
		return profileName == object.Name()
	case api.Entitlement:
		profile, found := catalog.Find(api.MachineInstallProfile, profileName)
		return found && profile.Spec().Get("subscription", "entitlementRef").Text() == object.Name()
	}
	return false
}

// TestUnrealizedTableMatchesUnsupported holds the Admitted but unrealized
// table of specs/api.md to the data validate warns from and to what planning
// refuses: every row is the data, every declaration a fixture makes is warned
// api.unrealized at its path and refused plan.object-unsupported before
// registration, and every row is declared by some fixture.
func TestUnrealizedTableMatchesUnsupported(t *testing.T) {
	t.Parallel()
	var want, got [][]string
	for _, declaration := range api.UnrealizedDeclarations() {
		if declaration.Item == "" {
			t.Fatalf("%s %s has no tracking item", declaration.Kind, declaration.Declaration())
		}
		item := "[" + declaration.Item + "](milestones/" + milestonePage(t, declaration.Item) + "#" + strings.ToLower(declaration.Item) + ")"
		want = append(want, []string{string(declaration.Kind), "`" + declaration.Declaration() + "`", item})
	}
	for _, row := range markdownTable(t, unrealizedSpec, "### Admitted but unrealized", []string{"Kind", "Declaration", "Tracked by"}) {
		got = append(got, []string{row["Kind"], row["Declaration"], row["Tracked by"]})
	}
	if !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("api.md Admitted but unrealized rows = %q, want %q", got, want)
	}
	resolver := buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{})
	declared := map[string]bool{}
	for name, sources := range unrealizedFixtures(t) {
		state, report := compileAcceptance(t, sources)
		refusals := lifecycle.Unsupported(resolver, state, effectBearing)
		for _, declaration := range api.UnrealizedDeclarations() {
			for _, object := range state.Effective().Objects() {
				if !declaration.DeclaredBy(object) {
					continue
				}
				declared[declaration.Declaration()+string(declaration.Kind)] = true
				if !slices.ContainsFunc(report.Advisories, func(d diagnostics.Diagnostic) bool {
					return d.Code == "api.unrealized" && d.Severity == "warning" && d.Field == declaration.Path && d.Object != nil && d.Object.Name == object.Name() && strings.Contains(d.Message, "tracked as "+declaration.Item)
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
	dir := filepath.Join("..", "..", "specs", "milestones")
	pages := []string{"backlog.md"}
	for _, row := range markdownLines(t, filepath.Join("..", "..", "specs", "milestones.md")) {
		if !strings.HasPrefix(row, "| [M") {
			continue
		}
		_, tail, linked := strings.Cut(row, "](milestones/")
		if page, _, closed := strings.Cut(tail, ")"); linked && closed {
			pages = append(pages, page)
		}
	}
	for _, page := range pages {
		for _, line := range markdownLines(t, filepath.Join(dir, page)) {
			if line == "### "+item {
				return page
			}
		}
	}
	t.Fatalf("no milestone page defines %s", item)
	return ""
}

// Planning claims each managed Network through the host block of the provider
// it names, which defines it.
func TestPlanningClaimsEveryManagedNetworkThroughItsProviderHost(t *testing.T) {
	t.Parallel()
	state, _ := compileAcceptance(t, exampleDirectory(t, "lab-networks"))
	resolver := buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{})
	for _, refusal := range lifecycle.Unsupported(resolver, state, effectBearing) {
		if refusal.Kind == string(api.Network) {
			t.Errorf("planning refuses %s/%s: %s", refusal.Kind, refusal.Name, refusal.Reason)
		}
	}
	managed := 0
	for _, network := range state.Effective().OfKind(api.Network) {
		if _, bears := effectBearing(network); bears {
			managed++
		}
	}
	if managed != 3 {
		t.Fatalf("lab-networks declares %d effect-bearing Networks, want its 3 managed ones", managed)
	}
}

func TestKubeVirtTemplateSnapshotCompilesWithoutACloneEffect(t *testing.T) {
	t.Parallel()
	const providerFile = "infra/providers/lab-kubevirt.yaml"
	const snapshotField = "$.spec.kubevirt.machineProfiles[0].templateSnapshotRef"
	const source = `        templateSnapshotRef:
          namespace: golden-images
          name: rhel-9.8-v3
          uid: 1cf3091a-6642-4687-bd20-086ef84d37b6
`
	const installProfile = `apiVersion: bootwright.io/v1alpha1
kind: MachineInstallProfile
metadata: {name: rhel-clone}
spec:
  os: {family: rhel, version: "9.8", architecture: x86_64}
  installer:
    templateClone:
      seed: {cloudInit: {}}
  updates: {policy: none}
`
	sources := patchedSources(t, exampleDirectory(t, "lab-kubevirt-external"), "environment.yaml",
		"    machineRef: controller\n", "    machineRef: controller\n\n  remoteMachinesAccessKey:\n    keyRef: kubevirt-cluster-admin-ssh-key\n", nil)
	sources.Files = slices.DeleteFunc(sources.Files, func(file desiredstate.SourceFile) bool {
		path := file.Path()
		return strings.HasSuffix(path, "clusters/kubevirt/cluster.yaml") ||
			strings.HasSuffix(path, "infra/machines/kubevirt-02.yaml") ||
			strings.HasSuffix(path, "infra/machines/kubevirt-03.yaml")
	})
	sources = patchedSources(t, sources, "infra/machines/kubevirt-01.yaml",
		"    clusterInstalled: {}", "    install: {profileRef: rhel-clone}", map[string]string{"infra/profiles/rhel-clone.yaml": installProfile})
	trustFailure(t, sources, "api.invariant", "$.spec.installer.templateClone")
	complete := patchedSources(t, sources, providerFile, "        diskGiB: 120\n", "        diskGiB: 120\n"+source, nil)
	trustFailure(t, complete, "api.invariant", "$.spec.os.install.hostKeyRef")
	complete = patchedSources(t, complete, "infra/machines/kubevirt-01.yaml", "install: {profileRef: rhel-clone}",
		"install: {profileRef: rhel-clone, hostKeyRef: clone-host-key}", map[string]string{
			"secret-descriptors/clone-host-key.yaml": "apiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata: {name: clone-host-key}\nspec: {type: sshKeyPair}\n",
		})
	state, _ := compileAcceptance(t, complete)
	arm := requireObject(t, state.Effective(), api.InfraProvider, "lab-kubevirt").Spec().Get("kubevirt")
	snapshot := arm.Get("machineProfiles").Items()[0].Get("templateSnapshotRef")
	if arm.Get("namespace").Text() != "bootwright-guests" || snapshot.Get("namespace").Text() != "golden-images" || snapshot.Get("uid").Text() != "1cf3091a-6642-4687-bd20-086ef84d37b6" {
		t.Fatal("source namespace or revision was lost during compilation", arm)
	}
	resolver := buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{})
	refusals := lifecycle.Unsupported(resolver, state, effectBearing)
	if !slices.ContainsFunc(refusals, func(item lifecycle.Refusal) bool {
		return item.Kind == "Machine" && item.Name == "kubevirt-01" && strings.Contains(item.Remediation, "B1166")
	}) {
		t.Fatalf("provisioning failed to refuse the unrealized source before registration: %+v", refusals)
	}
	machines, err := kubevirt.MachineRequests(state.Effective(), "controller", "template-test")
	if err != nil || len(machines) != 1 || machines[0].TemplateSnapshot == nil {
		t.Fatalf("request generation lost the snapshot intent: %+v, %v", machines, err)
	}
	providers, err := kubevirt.HostRequests(state.Effective(), "controller", "template-test")
	if err != nil || len(providers) != 1 || !slices.Equal(providers[0].TemplateSnapshots, []kubevirt.SnapshotRef{*machines[0].TemplateSnapshot}) {
		t.Fatalf("provider did not freeze the source selected by its machine: %+v, %v", providers, err)
	}
	incomplete := patchedSources(t, complete, providerFile, "          uid: 1cf3091a-6642-4687-bd20-086ef84d37b6\n", "", nil)
	trustFailure(t, incomplete, "api.required", snapshotField+".uid")
}
