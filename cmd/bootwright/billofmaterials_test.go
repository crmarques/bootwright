package main

import (
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
