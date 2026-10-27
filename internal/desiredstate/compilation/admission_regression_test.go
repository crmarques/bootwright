package compilation_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/desiredstate/yamlstream"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/environment"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/storage/cephcluster"
	"github.com/crmarques/bootwright/internal/substrate"
)

func environmentSelection(catalog api.Catalog) compilation.Selection {
	selected := environment.Select(catalog, nil, testClusterMembers)
	result := compilation.Selection{Catalog: selected.Catalog, ExcludedContainerClusters: selected.ExcludedContainerClusters, ExcludedStorageClusters: selected.ExcludedStorageClusters, Excluded: selected.Excluded}
	for _, problem := range selected.Problems {
		result.Problems = append(result.Problems, compilation.ObjectIssue{Object: problem.Object, Issue: problem.Issue, Target: problem.Target})
	}
	return result
}

func regressionCompiler() compilation.Compiler {
	return compilation.NewCompiler(yamlstream.Parser{}, environmentSelection,
		compilation.Rules{Normalize: environment.Normalize, Validate: testEnvironmentValidate},
		compilation.Rules{Normalize: infrastructureservices.Normalize, ValidateAuthored: infrastructureservices.ValidateAuthored, ValidatePartial: infrastructureservices.ValidatePartial, Validate: infrastructureservices.Validate},
		compilation.Rules{Normalize: secrets.Normalize, ValidateAuthored: secrets.ValidateAuthored, ValidatePartial: secrets.ValidatePartial, Validate: secrets.Validate},
		compilation.Rules{RetiredField: cephcluster.RetiredField, Normalize: cephcluster.Normalize, ValidateAuthored: cephcluster.ValidateAuthored, ValidatePartial: cephcluster.ValidatePartial, Validate: cephcluster.Validate},
		compilation.Rules{Normalize: substrate.Normalize, ValidateAuthored: substrate.ValidateAuthored, ValidatePartial: substrate.ValidatePartial, Validate: substrate.Validate})
}

func requireCompilationFailure(t *testing.T, state *compilation.State, report *compilation.Report, err error) []diagnostics.Diagnostic {
	t.Helper()
	if state != nil || report != nil || err == nil {
		t.Fatalf("failed admission exposed a result: state=%v report=%v err=%v", state, report, err)
	}
	sink := diagnostics.Of(err)
	if len(sink) == 0 {
		t.Fatalf("admission failure has no typed diagnostics: %v", err)
	}
	for i := 1; i < len(sink); i++ {
		if diagnostics.Compare(sink[i-1], sink[i]) > 0 {
			t.Fatal("admission diagnostics are not canonically ordered")
		}
	}
	return sink
}

func TestUnusedDefaultsRejectPresentContradictions(t *testing.T) {
	for _, test := range []struct{ name, fields, field string }{
		{"two protection arms", "  defaults:\n    CephPool:\n      replicated: {size: 2}\n      erasure: {dataChunks: 2, codingChunks: 1}\n", "$.spec.defaults.CephPool"},
		{"secret parameters", "  defaults:\n    Secret:\n      type: token\n      source: {generated: {username: alice}}\n", "$.spec.defaults.Secret.source.generated.username"},
		{"external service placement", "  defaults:\n    Proxy:\n      management: external\n      machineRef: host\n", "$.spec.defaults.Proxy.machineRef"},
		{"storage range", "  defaults:\n    CephPool:\n      compression: {minBlobSize: 20, maxBlobSize: 10}\n", "$.spec.defaults.CephPool.compression"},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, report, err := regressionCompiler().Compile(context.Background(), sources(environmentYAML+test.fields))
			sink := requireCompilationFailure(t, state, report, err)
			for _, d := range sink {
				if d.Code == "api.invariant" && d.Field == test.field && d.Object != nil && d.Object.Kind == string(api.Environment) {
					return
				}
			}
			t.Fatalf("missing partial-default diagnostic for %s: %#v", test.field, sink)
		})
	}
}

func TestUnusedDefaultsPermitMissingRequiredArmsAndReferences(t *testing.T) {
	input := sources(environmentYAML + "  defaults:\n    InfraProvider:\n      baremetal: {}\n      networkAttachments: [{name: network}]\n    CephPool:\n      clusterRef: future\n      compression: {minBlobSize: 20}\n    Secret:\n      source: {generated: {bytes: 32}}\n")
	state, report, err := regressionCompiler().Compile(context.Background(), input)
	if err != nil || report.Counts.ObjectsDecoded != 2 || len(state.Effective().Objects()) != 2 {
		t.Fatalf("partial defaults were completed or required a recipient: state=%v report=%v err=%v", state, report, err)
	}
	env, _ := state.Effective().Find(api.Environment, "synthetic")
	partial := env.Spec().Get("defaults", "InfraProvider")
	if partial.Has("baremetal", "defaults") || partial.Get("networkAttachments").Items()[0].Has("baremetal") {
		t.Fatal("unused partial defaults acquired built-in fields")
	}
}

func TestSelectedDuplicateRootsRemainAmbiguousAndEachReceiveDiagnostics(t *testing.T) {
	cluster := "apiVersion: bootwright.io/v1alpha1\nkind: CephCluster\nmetadata: {name: storage}\nspec: {management: external}\n"
	input := sources(environmentYAML + "  storageClusters: [storage]\n---\n" + cluster + "---\n" + cluster)
	state, report, err := regressionCompiler().Compile(context.Background(), input)
	sink := requireCompilationFailure(t, state, report, err)
	documents := map[int]bool{}
	selectionFailed := false
	for _, d := range sink {
		if d.Code == "api.reference" && d.Field == "$.spec.storageClusters[0]" {
			selectionFailed = true
		}
		if d.Code == "api.duplicate" && d.Object != nil && d.Object.Kind == string(api.CephCluster) && d.Object.Name == "storage" && d.Source != nil {
			documents[d.Source.Document] = true
		}
		if d.Severity == "warning" {
			t.Fatal("a requested ambiguous root was mislabeled as excluded", d)
		}
	}
	if selectionFailed || !reflect.DeepEqual(documents, map[int]bool{2: true, 3: true}) {
		t.Fatalf("duplicate identity evidence was lost or repeated at the selection entry: %#v", sink)
	}
}

func TestEnvironmentSelectionUsesBootwrightAPIVersion(t *testing.T) {
	native := desiredstate.NewSourceFile("/synthetic/native.yaml", []byte("apiVersion: other.example/v1\nkind: Environment\nmetadata: {name: native}\nspec: {}\n"))
	input := sources(environmentYAML)
	input.Files = append(input.Files, native)
	state, report, err := regressionCompiler().Compile(context.Background(), input)
	sink := requireCompilationFailure(t, state, report, err)
	if len(sink) != 1 || sink[0].Code != "api.version" || sink[0].Source.Path != native.Path() {
		t.Fatalf("selected foreign document escaped strict admission: %#v", sink)
	}
	state, report, err = regressionCompiler().Compile(context.Background(), desiredstate.Sources{Files: []desiredstate.SourceFile{native}})
	sink = requireCompilationFailure(t, state, report, err)
	if len(sink) != 1 || sink[0].Code != "api.version" {
		t.Fatalf("sole malformed Environment lost its version error: %#v", sink)
	}
}

func TestInheritedReferenceProvenanceAndImmutableSourceState(t *testing.T) {
	env := environmentYAML + "  defaults:\n    Registry:\n      credentialsRef: missing\n---\napiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata: {name: material}\nspec: {type: usernamePassword}\n"
	consumer := "apiVersion: bootwright.io/v1alpha1\nkind: Registry\nmetadata: {name: consumer}\nspec:\n  management: external\n  url: registry.example.test\n"
	input := sources(env)
	input.Files = append(input.Files, desiredstate.NewSourceFile("/synthetic/registries/consumer.yaml", []byte(consumer)))
	before := append([]desiredstate.SourceFile{}, input.Files...)
	state, report, err := regressionCompiler().Compile(context.Background(), input)
	sink := requireCompilationFailure(t, state, report, err)
	if len(sink) != 1 {
		t.Fatalf("missing owner caused dependent failures: %#v", sink)
	}
	d := sink[0]
	line := 1 + strings.Count(env[:strings.Index(env, "credentialsRef:")], "\n")
	if d.Code != "api.reference" || d.Field != "$.spec.credentialsRef" || d.Object == nil || d.Object.Kind != string(api.Registry) || d.Object.Name != "consumer" || d.Source == nil || d.Source.Path != "/synthetic/environment.yaml" || d.Source.Line != line || !strings.Contains(d.Message, "Environment defaults") || d.Remediation == "" {
		t.Fatalf("inherited reference provenance is incomplete: %#v", d)
	}
	if !reflect.DeepEqual(before, input.Files) {
		t.Fatal("failed admission mutated source bytes")
	}
	resolved := strings.Replace(env, "credentialsRef: missing", "credentialsRef: material", 1)
	input.Files[0] = desiredstate.NewSourceFile("/synthetic/environment.yaml", []byte(resolved+controllerYAML))
	state, _, err = regressionCompiler().Compile(context.Background(), input)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	authored, _ := state.Authored().Find(api.Registry, "consumer")
	effective, _ := state.Effective().Find(api.Registry, "consumer")
	if authored.Spec().Has("credentialsRef") || effective.Spec().Get("credentialsRef").Text() != "material" {
		t.Fatal("authored and effective values lost their separate identities")
	}
	origin, ok := state.Origin(effective.Identity())
	if !ok || origin.Path != "/synthetic/registries/consumer.yaml" || origin.Document != 1 {
		t.Fatal("object origin moved to its default provider", origin)
	}
	origin.Path = "changed"
	fields := effective.Spec().Fields()
	fields[0].Value = api.StringValue("changed")
	again, _ := state.Effective().Find(api.Registry, "consumer")
	againOrigin, _ := state.Origin(effective.Identity())
	if !again.Spec().Equal(effective.Spec()) || againOrigin.Path != "/synthetic/registries/consumer.yaml" || string(input.Files[1].Bytes()) != consumer {
		t.Fatal("returned inspection values alias compiler state or sources")
	}
}

func TestCompilerInheritancePreservesExplicitModesAndNestedSuppression(t *testing.T) {
	input := sources(environmentYAML + `  defaults:
    OpenShiftCluster:
      distribution:
        type: openshift
        release: {channel: stable}
      install:
        platform: {baremetal: {provisioningNetwork: disabled}}
        agent:
          bootArtifacts:
            artifactServerEndpoint: {serverRef: install, endpointRef: media}
        endpoints:
          api: {address: 192.0.2.10}
---
apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata: {name: node}
spec: {os: {provided: true}}
---
apiVersion: bootwright.io/v1alpha1
kind: OpenShiftCluster
metadata: {name: cluster}
spec:
  distribution: {type: okd, release: {version: 4.20.0}}
  install:
    mode: connected
    platform: {none: {}}
    endpoints:
      api: {source: {type: node}}
      ingress: {}
  nodes: [{name: master, role: controlPlane, machineRef: node}]
`)
	// Isolate compiler inheritance using the production wire shapes. Component
	// runtime prerequisites are tested by their owning admission suites.
	state, _, err := compilation.NewCompiler(yamlstream.Parser{}, nil).Compile(context.Background(), input)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	cluster, _ := state.Effective().Find(api.OpenShiftCluster, "cluster")
	spec := cluster.Spec()
	for _, path := range [][]string{{"distribution", "release", "channel"}, {"install", "platform", "baremetal"}, {"install", "agent", "bootArtifacts"}, {"install", "endpoints", "api", "address"}} {
		if spec.Has(path...) {
			t.Fatalf("inactive Environment fallback leaked into explicit mode: %v", path)
		}
	}
	if !spec.Has("install", "platform", "none") || spec.Get("distribution", "type").Text() != "okd" || spec.Get("install", "mode").Text() != "connected" {
		t.Fatal("Environment defaults changed an explicit discriminator")
	}
	env, _ := state.Effective().Find(api.Environment, "synthetic")
	if !env.Spec().Has("defaults", "OpenShiftCluster", "install", "agent", "bootArtifacts") {
		t.Fatal("recipient suppression mutated the authored defaults fragment")
	}
}

// An excluded cluster root is a selection warning, not an advisory: advisories
// carry only inactive authored semantics.
func TestExcludedClusterRootIsASelectionWarning(t *testing.T) {
	cluster := `
---
apiVersion: bootwright.io/v1alpha1
kind: OpenShiftCluster
metadata: {name: NAME}
spec:
  distribution: {type: openshift, release: {version: 4.20.0}}
  install:
    mode: connected
    platform: {none: {}}
    endpoints:
      api: {source: {type: node}}
      ingress: {}
  nodes: [{name: master, role: controlPlane, machineRef: node}]
`
	input := sources(environmentYAML + "  containerClusters: [kept]\n---\napiVersion: bootwright.io/v1alpha1\nkind: Machine\nmetadata: {name: node}\nspec: {os: {provided: true}}\n" +
		strings.Replace(cluster, "NAME", "kept", 1) + strings.Replace(cluster, "NAME", "dropped", 1))
	compiler := compilation.NewCompiler(yamlstream.Parser{}, environmentSelection, compilation.Rules{Normalize: environment.Normalize, Validate: testEnvironmentValidate})
	state, report, err := compiler.Compile(context.Background(), input)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if _, ok := state.Effective().Find(api.OpenShiftCluster, "dropped"); ok {
		t.Fatal("the excluded cluster root stayed in effective state")
	}
	if !reflect.DeepEqual(report.ExcludedContainerClusters, []string{"dropped"}) || len(report.Advisories) != 0 || len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "api.selection" {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func TestStorageRetirementGuidanceSurvivesStrictDecoding(t *testing.T) {
	for _, row := range []struct{ name, body, field, replacement string }{
		{"helper", "  monitoring: {enabled: true}\n", "$.spec.monitoring", "$.spec.services"},
		{"node roles", "  topology:\n    nodes: [{name: node, machineRef: controller, roles: [mon]}]\n", "$.spec.topology.nodes[0].roles", "$.spec.services"},
		{"drive group", "  topology:\n    osdDrivegroups: []\n", "$.spec.topology.osdDrivegroups", "$.spec.services[].spec.data_devices"},
		{"service identity", "  services: [{serviceType: mon}]\n", "$.spec.services[0].serviceType", "$.spec.services[0].service_type"},
	} {
		t.Run(row.name, func(t *testing.T) {
			input := sources(environmentYAML + "---\napiVersion: bootwright.io/v1alpha1\nkind: CephCluster\nmetadata: {name: storage}\nspec:\n" + row.body)
			state, report, err := regressionCompiler().Compile(context.Background(), input)
			sink := requireCompilationFailure(t, state, report, err)
			if len(sink) != 1 || sink[0].Code != "api.field" || sink[0].Field != row.field || sink[0].Remediation != "replace "+row.field+" with "+row.replacement || !strings.Contains(sink[0].Message, "native-service migration") {
				t.Fatalf("retirement lost exact replacement: %+v", sink)
			}
		})
	}
	input := sources(environmentYAML + "  defaults:\n    CephCluster:\n      mgmtGateway: {}\n")
	state, report, err := regressionCompiler().Compile(context.Background(), input)
	sink := requireCompilationFailure(t, state, report, err)
	if len(sink) != 1 || sink[0].Field != "$.spec.defaults.CephCluster.mgmtGateway" || sink[0].Remediation != "replace $.spec.defaults.CephCluster.mgmtGateway with $.spec.defaults.CephCluster.services" {
		t.Fatalf("retired default lost its owner: %+v", sink)
	}
}

// Every cluster family kind shares one name namespace, so the cluster commands
// resolve a name to one object; each colliding root names the other.
func TestClusterKindsOfOneNameRefuseNamingEachOther(t *testing.T) {
	storage := "apiVersion: bootwright.io/v1alpha1\nkind: CephCluster\nmetadata: {name: shared}\nspec: {management: external}\n"
	container := "apiVersion: bootwright.io/v1alpha1\nkind: OpenShiftCluster\nmetadata: {name: shared}\nspec: {}\n"
	state, report, err := regressionCompiler().Compile(context.Background(), sources(environmentYAML+"---\n"+storage+"---\n"+container))
	sink := requireCompilationFailure(t, state, report, err)
	named := map[string]string{}
	for _, d := range sink {
		if d.Code == "api.duplicate" && d.Object != nil && d.Field == "$.metadata.name" {
			named[d.Object.Kind] = d.Message
		}
	}
	if !strings.Contains(named["CephCluster"], "OpenShiftCluster/shared") || !strings.Contains(named["OpenShiftCluster"], "CephCluster/shared") {
		t.Fatalf("colliding cluster roots = %q, want each naming the other", named)
	}
}
