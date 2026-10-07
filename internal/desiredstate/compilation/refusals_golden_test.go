package compilation_test

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/desiredstate/yamlstream"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/environment"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/managedos"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/storage"
)

var update = flag.Bool("update", false, "rewrite testdata/*.golden from the current output")

const refusalSentinel = "zz-authored-sentinel"

const refusalSecret = "apiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata:\n  name: probe\nspec:\n"

type refusalRow struct{ name, environment, probe string }

func refusalCompiler() compilation.Compiler {
	return compilation.NewCompiler(yamlstream.Parser{}, environmentSelection,
		compilation.Rules{Normalize: environment.Normalize, Validate: environment.Validate},
		compilation.Rules{Normalize: secrets.Normalize, ValidateAuthored: secrets.ValidateAuthored, ValidatePartial: secrets.ValidatePartial, Validate: secrets.Validate},
		compilation.Rules{Normalize: managedos.Normalize, ValidateAuthored: managedos.ValidateAuthored, ValidatePartial: managedos.ValidatePartial, Validate: managedos.Validate},
		compilation.Rules{Normalize: storage.Normalize, ValidateAuthored: storage.ValidateAuthored, ValidatePartial: storage.ValidatePartial, Validate: storage.Validate},
		compilation.Rules{Normalize: machine.Normalize, NormalizationOrigins: machine.NormalizationOrigins, ValidateAuthored: machine.ValidateAuthored, ValidatePartial: machine.ValidatePartial, Validate: machine.Validate},
		compilation.Rules{Normalize: infrastructureservices.Normalize, ValidateAuthored: infrastructureservices.ValidateAuthored, ValidatePartial: infrastructureservices.ValidatePartial, Validate: infrastructureservices.Validate})
}

func refusalRows() []refusalRow {
	secondEnvironment := "\n---\n" + strings.Replace(environmentYAML, "name: synthetic", "name: second", 1)
	return []refusalRow{
		{"enum", environmentYAML, refusalSecret + "  type: " + refusalSentinel + "\n"},
		{"case-only enum", environmentYAML, refusalSecret + "  type: sshkeypair\n"},
		{"range", environmentYAML, refusalSecret + "  type: token\n  source: {generated: {bytes: 8}}\n"},
		{"quoted integer", environmentYAML, refusalSecret + "  type: token\n  source: {generated: {bytes: '32'}}\n"},
		{"quoted boolean", environmentYAML, "apiVersion: bootwright.io/v1alpha1\nkind: Machine\nmetadata: {name: probe}\nspec:\n  os: {provided: \"false\"}\n"},
		{"plain number in a string field", environmentYAML + "  sites: [{name: east, description: 9.8}]\n", ""},
		{"missing reference", environmentYAML + "  remoteMachinesAccessKey: {keyRef: missing}\n", ""},
		{"wrong Secret type", environmentYAML + "  remoteMachinesAccessKey: {keyRef: probe}\n", refusalSecret + "  type: opaque\n"},
		{"typo key", environmentYAML, refusalSecret + "  typ: opaque\n"},
		{"nested typo", strings.Replace(environmentYAML, "    base: example.test\n", "    base: example.test\n    basee: example.test\n", 1), ""},
		{"non-identifier key", environmentYAML, refusalSecret + "  type: opaque\n  \"" + refusalSentinel + " key!\": value\n"},
		{"kind defaults key", environmentYAML + "  defaults:\n    Machines: {}\n", ""},
		{"two Secret source arms", environmentYAML, refusalSecret + "  type: token\n  source: {contextStore: {}, generated: {bytes: 32}}\n"},
		{"Proxy implementation enum", environmentYAML, "apiVersion: bootwright.io/v1alpha1\nkind: Proxy\nmetadata: {name: probe}\nspec:\n  management: managed\n  machineRef: controller\n  implementation: " + refusalSentinel + "\n  bindAddress: 192.0.2.1\n"},
		{"Context document", environmentYAML, "apiVersion: bootwright.io/v1alpha1\nkind: Context\nmetadata: {name: probe}\nspec: {secretStore: {type: local-keyring}}\n"},
		{"machineAccess", environmentYAML + "  machineAccess: {}\n", ""},
		{"safety", environmentYAML + "  safety: {}\n", ""},
		{"no Environment", "", refusalSecret + "  type: opaque\n"},
		{"two Environments", environmentYAML + secondEnvironment, ""},
		{"empty required list", environmentYAML, "apiVersion: bootwright.io/v1alpha1\nkind: LoadBalancer\nmetadata: {name: probe}\nspec:\n  management: external\n  bindAddresses: []\n"},
		{"duplicate entry", environmentYAML + "  sites: [{name: east}, {name: east}]\n", ""},
		{"duplicate entry that is not a label", environmentYAML + "  sites: [{name: " + refusalSentinel + ".}, {name: " + refusalSentinel + ".}]\n", ""},
		{"name that is not a label", environmentYAML, strings.Replace(refusalSecret, "name: probe", "name: "+refusalSentinel+".", 1) + "  type: opaque\n  typ: x\n"},
		{"key three edits from its only near field", environmentYAML, refusalSecret + "  name: probe\n  type: opaque\n"},
		{"kind three edits from a registered kind", environmentYAML, "apiVersion: bootwright.io/v1alpha1\nkind: Mach\nmetadata: {name: probe}\nspec: {}\n"},
		{"kind defaults key three edits from a kind", environmentYAML + "  defaults:\n    Mach: {}\n", ""},
		{"referenced Secret that does not decode", environmentYAML + "  remoteMachinesAccessKey: {keyRef: probe}\n", refusalSecret + "  type: sshKeyPair\n  extra: x\n"},
		{"referenced Secret whose type is not a string", environmentYAML + "  remoteMachinesAccessKey: {keyRef: probe}\n", refusalSecret + "  type: 1\n"},
		{"network selection without a configured network", environmentYAML, "apiVersion: bootwright.io/v1alpha1\nkind: Machine\nmetadata: {name: probe}\nspec:\n  network:\n    installAddressRef: ip\n    addresses: [{name: ip, address: 192.0.2.9/24, interface: eth0}]\n"},
		{"interface assignment on an OS-ready Machine", environmentYAML, "apiVersion: bootwright.io/v1alpha1\nkind: Machine\nmetadata: {name: probe}\nspec:\n  os: {provided: true}\n  network:\n    addresses: [{name: ip, address: 192.0.2.9/24, interface: eth0}]\n"},
		{"selected cluster that does not decode", environmentYAML + "  containerClusters: [probe]\n", undecodableCluster},
	}
}

func refusalSources(row refusalRow) desiredstate.Sources {
	files := []desiredstate.SourceFile{}
	if row.environment != "" {
		files = append(files, desiredstate.NewSourceFile("/synthetic/environment.yaml", []byte(row.environment+controllerYAML)))
	}
	if row.probe != "" {
		files = append(files, desiredstate.NewSourceFile(strictYAMLProbePath, []byte(row.probe)))
	}
	return desiredstate.Sources{Files: files, Roots: []string{"/synthetic"}}
}

func TestValidateRefusalsNameObjectFieldExpectationAndRemedy(t *testing.T) {
	var golden strings.Builder
	for _, row := range refusalRows() {
		input := refusalSources(row)
		state, report, err := refusalCompiler().Compile(context.Background(), input)
		found := diagnostics.Of(err)
		if state != nil || report != nil || len(found) == 0 {
			t.Fatalf("%s: the probe was not refused: %v", row.name, err)
		}
		identities := documentIdentities(t, input)
		for _, d := range found {
			golden.WriteString(row.name + ": " + refusalLine(d) + "\n")
			if d.Severity == "error" && d.Remediation == "" {
				t.Errorf("%s: %s at %s has no next step", row.name, d.Code, d.Field)
			}
			if strings.HasSuffix(d.Remediation, " ") {
				t.Errorf("%s: %s at %s names an empty target: %q", row.name, d.Code, d.Field, d.Remediation)
			}
			if text := d.Message + "; " + d.Remediation + " " + d.Field + " " + objectName(d.Object); strings.Contains(text, refusalSentinel) {
				t.Errorf("%s: %s repeats authored text: %q", row.name, d.Code, text)
			}
			if d.Source == nil {
				continue
			}
			if want, ok := identities[fmt.Sprintf("%s#%d", d.Source.Path, d.Source.Document)]; ok && (d.Object == nil || *d.Object != want) {
				t.Errorf("%s: %s at %s names object %+v, want %+v", row.name, d.Code, d.Field, d.Object, want)
			}
		}
	}
	matchesTextGolden(t, "refusals", []byte(golden.String()))
}

func objectName(object *diagnostics.ObjectIdentity) string {
	if object == nil {
		return ""
	}
	return object.Name
}

func refusalLine(d diagnostics.Diagnostic) string {
	line := d.Code + " -"
	if d.Source != nil {
		line = fmt.Sprintf("%s %d:%d", d.Code, d.Source.Line, d.Source.Column)
	}
	if d.Object != nil {
		line += " [" + d.Object.Kind + "/" + d.Object.Name + "]"
	}
	if d.Field != "" {
		line += " (" + d.Field + ")"
	}
	line += " " + d.Message
	if d.Remediation != "" {
		line += "; next: " + d.Remediation
	}
	return line
}

func documentIdentities(t *testing.T, input desiredstate.Sources) map[string]diagnostics.ObjectIdentity {
	t.Helper()
	documents, _, err := yamlstream.Parser{}.Parse(context.Background(), input.Files)
	if err != nil {
		t.Fatal(err)
	}
	identities := map[string]diagnostics.ObjectIdentity{}
	for _, document := range documents {
		if document.Root == nil || len(document.Root.Content) != 1 {
			continue
		}
		root := document.Root.Content[0]
		apiVersion, kind := scalarAt(root, "apiVersion"), scalarAt(root, "kind")
		name := scalarAt(childAt(root, "metadata"), "name")
		if apiVersion == api.APIVersion && api.KindIndex(api.Kind(kind)) >= 0 && api.ValidLexical("name", name) {
			identities[fmt.Sprintf("%s#%d", document.Path, document.Index)] = diagnostics.ObjectIdentity{APIVersion: apiVersion, Kind: kind, Name: name}
		}
	}
	return identities
}

func childAt(node *desiredstate.Node, key string) *desiredstate.Node {
	if node == nil || node.Kind != desiredstate.MappingKind {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func scalarAt(node *desiredstate.Node, key string) string {
	if child := childAt(node, key); child != nil && child.Kind == desiredstate.ScalarKind {
		return child.Value
	}
	return ""
}

func matchesTextGolden(t *testing.T, name string, text []byte) {
	t.Helper()
	if len(text) == 0 || text[len(text)-1] != '\n' || bytes.HasSuffix(text, []byte("\n\n")) || bytes.Contains(text, []byte(" \n")) {
		t.Fatalf("%s: a golden holds non-empty text with one final LF and no trailing space: %q", name, text)
	}
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, text, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v; run with -update to write it", err)
	}
	if !bytes.Equal(want, text) {
		wantLines, gotLines := strings.Split(string(want), "\n"), strings.Split(string(text), "\n")
		for i := 0; i < max(len(wantLines), len(gotLines)); i++ {
			if i >= len(wantLines) || i >= len(gotLines) || wantLines[i] != gotLines[i] {
				t.Fatalf("%s differs at line %d; rerun with -update if the change is intended\ngolden: %q\ngot:    %q", path, i+1, lineAt(wantLines, i), lineAt(gotLines, i))
			}
		}
	}
}

func lineAt(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return ""
}

func TestOneInvalidSecretTypeYieldsOneDiagnostic(t *testing.T) {
	for name, spec := range map[string]string{"absent": "  source: {}\n", "case-only": "  type: sshkeypair\n"} {
		t.Run(name, func(t *testing.T) {
			input := refusalSources(refusalRow{environment: environmentYAML + "  remoteMachinesAccessKey: {keyRef: probe}\n", probe: refusalSecret + spec})
			_, _, err := refusalCompiler().Compile(context.Background(), input)
			found := diagnostics.Of(err)
			if len(found) != 1 || found[0].Object == nil || found[0].Object.Kind != string(api.Secret) || found[0].Object.Name != "probe" || found[0].Field != "$.spec.type" {
				t.Fatalf("one invalid Secret type gave %+v", found)
			}
		})
	}
}

const undecodableCluster = "apiVersion: bootwright.io/v1alpha1\nkind: ContainerCluster\nmetadata: {name: probe}\nspec:\n  extra: x\n"

func TestAnUndecodableTargetHasOnlyItsOwnDiagnostics(t *testing.T) {
	referenceRule := compilation.Rules{Validate: func(object api.Object, _ api.Catalog) []api.Issue {
		if object.Kind() != api.Environment {
			return nil
		}
		return []api.Issue{{Code: "api.reference", Field: "$.spec.remoteMachinesAccessKey.keyRef", Message: "domain reference check", Remediation: "domain remedy"}}
	}}
	withReferenceRule := compilation.NewCompiler(yamlstream.Parser{}, environmentSelection, referenceRule)
	for _, probe := range []struct {
		name     string
		compiler compilation.Compiler
		row      refusalRow
		target   string
	}{
		{"Secret with an unknown field", refusalCompiler(), refusalRow{environment: environmentYAML + "  remoteMachinesAccessKey: {keyRef: probe}\n", probe: refusalSecret + "  type: sshKeyPair\n  extra: x\n"}, "Secret"},
		{"Secret whose type is not a string", refusalCompiler(), refusalRow{environment: environmentYAML + "  remoteMachinesAccessKey: {keyRef: probe}\n", probe: refusalSecret + "  type: 1\n"}, "Secret"},
		{"domain reference check", withReferenceRule, refusalRow{environment: environmentYAML + "  remoteMachinesAccessKey: {keyRef: probe}\n", probe: refusalSecret + "  type: sshKeyPair\n  extra: x\n"}, "Secret"},
		{"selected cluster", refusalCompiler(), refusalRow{environment: environmentYAML + "  containerClusters: [probe]\n", probe: undecodableCluster}, "ContainerCluster"},
	} {
		t.Run(probe.name, func(t *testing.T) {
			_, _, err := probe.compiler.Compile(context.Background(), refusalSources(probe.row))
			found := diagnostics.Of(err)
			if len(found) != 1 || found[0].Object == nil || found[0].Object.Kind != probe.target || found[0].Object.Name != "probe" {
				t.Fatalf("one undecodable %s gave %+v", probe.target, found)
			}
		})
	}
	_, _, err := withReferenceRule.Compile(context.Background(), refusalSources(refusalRow{environment: environmentYAML + "  remoteMachinesAccessKey: {keyRef: probe}\n", probe: refusalSecret + "  type: sshKeyPair\n"}))
	if found := diagnostics.Of(err); len(found) != 1 || found[0].Message != "domain reference check" {
		t.Fatalf("a reference check against a decoded target was suppressed: %+v", found)
	}
}

func TestDefaultedMarksOnlyAFieldTheObjectHolds(t *testing.T) {
	probe := "apiVersion: bootwright.io/v1alpha1\nkind: Entitlement\nmetadata: {name: probe}\nspec:\n  type: redhat-rhel\n  rhsm: {}\n"
	rules := compilation.Rules{Validate: func(object api.Object, _ api.Catalog) []api.Issue {
		if object.Kind() != api.Entitlement {
			return nil
		}
		return []api.Issue{
			{Code: "api.reference", Field: "$.spec.rhsm.satellite.hostname", Message: "absent", Remediation: "own remedy"},
			{Code: "api.reference", Field: "$.spec.rhsm.management", Message: "built-in", Remediation: "own remedy"},
		}
	}}
	_, _, err := compilation.NewCompiler(yamlstream.Parser{}, nil, rules).Compile(context.Background(), refusalSources(refusalRow{environment: environmentYAML, probe: probe}))
	messages := map[string]diagnostics.Diagnostic{}
	for _, d := range diagnostics.Of(err) {
		messages[d.Field] = d
	}
	if absent := messages["$.spec.rhsm.satellite.hostname"]; absent.Message != "absent" || absent.Remediation != "own remedy" {
		t.Errorf("a field the object does not hold was marked defaulted: %+v", absent)
	}
	if defaulted := messages["$.spec.rhsm.management"]; defaulted.Message != "built-in (defaulted)" || defaulted.Remediation == "own remedy" {
		t.Errorf("a built-in default was not marked defaulted: %+v", defaulted)
	}
}

func TestEmptyListRemedyFollowsWhetherTheListIsRequired(t *testing.T) {
	loadBalancer := "apiVersion: bootwright.io/v1alpha1\nkind: LoadBalancer\nmetadata: {name: probe}\nspec:\n  management: external\n  bindAddresses: []\n"
	for _, probe := range []struct{ name, environment, probe, field, remediation string }{
		{"required", environmentYAML, loadBalancer, "$.spec.bindAddresses", "add at least one entry"},
		{"optional", environmentYAML + "  resources: []\n", "", "$.spec.resources", "add at least one entry, or omit the field"},
		{"kind defaults", environmentYAML + "  defaults:\n    LoadBalancer: {bindAddresses: []}\n", "", "$.spec.defaults.LoadBalancer.bindAddresses", "add at least one entry, or omit the field"},
	} {
		t.Run(probe.name, func(t *testing.T) {
			_, _, err := refusalCompiler().Compile(context.Background(), refusalSources(refusalRow{environment: probe.environment, probe: probe.probe}))
			found := diagnostics.Of(err)
			at := slices.IndexFunc(found, func(d diagnostics.Diagnostic) bool {
				return d.Field == probe.field && d.Message == "list must contain at least one entry"
			})
			if at < 0 || found[at].Remediation != probe.remediation {
				t.Fatalf("an empty %s list gave %+v, want the remedy %q", probe.name, found, probe.remediation)
			}
		})
	}
}
