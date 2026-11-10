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
