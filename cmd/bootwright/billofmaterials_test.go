package main

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"reflect"
	"runtime/debug"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/runtimelock"
)

func TestTheBillOfMaterialsNamesEveryCarriedAndPinnedArtifact(t *testing.T) {
	stamp, ok := debug.ReadBuildInfo()
	if !ok {
		t.Fatal("the test binary carries no build stamp")
	}
	bom, err := composeBillOfMaterials(cli.BuildInfo{Version: "v1.0.0"}, stamp)
	if err != nil {
		t.Fatal(err)
	}
	again, err := composeBillOfMaterials(cli.BuildInfo{Version: "v1.0.0"}, stamp)
	if err != nil || !reflect.DeepEqual(bom, again) {
		t.Fatalf("two compositions of one build differ: %v", err)
	}
	byRef := map[string]cli.BOMComponent{}
	for _, component := range bom.Components {
		byRef[component.Ref] = component
	}
	want := []string{"ansible:" + collectionName}
	lock, err := runtimelock.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	for _, abi := range lock.PythonABIs() {
		files, _ := lock.Files(abi)
		for _, file := range files {
			want = append(want, "pypi:"+file.Filename)
			if byRef["pypi:"+file.Filename].SHA256 != file.SHA256 {
				t.Errorf("%s does not carry its locked SHA-256", file.Filename)
			}
		}
	}
	want = append(want, "pypi:"+lock.Installer().Filename)
	pins, err := controller.ToolPins()
	if err != nil {
		t.Fatal(err)
	}
	for _, pin := range pins {
		ref := "tool:" + pin.Kind + "@" + pin.Default.Version
		want = append(want, ref)
		if byRef[ref].SHA256 != pin.Default.SHA256 || !strings.HasPrefix(byRef[ref].Distribution, "https://") {
			t.Errorf("%s does not carry its pinned SHA-256 and publisher", ref)
		}
	}
	for _, image := range pinnedImages() {
		want = append(want, "oci:"+image.reference)
		if !strings.Contains(image.reference, "@sha256:") {
			t.Errorf("%s's image %s is not pinned by digest", image.consumer, image.reference)
		}
	}
	for _, ref := range want {
		if _, found := byRef[ref]; !found {
			t.Errorf("the bill of materials does not name %s", ref)
		}
	}
	sum, err := os.ReadFile("../../go.sum")
	if err != nil {
		t.Fatal(err)
	}
	modules := 0
	for _, component := range bom.Components {
		if !strings.HasPrefix(component.Ref, "go:") || component.Name == "stdlib" {
			continue
		}
		modules++
		hash := component.Properties[slices.IndexFunc(component.Properties, func(property cli.BOMProperty) bool { return property.Name == "bootwright:go-sum" })].Value
		if !strings.Contains(string(sum), component.Name+" "+component.Version+" "+hash+"\n") {
			t.Errorf("%s %s %s is not the module go.sum names", component.Name, component.Version, hash)
		}
	}
	if modules == 0 {
		t.Fatal("the bill of materials names no Go module")
	}
}

var reviewedLicenseIDs = []string{"Apache-2.0", "BSD-2-Clause", "BSD-3-Clause", "GPL-2.0-only", "GPL-2.0-or-later", "GPL-3.0-only", "GPL-3.0-or-later", "ISC", "MIT", "MPL-2.0"}

func TestEveryThirdPartyComponentNamesAReviewedSPDXLicense(t *testing.T) {
	stamp, _ := debug.ReadBuildInfo()
	bom, err := composeBillOfMaterials(cli.BuildInfo{}, stamp)
	if err != nil {
		t.Fatal(err)
	}
	firstParty := 0
	for _, component := range bom.Components {
		if strings.HasPrefix(component.Ref, "ansible:") {
			firstParty++
			if component.License != "" {
				t.Errorf("%s is the project's own collection, whose license the project has not declared, and names %s", component.Ref, component.License)
			}
			continue
		}
		if component.License == "" || !runtimelock.SPDXExpression.MatchString(component.License) {
			t.Errorf("%s names the license %q, not a plain SPDX expression", component.Ref, component.License)
		}
		for _, token := range strings.Fields(component.License) {
			if token != "AND" && token != "OR" && token != "WITH" && !slices.Contains(reviewedLicenseIDs, token) {
				t.Errorf("%s names %s, which no one has reviewed; add it to reviewedLicenseIDs after reading its terms", component.Ref, token)
			}
		}
	}
	if firstParty != 1 {
		t.Errorf("the bill of materials names %d first-party collections, want 1", firstParty)
	}
	document, err := runBillOfMaterials()
	if err != nil {
		t.Fatal(err)
	}
	var rendered struct {
		Components []struct {
			Ref      string            `json:"bom-ref"`
			Licenses []json.RawMessage `json:"licenses"`
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(document), &rendered); err != nil {
		t.Fatal(err)
	}
	for _, component := range rendered.Components {
		if len(component.Licenses) == 0 && !strings.HasPrefix(component.Ref, "ansible:") {
			t.Errorf("the rendered document names no license for %s", component.Ref)
		}
	}
}

func TestTheGoLicenseTableHoldsTheTextOfEveryModuleTheBuildNamesAndNoOther(t *testing.T) {
	stamp, _ := debug.ReadBuildInfo()
	table, err := goLicenses()
	if err != nil {
		t.Fatal(err)
	}
	sum, err := os.ReadFile("../../go.sum")
	if err != nil {
		t.Fatal(err)
	}
	named := map[string]bool{"stdlib@": true}
	for _, module := range stamp.Deps {
		named[module.Path+"@"+module.Version] = true
	}
	for key, entry := range table {
		if entry.Text == "" || entry.File == "" || entry.License == "" {
			t.Errorf("%s names no license text; run make licenses", key)
		}
		if !strings.Contains(string(sum), strings.Replace(key, "@", " ", 1)+" h1:") && key != "stdlib@" {
			t.Errorf("go-licenses.json holds %s, which go.sum does not name; run make licenses", key)
		}
	}
	for key := range named {
		if _, found := table[key]; !found {
			t.Errorf("go-licenses.json holds nothing for %s; run make licenses", key)
		}
	}
	if len(table) < 2 {
		t.Fatal("the Go license table is empty")
	}
}

func TestEveryPinnedImageAndToolNamesItsPublishersLicense(t *testing.T) {
	pinned := map[string]bool{}
	for _, image := range pinnedImages() {
		repository, _, _ := strings.Cut(image.reference, "@sha256:")
		pinned[repository] = true
		if license := imageLicenses[repository]; license.license == "" || license.source == "" || !runtimelock.SPDXExpression.MatchString(license.license) {
			t.Errorf("%s's image %s names no license with the source that declares it", image.consumer, repository)
		}
	}
	for repository := range imageLicenses {
		if !pinned[repository] {
			t.Errorf("the image license table names %s, which no service pins", repository)
		}
	}
	pins, err := controller.ToolPins()
	if err != nil {
		t.Fatal(err)
	}
	for _, pin := range pins {
		if pin.License == "" {
			t.Errorf("the tool table names no license for %s", pin.Kind)
		}
	}
}

func TestABillOfMaterialsRefusesWhatItCannotNameExactly(t *testing.T) {
	if _, err := composeBillOfMaterials(cli.BuildInfo{}, nil); err == nil {
		t.Error("an executable with no build stamp composed a bill of materials")
	}
	local := &debug.BuildInfo{Deps: []*debug.Module{{Path: "example.test/module", Version: "v1.0.0", Sum: "h1:x", Replace: &debug.Module{Path: "../module"}}}}
	if _, err := composeBillOfMaterials(cli.BuildInfo{}, local); err == nil {
		t.Error("a local module replacement was named as a released module")
	}
}

func TestEveryLockedWheelIsNamedCarriedOnlyWhenTheExecutableCarriesIt(t *testing.T) {
	stamp, _ := debug.ReadBuildInfo()
	bom, err := composeBillOfMaterials(cli.BuildInfo{}, stamp)
	if err != nil {
		t.Fatal(err)
	}
	carried := runtimelock.Carried()
	for _, component := range bom.Components {
		filename, wheel := strings.CutPrefix(component.Ref, "pypi:")
		if !wheel {
			continue
		}
		_, statErr := fs.Stat(carried, filename)
		if (statErr == nil) != (component.Scope == "required") {
			t.Errorf("%s is %s while the executable carries it: %t", filename, component.Scope, statErr == nil)
		}
	}
}

func TestEveryReviewedExceptionNamesAComponentAndTravelsAsVEX(t *testing.T) {
	stamp, _ := debug.ReadBuildInfo()
	bom, err := composeBillOfMaterials(cli.BuildInfo{}, stamp)
	if err != nil {
		t.Fatal(err)
	}
	refs := map[string]bool{}
	for _, component := range bom.Components {
		refs[component.Ref] = true
	}
	if len(bom.Vulnerabilities) == 0 {
		t.Fatal("the embedded exceptions are empty or were not read")
	}
	for _, vulnerability := range bom.Vulnerabilities {
		if !refs[vulnerability.Ref] {
			t.Errorf("the exception for %s names %s, which the bill of materials does not", vulnerability.ID, vulnerability.Ref)
		}
		if vulnerability.State == "not_affected" && vulnerability.Justification == "" || strings.TrimSpace(vulnerability.Reason) == "" || len(vulnerability.Expires) != 10 {
			t.Errorf("the exception for %s lacks its justification, reason or expiry", vulnerability.ID)
		}
	}
	if _, err := runBillOfMaterials(); err != nil {
		t.Fatalf("the bill of materials with its exceptions did not render: %v", err)
	}
}

func runBillOfMaterials() (string, error) {
	var stdout, stderr strings.Builder
	if code := run(context.Background(), []string{"sbom", "--format", "cyclonedx-json"}, &stdout, &stderr); code != 0 {
		return "", errors.New(stderr.String())
	}
	if !strings.Contains(stdout.String(), `"vulnerabilities"`) {
		return "", errors.New("the document carries no vulnerabilities")
	}
	return stdout.String(), nil
}
