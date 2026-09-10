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
	"github.com/crmarques/bootwright/internal/environment"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/storage"
	"github.com/crmarques/bootwright/internal/substrate"
)

func regressionCompiler() compilation.Compiler {
	selection := func(catalog api.Catalog) compilation.Selection {
		selected := environment.Select(catalog, nil)
		result := compilation.Selection{Catalog: selected.Catalog, ExcludedContainerClusters: selected.ExcludedContainerClusters, ExcludedStorageClusters: selected.ExcludedStorageClusters}
		for _, problem := range selected.Problems {
			result.Problems = append(result.Problems, compilation.ObjectIssue{Object: problem.Object, Issue: problem.Issue})
		}
		return result
	}
	return compilation.NewCompiler(yamlstream.Parser{}, selection,
		compilation.Rules{Normalize: environment.Normalize, Validate: environment.Validate},
		compilation.Rules{Normalize: infrastructureservices.Normalize, ValidateAuthored: infrastructureservices.ValidateAuthored, ValidatePartial: infrastructureservices.ValidatePartial, Validate: infrastructureservices.Validate},
		compilation.Rules{Normalize: secrets.Normalize, ValidateAuthored: secrets.ValidateAuthored, ValidatePartial: secrets.ValidateAuthored, Validate: secrets.Validate},
		compilation.Rules{Normalize: storage.Normalize, ValidateAuthored: storage.ValidateAuthored, ValidatePartial: storage.ValidatePartial, Validate: storage.Validate},
		compilation.Rules{Normalize: substrate.Normalize, ValidateAuthored: substrate.ValidateAuthored, ValidatePartial: substrate.ValidatePartial, Validate: substrate.Validate})
}

func requireCompilationFailure(t *testing.T, state *compilation.State, report *compilation.Report, err error) []desiredstate.Diagnostic {
	t.Helper()
	if state != nil || report != nil || err == nil {
		t.Fatalf("failed admission exposed a result: state=%v report=%v err=%v", state, report, err)
	}
	diagnostics := desiredstate.DiagnosticsOf(err)
	if len(diagnostics) == 0 {
		t.Fatalf("admission failure has no typed diagnostics: %v", err)
	}
	for i := 1; i < len(diagnostics); i++ {
		if desiredstate.CompareDiagnostics(diagnostics[i-1], diagnostics[i]) > 0 {
			t.Fatal("admission diagnostics are not canonically ordered")
		}
	}
	return diagnostics
}

func TestUnusedDefaultsRejectPresentContradictions(t *testing.T) {
	for _, test := range []struct{ name, fields, field string }{
		{"discriminator", "  defaults:\n    StoragePool:\n      type: replicated\n      erasure: {dataChunks: 2, codingChunks: 1}\n", "$.spec.defaults.StoragePool.erasure"},
		{"secret parameters", "  defaults:\n    Secret:\n      type: token\n      source: {generated: {username: alice}}\n", "$.spec.defaults.Secret.source.generated.username"},
		{"external service placement", "  defaults:\n    Proxy:\n      management: external\n      machineRef: host\n", "$.spec.defaults.Proxy.machineRef"},
		{"storage range", "  defaults:\n    StoragePool:\n      compression: {minBlobSize: 20, maxBlobSize: 10}\n", "$.spec.defaults.StoragePool.compression"},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, report, err := regressionCompiler().Compile(context.Background(), sources(environmentYAML+test.fields))
			diagnostics := requireCompilationFailure(t, state, report, err)
			for _, d := range diagnostics {
				if d.Code == "api.invariant" && d.Field == test.field && d.Object != nil && d.Object.Kind == string(api.Environment) {
					return
				}
			}
			t.Fatalf("missing partial-default diagnostic for %s: %#v", test.field, diagnostics)
		})
	}
}

func TestUnusedDefaultsPermitMissingRequiredArmsAndReferences(t *testing.T) {
	input := sources(environmentYAML + "  defaults:\n    InfraProvider:\n      baremetal: {}\n      networkAttachments: [{name: network}]\n    StoragePool:\n      clusterRef: future\n      compression: {minBlobSize: 20}\n    Secret:\n      source: {generated: {bytes: 32}}\n")
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
	cluster := "apiVersion: bootwright.io/v1alpha1\nkind: StorageCluster\nmetadata: {name: storage}\nspec: {type: ceph, management: external}\n"
	input := sources(environmentYAML + "  storageClusters: [storage]\n---\n" + cluster + "---\n" + cluster)
	state, report, err := regressionCompiler().Compile(context.Background(), input)
	diagnostics := requireCompilationFailure(t, state, report, err)
	documents := map[int]bool{}
	selectionFailed := false
	for _, d := range diagnostics {
		if d.Code == "api.reference" && d.Field == "$.spec.storageClusters" {
			selectionFailed = true
		}
		if d.Code == "api.duplicate" && d.Object != nil && d.Object.Kind == string(api.StorageCluster) && d.Object.Name == "storage" && d.Source != nil {
			documents[d.Source.Document] = true
		}
		if d.Severity == "warning" {
			t.Fatal("a requested ambiguous root was mislabeled as excluded", d)
		}
	}
	if !selectionFailed || !reflect.DeepEqual(documents, map[int]bool{2: true, 3: true}) {
		t.Fatalf("duplicate identity evidence was lost: %#v", diagnostics)
	}
}

func TestEnvironmentSelectionUsesBootwrightAPIVersion(t *testing.T) {
	native := desiredstate.NewSourceFile("/synthetic/native.yaml", []byte("apiVersion: other.example/v1\nkind: Environment\nmetadata: {name: native}\nspec: {}\n"))
	for _, selected := range []bool{false, true} {
		body := environmentYAML
		if !selected {
			body += "  resources: [environment.yaml]\n"
		}
		input := sources(body)
		input.Files = append(input.Files, native)
		state, report, err := regressionCompiler().Compile(context.Background(), input)
		if !selected {
			if err != nil || report.Counts != (compilation.Counts{FilesSeen: 2, ObjectsDecoded: 2}) || len(report.Diagnostics) != 0 || len(state.Effective().Objects()) != 2 {
				t.Fatalf("foreign excluded Environment affected selecting identity: %v %#v", err, report)
			}
			continue
		}
		diagnostics := requireCompilationFailure(t, state, report, err)
		if len(diagnostics) != 1 || diagnostics[0].Code != "api.version" || diagnostics[0].Source.Path != native.Path() {
			t.Fatalf("selected foreign document escaped strict admission: %#v", diagnostics)
		}
	}
	state, report, err := regressionCompiler().Compile(context.Background(), desiredstate.Sources{Files: []desiredstate.SourceFile{native}})
	diagnostics := requireCompilationFailure(t, state, report, err)
	if len(diagnostics) != 1 || diagnostics[0].Code != "api.version" {
		t.Fatalf("sole malformed Environment lost its version error: %#v", diagnostics)
	}
}

func TestInheritedReferenceProvenanceAndImmutableSourceState(t *testing.T) {
	env := environmentYAML + "  defaults:\n    StorageExport:\n      clusterRef: missing\n---\napiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata: {name: material}\nspec: {type: opaque}\n"
	consumer := "apiVersion: bootwright.io/v1alpha1\nkind: StorageExport\nmetadata: {name: consumer}\nspec:\n  type: dataFoundation\n  externalDetails: {fromSecretRef: material}\n"
	input := sources(env)
	input.Files = append(input.Files, desiredstate.NewSourceFile("/synthetic/exports/consumer.yaml", []byte(consumer)))
	before := append([]desiredstate.SourceFile{}, input.Files...)
	state, report, err := regressionCompiler().Compile(context.Background(), input)
	diagnostics := requireCompilationFailure(t, state, report, err)
	if len(diagnostics) != 1 {
		t.Fatalf("missing owner caused dependent failures: %#v", diagnostics)
	}
	d := diagnostics[0]
	line := 1 + strings.Count(env[:strings.Index(env, "clusterRef:")], "\n")
	if d.Code != "api.reference" || d.Field != "$.spec.clusterRef" || d.Object == nil || d.Object.Kind != string(api.StorageExport) || d.Object.Name != "consumer" || d.Source == nil || d.Source.Path != "/synthetic/environment.yaml" || d.Source.Line != line || !strings.Contains(d.Message, "Environment defaults") || d.Remediation == "" {
		t.Fatalf("inherited reference provenance is incomplete: %#v", d)
	}
	if !reflect.DeepEqual(before, input.Files) {
		t.Fatal("failed admission mutated source bytes")
	}
	resolved := strings.Replace(env, "clusterRef: missing", "clusterRef: storage", 1) + "---\napiVersion: bootwright.io/v1alpha1\nkind: StorageCluster\nmetadata: {name: storage}\nspec: {type: ceph, management: external}\n"
	input.Files[0] = desiredstate.NewSourceFile("/synthetic/environment.yaml", []byte(resolved+controllerYAML))
	state, _, err = regressionCompiler().Compile(context.Background(), input)
	if err != nil {
		t.Fatal(desiredstate.DiagnosticsOf(err))
	}
	authored, _ := state.Authored().Find(api.StorageExport, "consumer")
	effective, _ := state.Effective().Find(api.StorageExport, "consumer")
	if authored.Spec().Has("clusterRef") || effective.Spec().Get("clusterRef").Text() != "storage" {
		t.Fatal("authored and effective values lost their separate identities")
	}
	origin, ok := state.Origin(effective.Identity())
	if !ok || origin.Path != "/synthetic/exports/consumer.yaml" || origin.Document != 1 {
		t.Fatal("object origin moved to its default provider", origin)
	}
	origin.Path = "changed"
	fields := effective.Spec().Fields()
	fields[0].Value = api.StringValue("changed")
	again, _ := state.Effective().Find(api.StorageExport, "consumer")
	againOrigin, _ := state.Origin(effective.Identity())
	if !again.Spec().Equal(effective.Spec()) || againOrigin.Path != "/synthetic/exports/consumer.yaml" || string(input.Files[1].Bytes()) != consumer {
		t.Fatal("returned inspection values alias compiler state or sources")
	}
}

func TestCompilerInheritancePreservesExplicitModesAndNestedSuppression(t *testing.T) {
	input := sources(environmentYAML + `  defaults:
    ContainerCluster:
      distribution:
        type: openshift
        release: {channel: stable}
      install:
        platform: {type: baremetal, baremetal: {provisioningNetwork: disabled}}
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
kind: ContainerCluster
metadata: {name: cluster}
spec:
  distribution: {type: okd, release: {version: 4.20.0}}
  install:
    mode: connected
    platform: {type: none}
    endpoints:
      api: {source: {type: node}}
      ingress: {}
  nodes: [{name: master, role: master, machineRef: node}]
`)
	// Isolate compiler inheritance using the production wire shapes. Component
	// runtime prerequisites are tested by their owning admission suites.
	state, _, err := compilation.NewCompiler(yamlstream.Parser{}, nil).Compile(context.Background(), input)
	if err != nil {
		t.Fatal(desiredstate.DiagnosticsOf(err))
	}
	cluster, _ := state.Effective().Find(api.ContainerCluster, "cluster")
	spec := cluster.Spec()
	for _, path := range [][]string{{"distribution", "release", "channel"}, {"install", "platform", "baremetal"}, {"install", "agent", "bootArtifacts"}, {"install", "endpoints", "api", "address"}} {
		if spec.Has(path...) {
			t.Fatalf("inactive Environment fallback leaked into explicit mode: %v", path)
		}
	}
	if spec.Get("install", "platform", "type").Text() != "none" || spec.Get("distribution", "type").Text() != "okd" || spec.Get("install", "mode").Text() != "connected" {
		t.Fatal("Environment defaults changed an explicit discriminator")
	}
	env, _ := state.Effective().Find(api.Environment, "synthetic")
	if !env.Spec().Has("defaults", "ContainerCluster", "install", "agent", "bootArtifacts") {
		t.Fatal("recipient suppression mutated the authored defaults fragment")
	}
}
