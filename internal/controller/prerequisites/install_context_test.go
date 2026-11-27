package prerequisites

import (
	"context"
	"errors"
	"path"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/evidence"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
)

type installCheckStore struct {
	fixedStorage
	setupRootInspection
}

func (s installCheckStore) ReadController(ctx context.Context, name string, read func(StorageView) error) error {
	return s.fixedStorage.ReadController(ctx, name, read)
}

type installCheckTools struct {
	TargetToolCatalog
	definitions []ToolDefinition
	complete    bool
}

func (t installCheckTools) Select(_ []evidence.ToolRequest, _ []DependencySource) ([]ToolDefinition, bool, error) {
	return t.definitions, t.complete, nil
}

type installCheckArea struct {
	BundleArea
	entries []BundleEntry
	reads   int
	err     error
}

func (a *installCheckArea) Entries(context.Context) ([]BundleEntry, error) {
	a.reads++
	return a.entries, a.err
}

func TestContextInstallCheckVerifiesEverySelectedManagedExecutable(t *testing.T) {
	const name = "example"
	f := newSetupFixture(t)
	f.prepared()
	objects := agentClusterObjects(api.MapValue())
	catalog := api.NewCatalog(objects)
	f.service.compiler = contextToolCompiler{compilation.NewState(catalog, catalog, nil)}
	selection, err := controller.Select(catalog)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range StageRequirements(StageNativeOf(selection)) {
		f.packages.present[pkg] = true
	}
	digest, err := f.host.identity.PrivateDigest()
	if err != nil {
		t.Fatal(err)
	}
	requests, err := controller.SelectTools(catalog)
	if err != nil {
		t.Fatal(err)
	}
	var definitions []ToolDefinition
	var entries []BundleEntry
	area := &installCheckArea{}
	for _, request := range requests {
		tool := ToolDefinition{Kind: request.Kind, Version: request.Version, Compatibility: request.Compatibility,
			Source: DependencySource{ID: request.Kind + "-" + request.Version, Bytes: 42}, Files: ToolFiles(request)}
		definitions = append(definitions, tool)
		area.entries = append(area.entries, BundleEntry{Path: "sources/" + tool.Source.ID, Size: 42})
		for _, file := range tool.Files {
			entry := BundleEntry{Path: file.Path}
			entries = append(entries, entry)
			if path.Base(file.Path) != "kubectl" {
				area.entries = append(area.entries, entry)
			}
		}
	}
	f.service.options.Tools = installCheckTools{definitions: definitions, complete: true}
	f.service.storage = installCheckStore{
		fixedStorage: fixedStorage{StorageView{
			Context: SetupContext{Name: name},
			State:   HostState{Bindings: []ControllerBinding{{Context: name, Machine: "controller", HostDigest: digest}}},
			OpenBundle: func(_ context.Context, id string) (BundleArea, error) {
				if id != ToolsDigest(definitions) {
					t.Fatalf("looked for %s outside the selected closure", id)
				}
				return area, nil
			},
		}},
		setupRootInspection: setupRootInspection{result: StateRootInspection{
			Required: "a qualified private state root", Observed: "qualified", Status: "ready",
		}},
	}
	report, err := f.service.CheckInstallation(t.Context(), name)
	if codeOf(err) != "preflight.failed" || report == nil || report.Next != stageInvocation(name) || report.Outcome != "not-ready" {
		t.Fatalf("partial installation = %+v, %v", report, err)
	}
	if area.reads != 1 || checkOf(report.Checks, "state-root").Status != "ready" || len(report.Files) != 0 {
		t.Fatalf("partial installation inspected unexpected evidence: reads=%d, report=%+v", area.reads, report)
	}
	var names []string
	for _, tool := range report.Tools {
		names = append(names, tool.Executable)
		if !slices.Equal(tool.SelectedBy, []string{"OpenShiftCluster/cluster"}) || tool.Version == "" {
			t.Fatalf("finding lacks provenance: %+v", tool)
		}
		if tool.Executable == "kubectl" {
			if tool.Status != "not-ready" || tool.Observed != "managed executable missing" {
				t.Fatalf("missing executable: %+v", tool)
			}
		} else if tool.Status != "ready" {
			t.Fatalf("present executable: %+v", tool)
		}
	}
	if !slices.Equal(names, []string{"helm", "oc", "kubectl", "openshift-install"}) {
		t.Fatalf("reported tools = %v", names)
	}

	area.entries = append(area.entries, entries...)
	report, err = f.service.CheckInstallation(t.Context(), name)
	if err != nil || report.Outcome != "ready" || report.Next != "" || area.reads != 2 {
		t.Fatalf("complete installation = %+v, %v, reads=%d", report, err, area.reads)
	}
	area.err = errors.New("could not read the managed tool area")
	report, err = f.service.CheckInstallation(t.Context(), name)
	if !errors.Is(err, area.err) || report.Outcome != "not-ready" || report.Tools[0].Status != "unverified" {
		t.Fatalf("unverified installation = %+v, %v", report, err)
	}
	f.service.options.Tools = installCheckTools{definitions: definitions[:1]}
	report, err = f.service.CheckInstallation(t.Context(), name)
	if codeOf(err) != "preflight.failed" || report.Next != stageInvocation(name) || area.reads != 3 ||
		report.Tools[0].Observed != "managed tool area for the selected releases is unavailable" ||
		report.Tools[1].Observed != "managed source identity missing" {
		t.Fatalf("partial retained selection = %+v, %v, reads=%d", report, err, area.reads)
	}
	if len(f.effects()) != 0 {
		t.Fatalf("installation checks performed effects: %v", f.events)
	}
}
