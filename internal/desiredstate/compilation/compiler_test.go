package compilation_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/desiredstate/yamlstream"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/environment"
	"github.com/crmarques/bootwright/internal/secrets"
)

const environmentYAML = "apiVersion: bootwright.io/v1alpha1\nkind: Environment\nmetadata:\n  name: synthetic\nspec:\n  domains:\n    base: example.test\n  controller: {machineRef: controller}\n"

const controllerYAML = "\n---\napiVersion: bootwright.io/v1alpha1\nkind: Machine\nmetadata: {name: controller}\nspec:\n  os: {provided: true}\n  access: {local: true}\n"

func compiler() compilation.Compiler {
	return compilation.NewCompiler(yamlstream.Parser{}, nil, compilation.Rules{Normalize: environment.Normalize, Validate: testEnvironmentValidate}, compilation.Rules{Normalize: secrets.Normalize, ValidateAuthored: secrets.ValidateAuthored, Validate: secrets.Validate})
}
func sources(content string) desiredstate.Sources {
	return desiredstate.Sources{Files: []desiredstate.SourceFile{desiredstate.NewSourceFile("/synthetic/environment.yaml", []byte(content+controllerYAML))}, Roots: []string{"/synthetic"}}
}

func TestCompilerPreservesAuthoredAndDerivedValues(t *testing.T) {
	input := sources(environmentYAML)
	before := input.Files[0].Bytes()
	state, report, err := compiler().Compile(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if report.Counts != (compilation.Counts{FilesSeen: 1, ObjectsDecoded: 2}) || report.Advisories == nil {
		t.Fatalf("invalid report: %#v", report)
	}
	if state.Authored().Objects()[0].Spec().Has("domains", "machines") {
		t.Fatal("authored state mutated")
	}
	if state.Effective().Objects()[0].Spec().Get("domains", "machines").Text() != "example.test" {
		t.Fatal("domain fallback missing")
	}
	if !reflect.DeepEqual(before, input.Files[0].Bytes()) {
		t.Fatal("source bytes mutated")
	}
}

func TestCompilerRejectsStrictSyntaxAndScalarViolations(t *testing.T) {
	for _, test := range []struct{ name, body, code string }{
		{"duplicate", "type: opaque\n  type: token", "yaml.duplicate-key"},
		{"unknown", "type: opaque\n  unexpected: value", "api.field"},
		{"quoted integer", "type: token\n  source:\n    generated:\n      bytes: '32'", "api.type"},
		{"integer separator", "type: token\n  source:\n    generated:\n      bytes: 3_2", "api.type"},
		{"null", "type: opaque\n  source: null", "api.type"},
		{"alias", "type: opaque\n  source: &source {}", "yaml.alias"},
	} {
		t.Run(test.name, func(t *testing.T) {
			content := environmentYAML + "---\napiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata:\n  name: material\nspec:\n  " + test.body + "\n"
			state, report, err := compiler().Compile(context.Background(), sources(content))
			if state != nil || report != nil || err == nil {
				t.Fatalf("invalid success: %#v %#v %v", state, report, err)
			}
			found := false
			for _, d := range diagnostics.Of(err) {
				if d.Code == test.code {
					found = true
				}
			}
			if !found {
				t.Fatalf("wanted %s: %#v", test.code, diagnostics.Of(err))
			}
		})
	}
}

func TestCompilerDefaultPrecedenceAndRecipientPaths(t *testing.T) {
	env := environmentYAML + "  defaults:\n    Secret:\n      type: token\n      source:\n        generated:\n          bytes: 64\n"
	secret := "apiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata:\n  name: material\nspec:\n  source: {}\n"
	input := desiredstate.Sources{Files: []desiredstate.SourceFile{desiredstate.NewSourceFile("/synthetic/environment.yaml", []byte(env+controllerYAML)), desiredstate.NewSourceFile("/synthetic/nested/secret.yaml", []byte(secret))}, Roots: []string{"/synthetic"}}
	state, _, err := compiler().Compile(context.Background(), input)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	object, _ := state.Effective().Find(api.Secret, "material")
	if !object.Spec().Has("source", "contextStore") || object.Spec().Has("source", "generated") {
		t.Fatal("explicit empty source lost suppression")
	}
}

// The file source is retired in every type's shape: compilation refuses it,
// names the Secret, and names the context-storage declaration and the secret
// set flags of that type that replace it.
func TestCompilerRefusesTheRetiredFileSourceWithItsRemedy(t *testing.T) {
	for _, test := range []struct{ kind, shape, flags string }{
		{"opaque", "path: /nonexistent/opaque", "--value-file <path>"},
		{"token", "path: secrets/token", "--value-file <path>"},
		{"dockerConfigJson", "path: payload.json", "--value-file <path>"},
		{"usernamePassword", "path: secrets/credentials.json", "--username <username> --password-file <path>"},
		{"caBundle", "path: secrets/ca.pem", "--certificate-file <path>"},
		{"tlsCertificate", "cert: secrets/tls.crt, key: secrets/tls.key", "--certificate-file <path> --private-key-file <path>"},
		{"sshKeyPair", "privateKey: secrets/id", "--private-key-file <path> [--public-key-file <path>]"},
		{"sshKeyPair", "privateKey: secrets/id, publicKey: secrets/id.pub", "--private-key-file <path> [--public-key-file <path>]"},
	} {
		t.Run(test.kind+"/"+test.shape, func(t *testing.T) {
			content := environmentYAML + "---\napiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata:\n  name: material\nspec:\n  type: " + test.kind + "\n  source:\n    file: {" + test.shape + "}\n"
			state, report, err := compiler().Compile(context.Background(), sources(content))
			if state != nil || report != nil || err == nil {
				t.Fatalf("a file source compiled: %#v %#v %v", state, report, err)
			}
			found := diagnostics.Of(err)
			want := "declare source: {contextStore: {}} and run bootwright secret set --name material --context <context> " + test.flags
			if len(found) != 1 {
				t.Fatalf("diagnostics = %#v", found)
			}
			d := found[0]
			if d.Code != "api.field" || d.Field != "$.spec.source.file" || !strings.Contains(d.Message, "file source is retired") || d.Remediation != want || d.Object == nil || d.Object.Kind != "Secret" || d.Object.Name != "material" {
				t.Fatalf("refusal = %#v, want remedy %q", d, want)
			}
		})
	}
}

func TestCompilerRetiresResourcesAndHonoursCancellation(t *testing.T) {
	_, _, err := compiler().Compile(context.Background(), sources(environmentYAML+"  resources: [environment.yaml]\n"))
	found := diagnostics.Of(err)
	if len(found) != 1 || found[0].Code != "api.field" || found[0].Field != "$.spec.resources" || !strings.Contains(found[0].Remediation, "spec.containerClusters and spec.storageClusters are the only narrowing") {
		t.Fatalf("an authored resources list = %#v, want the retired-field refusal naming the cluster lists", found)
	}
	input := sources(environmentYAML)
	input.Files = append(input.Files, desiredstate.NewSourceFile("/synthetic/other.yaml", []byte("apiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata: {name: other}\nspec: {type: token}\n")))
	state, report, err := compiler().Compile(context.Background(), input)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if len(state.Effective().Objects()) != 3 || report.Counts.FilesSeen != 2 || report.Counts.ObjectsDecoded != 3 {
		t.Fatalf("every discovered file is not selected: %#v", report)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	state, report, err = compiler().Compile(ctx, input)
	if state != nil || report != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

// A skipped secrets directory holding YAML warns once, naming the directory,
// and a reference to an undeclared Secret names it in its remedy.
func TestASkippedSecretsDirectoryWarnsAndTheSecretRemedyNamesIt(t *testing.T) {
	input := sources(environmentYAML)
	input.SkippedSecrets = []string{"/synthetic/secrets"}
	_, report, err := compiler().Compile(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	var warned []string
	for _, d := range report.Diagnostics {
		if d.Code == "input.secrets-directory" && d.Severity == "warning" && d.Source != nil {
			warned = append(warned, d.Source.Path)
		}
	}
	if len(warned) != 1 || warned[0] != "/synthetic/secrets" {
		t.Fatalf("secrets-directory warnings = %v", warned)
	}
	input = sources(environmentYAML + "---\napiVersion: bootwright.io/v1alpha1\nkind: Entitlement\nmetadata: {name: rhel}\nspec:\n  type: redhatRhel\n  rhsm: {organizationRef: org, activationKeyRef: key}\n")
	input.SkippedSecrets = []string{"/synthetic/secrets"}
	_, _, err = compiler().Compile(context.Background(), input)
	named := 0
	for _, d := range diagnostics.Of(err) {
		if d.Code == "api.reference" && strings.Contains(d.Remediation, "discovery skips the secrets directory /synthetic/secrets") {
			named++
		}
	}
	if named != 2 {
		t.Fatalf("the unresolved Secret remedies name the directory %d times, want 2: %+v", named, diagnostics.Of(err))
	}
}

// A retired kind named where a kind is expected, as a kind-defaults key or a
// ClusterAddon input's resourceKind, is refused naming its replacement.
func TestCompilerNamesTheReplacementOfARetiredKindInDefaultsAndInputs(t *testing.T) {
	_, _, err := compiler().Compile(context.Background(), sources(environmentYAML+"  defaults:\n    ContainerCluster: {}\n"))
	found := diagnostics.Of(err)
	if len(found) != 1 || found[0].Field != "$.spec.defaults.ContainerCluster" || found[0].Message != "kind defaults name ContainerCluster, which is retired" ||
		found[0].Remediation != "use OpenShiftCluster; OKD stays spec.distribution.type: okd" {
		t.Fatalf("a retired kind-defaults key = %#v", found)
	}
	addon := "---\napiVersion: bootwright.io/v1alpha1\nkind: ClusterAddon\nmetadata: {name: addon}\nspec:\n  inputs:\n    - name: cluster\n      resourceKind: ContainerCluster\n"
	_, _, err = compiler().Compile(context.Background(), sources(environmentYAML+addon))
	for _, d := range diagnostics.Of(err) {
		if d.Field == "$.spec.inputs[0].resourceKind" {
			if !strings.Contains(d.Remediation, "use OpenShiftCluster") {
				t.Fatalf("a retired resourceKind = %#v", d)
			}
			return
		}
	}
	t.Fatalf("a retired resourceKind was not refused at its field: %#v", diagnostics.Of(err))
}
