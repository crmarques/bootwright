package prerequisites

import (
	"context"
	"errors"
	"testing"

	"github.com/crmarques/bootwright/internal/controller"
)

type failingToolArea struct {
	BundleArea
	failure error
	at      string
}

func (a failingToolArea) Entries(context.Context) ([]BundleEntry, error) {
	if a.at == "entries" {
		return nil, a.failure
	}
	return []BundleEntry{{Path: "tools/helm/helm"}}, nil
}
func (a failingToolArea) Location(context.Context) (BundleLocation, error) {
	return BundleLocation{}, a.failure
}

func TestInstalledToolLookupPreservesStoreFailures(t *testing.T) {
	failure := errors.New("store unavailable")
	for _, at := range []string{"open", "entries", "location"} {
		t.Run(at, func(t *testing.T) {
			view := StorageView{OpenBundle: func(context.Context, string) (BundleArea, error) {
				if at == "open" {
					return nil, failure
				}
				return failingToolArea{failure: failure, at: at}, nil
			}}
			_, err := LocateInstalledTool(context.Background(), view, []ToolDefinition{{Kind: "helm", Version: "v4.2.3", Files: []ToolFile{{Path: "tools/helm/helm"}}}}, controller.InstalledTool{Kind: "helm", Version: "v4.2.3", Executable: "helm"})
			if !errors.Is(err, failure) {
				t.Fatalf("store failure replaced: %v", err)
			}
		})
	}
}

// A latest intent names the one release the proved closure resolved it to,
// and nothing when the closure holds no such client or several of it.
func TestALatestIntentLocatesTheOneReleaseItsClosureResolved(t *testing.T) {
	virtctl := func(version string) ToolDefinition {
		return ToolDefinition{Kind: "virtctl", Compatibility: "kubevirt", Version: version,
			Files: []ToolFile{{Member: "virtctl", Path: "tools/virtctl/kubevirt/" + version + "/virtctl"}}}
	}
	latest := controller.InstalledTool{Kind: "virtctl", Compatibility: "kubevirt", Version: "latest", Executable: "virtctl"}
	if member, found := publishedMember([]ToolDefinition{virtctl("v1.6.2")}, latest); !found || member != "tools/virtctl/kubevirt/v1.6.2/virtctl" {
		t.Fatalf("one release: %q %t", member, found)
	}
	for name, tools := range map[string][]ToolDefinition{"none": {}, "several": {virtctl("v1.6.2"), virtctl("v1.7.0")}} {
		if member, found := publishedMember(tools, latest); found {
			t.Fatalf("%s: located %q", name, member)
		}
	}
}
