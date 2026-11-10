package preflight

import (
	"context"
	"reflect"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/substrate/capacity"
)

type mirrorFake struct {
	calls    int
	selected string
	files    []MirrorFile
	err      error
}

func (f *mirrorFake) ProbeMirror(_ context.Context, selected string, _ api.Catalog) ([]MirrorFile, error) {
	f.calls++
	f.selected = selected
	return f.files, f.err
}

func mirroredCatalog() api.Catalog {
	return api.NewCatalog([]api.Object{api.NewObject(api.Environment, "lab", api.Value{}, api.MapValue().WithPath(api.StringValue("https://mirror.example.test/tools"), "mirror", "artifacts", "url"))})
}

func mirrorFile(tool, name string, status int, served int64, failure string) MirrorFile {
	return MirrorFile{Tool: tool, URL: "https://mirror.example.test/tools/" + name, Publisher: "https://publisher.example.test/" + name, Bytes: 1024, Status: status, Served: served, Failure: failure}
}

// Preflight reads each mirrored file's size, refuses one the mirror lacks or
// serves at another size, naming what to copy there, and reports one it
// could not read as unknown.
func TestTheControllerStageProvesTheArtifactMirrorHoldsEachFile(t *testing.T) {
	service, _, _, _ := fixture()
	service.Graph = graph{mirroredCatalog()}
	mirror := &mirrorFake{files: []MirrorFile{
		mirrorFile("helm 4.3.1", "helm", 200, 1024, ""),
		mirrorFile("govc 0.52.0", "govc", 404, -1, ""),
		mirrorFile("kubectl 1.36.1", "kubectl", 206, 7, ""),
		mirrorFile("virtctl 1.9.1", "virtctl", 0, -1, "artifact mirror mirror.example.test could not be reached from this host"),
	}}
	service.Mirror = mirror
	result, err := service.Preflight(context.Background(), PreflightRequest{Stages: []string{"controller"}})
	if found := diagnostics.Of(err); result == nil || len(found) != 1 || found[0].Code != "preflight.failed" {
		t.Fatalf("result %+v, error %v", result, err)
	}
	want := []string{
		"controller controller.host-packages passed",
		"https://mirror.example.test/tools/govc controller.artifact-mirror failed",
		"https://mirror.example.test/tools/helm controller.artifact-mirror passed",
		"https://mirror.example.test/tools/kubectl controller.artifact-mirror failed",
		"https://mirror.example.test/tools/virtctl controller.artifact-mirror unknown",
	}
	if got := targets(result.Checks); !reflect.DeepEqual(got, want) {
		t.Fatalf("checks\n%q\nwant\n%q", got, want)
	}
	if mirror.calls != 1 || mirror.selected != "lab" {
		t.Fatalf("probe calls %d for %q", mirror.calls, mirror.selected)
	}
	for _, check := range result.Checks {
		switch check.Target {
		case "https://mirror.example.test/tools/govc":
			if !strings.Contains(check.Reason, "does not hold govc 0.52.0") || check.Next != "copy https://publisher.example.test/govc to https://mirror.example.test/tools/govc, as bootwright render acquisitions --context lab lists, then repeat the preflight" {
				t.Errorf("an absent file: %+v", check)
			}
		case "https://mirror.example.test/tools/kubectl":
			if !strings.Contains(check.Reason, "serves 7 bytes for kubectl 1.36.1, and this build pins 1024") || !strings.HasPrefix(check.Next, "copy https://publisher.example.test/kubectl to ") {
				t.Errorf("a file of another size: %+v", check)
			}
		case "https://mirror.example.test/tools/virtctl":
			if !strings.Contains(check.Reason, "could not be reached") || !strings.Contains(check.Next, "over verified HTTPS") {
				t.Errorf("an unread file: %+v", check)
			}
		}
	}
}

func TestTheArtifactMirrorIsProbedOnlyWhereItIsDeclaredAndNeverInADryRun(t *testing.T) {
	service, _, _, _ := fixture()
	mirror := &mirrorFake{}
	service.Mirror = mirror
	if _, err := service.Preflight(context.Background(), PreflightRequest{Stages: []string{"controller"}}); err != nil || mirror.calls != 0 {
		t.Fatalf("an undeclared mirror was probed %d times (%v)", mirror.calls, err)
	}
	service.Graph = graph{mirroredCatalog()}
	if _, err := service.Preflight(context.Background(), PreflightRequest{Stages: []string{"substrates"}}); err != nil || mirror.calls != 0 {
		t.Fatalf("a stage other than the controller's probed the mirror %d times (%v)", mirror.calls, err)
	}
	result, err := service.Preflight(context.Background(), PreflightRequest{Stages: []string{"controller"}, DryRun: true})
	if mirror.calls != 0 || result == nil || !strings.Contains(strings.Join(targets(result.Checks), "\n"), "controller controller.artifact-mirror "+capacity.Pending) {
		t.Fatalf("a dry run probed %d times or reported %v (%v)", mirror.calls, result, err)
	}
	service.Mirror = nil
	if _, err := service.Preflight(context.Background(), PreflightRequest{Stages: []string{"controller"}}); err != availability.ErrNotImplemented {
		t.Fatalf("a declared mirror without a prober = %v", err)
	}
}

func TestAMirrorThatCannotBeProbedIsUnknownWithItsOwnRemedy(t *testing.T) {
	service, _, _, _ := fixture()
	service.Graph = graph{mirroredCatalog()}
	service.Mirror = &mirrorFake{err: diagnostics.NewFailureWithRemediation("secret.part", "Secret mirror-ca holds no certificate", "", "set it with bootwright secret set")}
	result, _ := service.Preflight(context.Background(), PreflightRequest{Stages: []string{"controller"}})
	found := 0
	for _, check := range result.Checks {
		if check.ID != mirrorCheck {
			continue
		}
		found++
		if check.Status != capacity.Unknown || check.Reason != "Secret mirror-ca holds no certificate" || check.Next != "set it with bootwright secret set" {
			t.Fatalf("an unprobed mirror = %+v", check)
		}
	}
	if found != 1 {
		t.Fatalf("an unprobed mirror reported %d checks", found)
	}
}
