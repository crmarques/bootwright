package compilation_test

import (
	"context"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/desiredstate/yamlstream"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// Normalization may sort a list, but a diagnostic names the authored
// position, the field and the line alike (specs/api.md, YAML streams and
// decoding).
func TestSortedNestedListsKeepAuthoredAndInheritedDiagnosticLocations(t *testing.T) {
	list := "    - name: z-step\n      gates: apply\n      playbook: playbooks/a.yaml\n      target:\n        boundCluster: {}\n      outputs:\n        - name: z-output\n          file: present\n        - name: a-output\n          file: ''\n    - name: a-step\n      gates: apply\n      manifests:\n        - path: manifests/a.yaml\n"
	addon := "apiVersion: bootwright.io/v1alpha1\nkind: ClusterAddon\nmetadata:\n  name: selected\nspec:\n  manifestSet:\n    manifests:\n      - path: manifests/a.yaml\n"
	sortSteps := func(o api.Object, _ api.Catalog) (api.Object, []api.Issue) {
		if o.Kind() != api.ClusterAddon {
			return o, nil
		}
		byName := func(a, b api.Value) int { return strings.Compare(a.Get("name").Text(), b.Get("name").Text()) }
		steps := o.Spec().Get("steps").Items()
		for i := range steps {
			if steps[i].Has("outputs") {
				outputs := steps[i].Get("outputs").Items()
				slices.SortFunc(outputs, byName)
				steps[i] = steps[i].With("outputs", api.ListValue(outputs...))
			}
		}
		slices.SortFunc(steps, byName)
		return o.WithSpec(o.Spec().With("steps", api.ListValue(steps...))), nil
	}
	for _, inherited := range []bool{false, true} {
		t.Run(map[bool]string{false: "authored", true: "inherited"}[inherited], func(t *testing.T) {
			content := environmentYAML
			if inherited {
				content += "  defaults:\n    ClusterAddon:\n      steps:\n" + indent(list, 4)
				content += "---\n" + addon
			} else {
				content += "---\n" + addon + "  steps:\n" + list
			}
			c := compilation.NewCompiler(yamlstream.Parser{}, nil, compilation.Rules{Normalize: sortSteps})
			_, _, err := c.Compile(context.Background(), sources(content))
			if err == nil {
				t.Fatal("invalid references accepted")
			}
			want := strings.Count(strings.Split(content, "file: ''")[0], "\n") + 1
			found := false
			for _, diagnostic := range diagnostics.Of(err) {
				if diagnostic.Object != nil && diagnostic.Object.Kind == string(api.ClusterAddon) && diagnostic.Field == "$.spec.steps[0].outputs[1].file" {
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
				t.Fatalf("expected nested field error: %+v", diagnostics.Of(err))
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
			sink := diagnostics.Of(err)
			if len(sink) != 1 || sink[0].Code != "api.type" || sink[0].Field != "$."+field {
				t.Fatalf("cascaded diagnostics: %+v", sink)
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
		t.Fatalf("valid native map rejected: %+v", diagnostics.Of(err))
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
	sink := diagnostics.Of(err)
	if len(sink) != 1 || sink[0].Field != "$.spec.extraVars" || strings.Contains(sink[0].Message, key) {
		t.Fatalf("native key escaped into diagnostics: count%d", len(sink))
	}
}

func TestMalformedEnvironmentDoesNotEstablishScope(t *testing.T) {
	malformed := "apiVersion: bootwright.io/v1alpha1\nkind: Environment\nmetadata: null\nspec: {}\n"
	_, _, err := compiler().Compile(context.Background(), sources(environmentYAML+"---\n"+malformed))
	sink := diagnostics.Of(err)
	if len(sink) != 1 || sink[0].Code != "api.type" {
		t.Fatalf("wrong Environment cardinality: %+v", sink)
	}
}

// Exactly one Environment is a graph rule, so a missing or repeated one is
// reported as the documented api.invariant rather than a code of its own.
func TestEnvironmentCardinalityIsAGraphInvariant(t *testing.T) {
	second := strings.Replace(environmentYAML, "name: synthetic", "name: second", 1)
	for name, input := range map[string]desiredstate.Sources{
		"none":     {Roots: []string{"/synthetic"}},
		"repeated": sources(environmentYAML + "---\n" + second),
	} {
		_, _, err := compiler().Compile(context.Background(), input)
		if sink := diagnostics.Of(err); len(sink) == 0 || sink[0].Code != "api.invariant" || sink[0].Message != "exactly one Environment is required" {
			t.Fatalf("%s: Environment cardinality = %+v", name, sink)
		}
	}
}

func TestSyntaxDiagnosticBudgetBoundsReturnedDiagnostics(t *testing.T) {
	input := sources(environmentYAML)
	for i := 0; i < desiredstate.MaxDiagnostics; i++ {
		input.Files = append(input.Files, desiredstate.NewSourceFile(fmt.Sprintf("/synthetic/bad%04d.yaml", i), []byte("[")))
	}
	state, report, err := compiler().Compile(context.Background(), input)
	sink := diagnostics.Of(err)
	if state != nil || report != nil || len(sink) != 1000 || sink[999].Code != "input.limit" || sink[999].Source != nil {
		t.Fatalf("wrong returned diagnostic budget: %d %v", len(sink), err)
	}
	for _, d := range sink[:999] {
		if d.Code != "yaml.syntax" {
			t.Fatalf("lost syntax diagnostics: %+v", d)
		}
	}
}

func TestFatalParserLimitRetainsPriorSyntaxDiagnostics(t *testing.T) {
	input := sources(environmentYAML)
	input.Files = append(input.Files, desiredstate.NewSourceFile("/synthetic/a-invalid.yaml", []byte("[")), desiredstate.NewSourceFile("/synthetic/z-deep.yaml", []byte(strings.Repeat("[", 65)+"x"+strings.Repeat("]", 65))))
	_, _, err := compiler().Compile(context.Background(), input)
	sink := diagnostics.Of(err)
	if len(sink) != 2 || sink[0].Code != "yaml.syntax" || sink[1].Code != "input.limit" {
		t.Fatalf("prior syntax lost: %+v", sink)
	}
}

func TestInvalidIdentityIsNotRepeatedInDiagnostics(t *testing.T) {
	invalidName := strings.Repeat("synthetic-invalid-identity-", 4000)
	input := sources(strings.Replace(environmentYAML, "name: synthetic", "name: "+invalidName, 1))
	_, _, err := compiler().Compile(context.Background(), input)
	sink := diagnostics.Of(err)
	if len(sink) != 1 || sink[0].Code != "api.value" || sink[0].Field != "$.metadata.name" || sink[0].Source == nil || sink[0].Object != nil {
		t.Fatalf("malformed identity was echoed or obscured: count%d", len(sink))
	}
}
