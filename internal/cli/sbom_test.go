package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestTheBillOfMaterialsIsOneCycloneDXDocumentInReferenceOrder(t *testing.T) {
	bom := BillOfMaterials{Name: "bootwright", Version: "v1.0.0", Commit: strings.Repeat("a", 40), Source: "clean", Components: []BOMComponent{
		{Ref: "tool:helm@4.3.1", Type: "application", Name: "helm", Version: "4.3.1", Scope: "optional", SHA256: strings.Repeat("b", 64), Distribution: "https://get.helm.sh/helm.tar.gz",
			Properties: []BOMProperty{{Name: "z", Value: "last"}, {Name: "a", Value: "first"}}},
		{Ref: "go:stdlib@1.26.10", Type: "library", Name: "stdlib", Version: "1.26.10", Scope: "required", PURL: "pkg:golang/stdlib@1.26.10"},
	}}
	first, err := cycloneDX(bom)
	if err != nil {
		t.Fatal(err)
	}
	bom.Components[0], bom.Components[1] = bom.Components[1], bom.Components[0]
	second, err := cycloneDX(bom)
	if err != nil || string(first) != string(second) {
		t.Fatalf("the document depends on component order: %v", err)
	}
	var document struct {
		BOMFormat   string `json:"bomFormat"`
		SpecVersion string `json:"specVersion"`
		Components  []struct {
			Ref        string        `json:"bom-ref"`
			Hashes     []cycloneHash `json:"hashes"`
			References []cycloneLink `json:"externalReferences"`
			Properties []BOMProperty `json:"properties"`
		} `json:"components"`
	}
	if err := json.Unmarshal(first, &document); err != nil {
		t.Fatal(err)
	}
	if document.BOMFormat != "CycloneDX" || document.SpecVersion != "1.6" || len(document.Components) != 2 || document.Components[0].Ref != "go:stdlib@1.26.10" {
		t.Fatalf("unexpected document: %s", first)
	}
	helm := document.Components[1]
	if len(helm.Hashes) != 1 || helm.Hashes[0].Algorithm != "SHA-256" || len(helm.References) != 1 || helm.References[0].Type != "distribution" || helm.Properties[0].Name != "a" {
		t.Fatalf("the tool component lost its hash, distribution or property order: %s", first)
	}
	if strings.Contains(string(first), "timestamp") || strings.Contains(string(first), "serialNumber") {
		t.Fatal("the document carries a value that changes between builds")
	}
}

func TestABillOfMaterialsWithoutUniqueReferencesIsRefused(t *testing.T) {
	component := BOMComponent{Ref: "go:stdlib@1.26.10", Type: "library", Name: "stdlib"}
	for name, bom := range map[string]BillOfMaterials{
		"no executable":        {Components: []BOMComponent{component}},
		"no component":         {Name: "bootwright"},
		"a repeated reference": {Name: "bootwright", Components: []BOMComponent{component, component}},
		"an unnamed reference": {Name: "bootwright", Components: []BOMComponent{{Type: "library", Name: "stdlib"}}},
	} {
		if _, err := cycloneDX(bom); err == nil {
			t.Errorf("%s was rendered", name)
		}
	}
}

func TestAComponentsLicenseIsAnSPDXIdOrAnExpressionAndAbsentWhenNone(t *testing.T) {
	bom := BillOfMaterials{Name: "bootwright", Components: []BOMComponent{
		{Ref: "go:a@v1", Type: "library", Name: "a", License: "MIT"},
		{Ref: "go:b@v1", Type: "library", Name: "b", License: "MIT AND Apache-2.0"},
		{Ref: "go:c@v1", Type: "library", Name: "c", License: "Apache-2.0 OR BSD-2-Clause"},
		{Ref: "ansible:d", Type: "library", Name: "d"},
	}}
	data, err := cycloneDX(bom)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Components []map[string]json.RawMessage `json:"components"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"go:a@v1":   `[{"license":{"id":"MIT"}}]`,
		"go:b@v1":   `[{"expression":"MIT AND Apache-2.0"}]`,
		"go:c@v1":   `[{"expression":"Apache-2.0 OR BSD-2-Clause"}]`,
		"ansible:d": "",
	}
	for _, component := range document.Components {
		var ref string
		if err := json.Unmarshal(component["bom-ref"], &ref); err != nil {
			t.Fatal(err)
		}
		licenses, named := component["licenses"]
		var compact bytes.Buffer
		if named {
			if err := json.Compact(&compact, licenses); err != nil {
				t.Fatal(err)
			}
		}
		if got := compact.String(); got != want[ref] || (want[ref] == "") == named {
			t.Errorf("%s names licenses %q, want %q", ref, got, want[ref])
		}
	}
}
