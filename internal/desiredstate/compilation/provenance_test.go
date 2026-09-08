package compilation_test

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/desiredstate/yamlstream"
)

func TestSortedNestedListsKeepAuthoredAndInheritedDiagnosticLocations(t *testing.T) {
	list := "    - addonRef: z-addon\n      inputs:\n        - name: z-input\n          value: present\n        - name: a-input\n          value: ''\n    - addonRef: a-addon\n"
	binding := "apiVersion: bootwright.io/v1alpha1\nkind: ClusterAddonBinding\nmetadata:\n  name: selected\nspec:\n  clusterRef: cluster\n"
	for _, inherited := range []bool{false, true} {
		t.Run(map[bool]string{false: "authored", true: "inherited"}[inherited], func(t *testing.T) {
			content := environmentYAML
			if inherited {
				content += "  defaults:\n    ClusterAddonBinding:\n      addonConfigs:\n" + indent(list, 4)
				content += "---\n" + binding
			} else {
				content += "---\n" + binding + "  addonConfigs:\n" + list
			}
			c := compilation.NewCompiler(yamlstream.Parser{}, nil, compilation.Rules{Normalize: addons.Normalize})
			_, _, err := c.Compile(context.Background(), sources(content))
			if err == nil {
				t.Fatal("invalid references accepted")
			}
			want := strings.Count(strings.Split(content, "value: ''")[0], "\n") + 1
			found := false
			for _, diagnostic := range desiredstate.DiagnosticsOf(err) {
				if diagnostic.Object != nil && diagnostic.Object.Kind == string(api.ClusterAddonBinding) && diagnostic.Field == "$.spec.addonConfigs[1].inputs[0].value" {
					found = true
					if diagnostic.Source == nil || diagnostic.Source.Line != want {
						t.Fatalf("wrong origin: want line%d got%+v", want, diagnostic)
					}
					if inherited && !strings.Contains(diagnostic.Message, "from Environment defaults") {
						t.Fatalf("inherited origin missing: %+v", diagnostic)
					}
				}
			}
			if !found {
				t.Fatalf("expected nested field error: %+v", desiredstate.DiagnosticsOf(err))
			}
		})
	}
}

func indent(text string, width int) string {
	prefix := strings.Repeat(" ", width)
	return prefix + strings.ReplaceAll(strings.TrimSuffix(text, "\n"), "\n", "\n"+prefix) + "\n"
}

func TestMalformedEnvelopeValuesDoNotCascadeAsMissing(t *testing.T) {
	for _, field := range []string{"apiVersion", "kind", "metadata", "spec", "metadata.name"} {
		t.Run(field, func(t *testing.T) {
			object := "apiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata:\n  name: material\nspec:\n  type: opaque\n  source:\n    contextStore: {}\n"
			switch field {
			case "apiVersion":
				object = strings.Replace(object, "apiVersion: bootwright.io/v1alpha1", "apiVersion: null", 1)
			case "kind":
				object = strings.Replace(object, "kind: Secret", "kind: null", 1)
			case "metadata":
				object = strings.Replace(object, "metadata:\n  name: material", "metadata: null", 1)
			case "metadata.name":
				object = strings.Replace(object, "name: material", "name: null", 1)
			case "spec":
				object = object[:strings.Index(object, "spec:")] + "spec: null\n"
			}
			_, _, err := compiler().Compile(context.Background(), sources(environmentYAML+"---\n"+object))
			diagnostics := desiredstate.DiagnosticsOf(err)
			if len(diagnostics) != 1 || diagnostics[0].Code != "api.type" || diagnostics[0].Field != "$."+field {
				t.Fatalf("cascaded diagnostics: %+v", diagnostics)
			}
		})
	}
}

func TestNativeProvenanceDoesNotRepeatLongKeys(t *testing.T) {
	var document strings.Builder
	document.WriteString(environmentYAML + "---\napiVersion: bootwright.io/v1alpha1\nkind: CustomPlaybook\nmetadata:\n  name: reserved\nspec:\n  enabled: false\n  gates: fabric\n  playbook: playbooks/check.yaml\n  target:\n    hostGroups: [synthetic]\n  extraVars:\n    ? ")
	key := strings.Repeat("synthetic-native-key-", 3000)
	document.WriteString(key + "\n    :\n")
	for i := 0; i < 4000; i++ {
		fmt.Fprintf(&document, "      k%d: true\n", i)
	}
	input := sources(document.String())
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	state, _, err := compiler().Compile(context.Background(), input)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatalf("valid native map rejected: %+v", desiredstate.DiagnosticsOf(err))
	}
	// This fixture used to copy the 60 KiB key into 4,000 descendant paths.
	// A generous allocation bound detects that amplification independently of
	// parser internals and ordinary garbage-collector timing.
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 128<<20 {
		t.Fatalf("native provenance amplified allocations to%d bytes", allocated)
	}
	runtime.KeepAlive(state)
	invalid := strings.Replace(document.String(), "k0: true", "k0: null", 1)
	_, _, err = compiler().Compile(context.Background(), sources(invalid))
	diagnostics := desiredstate.DiagnosticsOf(err)
	if len(diagnostics) != 1 || diagnostics[0].Field != "$.spec.extraVars" || strings.Contains(diagnostics[0].Message, key) {
		t.Fatalf("native key escaped into diagnostics: count%d", len(diagnostics))
	}
}

func TestMalformedEnvironmentDoesNotEstablishScope(t *testing.T) {
	malformed := "apiVersion: bootwright.io/v1alpha1\nkind: Environment\nmetadata: null\nspec: {}\n"
	_, _, err := compiler().Compile(context.Background(), sources(environmentYAML+"---\n"+malformed))
	diagnostics := desiredstate.DiagnosticsOf(err)
	if len(diagnostics) != 1 || diagnostics[0].Code != "api.type" {
		t.Fatalf("wrong Environment cardinality: %+v", diagnostics)
	}
}

func TestSyntaxDiagnosticBudgetFollowsResourceSelection(t *testing.T) {
	for _, excluded := range []bool{false, true} {
		t.Run(map[bool]string{false: "selected", true: "excluded"}[excluded], func(t *testing.T) {
			env := environmentYAML
			if excluded {
				env += "  resources: [environment.yaml]\n"
			}
			input := sources(env)
			for i := 0; i < desiredstate.MaxDiagnostics; i++ {
				input.Files = append(input.Files, desiredstate.NewSourceFile(fmt.Sprintf("/synthetic/bad%04d.yaml", i), []byte("[")))
			}
			state, report, err := compiler().Compile(context.Background(), input)
			if excluded {
				if err != nil || state == nil || report.Counts.FilesSeen != 1001 {
					t.Fatalf("excluded syntax consumed diagnostics: %v", err)
				}
			} else {
				diagnostics := desiredstate.DiagnosticsOf(err)
				if state != nil || report != nil || len(diagnostics) != 1000 || diagnostics[999].Code != "input.limit" || diagnostics[999].Source != nil {
					t.Fatalf("wrong returned diagnostic budget: %d %v", len(diagnostics), err)
				}
				for _, d := range diagnostics[:999] {
					if d.Code != "yaml.syntax" {
						t.Fatalf("lost syntax diagnostics: %+v", d)
					}
				}
			}
		})
	}
}

func TestFatalParserLimitRetainsPriorSyntaxDiagnostics(t *testing.T) {
	input := sources(environmentYAML)
	input.Files = append(input.Files, desiredstate.NewSourceFile("/synthetic/a-invalid.yaml", []byte("[")), desiredstate.NewSourceFile("/synthetic/z-deep.yaml", []byte(strings.Repeat("[", 65)+"x"+strings.Repeat("]", 65))))
	_, _, err := compiler().Compile(context.Background(), input)
	diagnostics := desiredstate.DiagnosticsOf(err)
	if len(diagnostics) != 2 || diagnostics[0].Code != "yaml.syntax" || diagnostics[1].Code != "input.limit" {
		t.Fatalf("prior syntax lost: %+v", diagnostics)
	}
}

func TestInvalidIdentityIsNotRepeatedInDiagnostics(t *testing.T) {
	invalidName := strings.Repeat("synthetic-invalid-identity-", 4000)
	input := sources(strings.Replace(environmentYAML, "name: synthetic", "name: "+invalidName, 1))
	_, _, err := compiler().Compile(context.Background(), input)
	diagnostics := desiredstate.DiagnosticsOf(err)
	if len(diagnostics) != 1 || diagnostics[0].Code != "api.value" || diagnostics[0].Field != "$.metadata.name" || diagnostics[0].Source == nil || diagnostics[0].Object != nil {
		t.Fatalf("malformed identity was echoed or obscured: count%d", len(diagnostics))
	}
}
