package installation

import (
	"os"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const projectionSpec = "../../../specs/state-reconciliation.md"

const releaseKernel = "5.14.0-570.12.1.el9_6.x86_64"

func releaseBlock(t *testing.T, request Request, machine, digest string) reconciliation.Block {
	t.Helper()
	request.Identity.Object = machine
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	return reconciliation.Block{
		BlockDefinition: reconciliation.BlockDefinition{ID: BlockID(machine), Request: canonical},
		RequestDigest:   digest,
	}
}

func releaseEvidence(t *testing.T, request Request, digest string, system OSEvidence) []byte {
	t.Helper()
	marker, err := MarkerFor(request, digest)
	if err != nil {
		t.Fatal(err)
	}
	data, err := reconciliation.Freeze(Evidence{
		Address: request.Address, HostKey: "ssh-ed25519 AAAAHOST", Image: true, Marker: string(marker),
		Postcondition: true, Power: "On", Reachable: true, Request: digest, Tree: true, TreeContent: true,
		TreeIdentity: dvdDigest, Network: true, NetworkReboot: true, NetworkDifferences: []string{},
		OS: system, Update: UpdateEvidence{State: updateNone},
	}, "installation evidence")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func projectRelease(t *testing.T, applied api.Catalog, machine string, system OSEvidence) []lifecycle.Projection {
	t.Helper()
	request := pinnedRequest(t, labCatalog())
	digest := strings.Repeat("1", 64)
	projections, err := New(nil).Project(applied, releaseBlock(t, request, machine, digest), releaseEvidence(t, request, digest, system))
	if err != nil {
		t.Fatalf("project = %v", diagnostics.Of(err))
	}
	return projections
}

func versionFragment(version string) api.Value {
	return api.MapValue(field("os", api.MapValue(text("version", version))))
}

func requireRelease(t *testing.T, got []lifecycle.Projection, machine string, observed api.Value, notes ...string) {
	t.Helper()
	holder := lifecycle.ProjectionHolder{Kind: api.Machine, Name: machine, Field: "spec.os.installProfileRef"}
	if len(got) != 1 || got[0].Kind != api.MachineInstallProfile || got[0].Name != "rhel-9-8" || !got[0].Observed.Equal(observed) ||
		!slices.Equal(got[0].Notes, notes) || got[0].Holder == nil || *got[0].Holder != holder {
		t.Fatalf("project = %+v, want MachineInstallProfile/rhel-9-8 observing %+v with notes %q held by %+v", got, observed, notes, holder)
	}
}

func TestInstallationProjectsTheReleaseItsMachineReads(t *testing.T) {
	projections := projectRelease(t, labCatalog(), "rhel-01", OSEvidence{ID: "rhel", Kernel: releaseKernel, VersionID: "9.6"})
	requireRelease(t, projections, "rhel-01", versionFragment("9.6"), "rhel-01: kernel "+releaseKernel)
}

func TestTwoMachinesOfOneProfileProjectTheirOwnReleases(t *testing.T) {
	second := api.NewObject(api.Machine, "rhel-02", api.Value{}, guest().Spec())
	applied := labCatalog(second)
	first := projectRelease(t, applied, "rhel-01", OSEvidence{ID: "rhel", Kernel: releaseKernel, VersionID: "9.8"})
	other := projectRelease(t, applied, "rhel-02", OSEvidence{ID: "rhel", Kernel: releaseKernel, VersionID: "9.6"})
	requireRelease(t, first, "rhel-01", versionFragment("9.8"), "rhel-01: kernel "+releaseKernel)
	requireRelease(t, other, "rhel-02", versionFragment("9.6"), "rhel-02: kernel "+releaseKernel)
}

func TestAReleaseOutsideTheAPIRuleProjectsNoVersion(t *testing.T) {
	for name, test := range map[string]struct {
		system   OSEvidence
		observed api.Value
		notes    []string
	}{
		"another family": {
			OSEvidence{ID: "centos", Kernel: releaseKernel, VersionID: "9.6"}, versionFragment("9.6"),
			[]string{"rhel-01: kernel " + releaseKernel, "rhel-01: /etc/os-release ID centos is not rhel"},
		},
		"a major-only version": {
			OSEvidence{ID: "rhel", Kernel: releaseKernel, VersionID: "9"}, api.MapValue(),
			[]string{"rhel-01: kernel " + releaseKernel, "rhel-01: /etc/os-release VERSION_ID 9 is not <major>.<minor>"},
		},
		"a leading zero": {
			OSEvidence{ID: "rhel", VersionID: "9.06"}, api.MapValue(),
			[]string{"rhel-01: /etc/os-release VERSION_ID 9.06 is not <major>.<minor>"},
		},
		"nothing in os-release": {
			OSEvidence{Kernel: releaseKernel}, api.MapValue(),
			[]string{"rhel-01: kernel " + releaseKernel, "rhel-01: /etc/os-release ID none is not rhel", "rhel-01: /etc/os-release VERSION_ID none is not <major>.<minor>"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			requireRelease(t, projectRelease(t, labCatalog(), "rhel-01", test.system), "rhel-01", test.observed, test.notes...)
		})
	}
}

func TestAnUnreachedOrProvidedMachineProjectsNoRelease(t *testing.T) {
	if got := projectRelease(t, labCatalog(), "rhel-01", OSEvidence{}); len(got) != 0 {
		t.Errorf("an unreached system projected %+v", got)
	}
	provided := guest(field("os", api.MapValue(field("provided", api.BoolValue(true)))))
	if got := projectRelease(t, labCatalog(provided), "rhel-01", OSEvidence{ID: "rhel", Kernel: releaseKernel, VersionID: "9.8"}); len(got) != 0 {
		t.Errorf("a provided OS projected %+v", got)
	}
	if got := projectRelease(t, labCatalog(), "rhel-09", OSEvidence{ID: "rhel", Kernel: releaseKernel, VersionID: "9.8"}); len(got) != 0 {
		t.Errorf("a Machine the applied revision does not declare projected %+v", got)
	}
}

func TestAMalformedReleaseIsRefused(t *testing.T) {
	request := pinnedRequest(t, labCatalog())
	digest := strings.Repeat("1", 64)
	for name, system := range map[string]OSEvidence{
		"an unbounded id":        {ID: strings.Repeat("r", 65)},
		"a version with a space": {ID: "rhel", VersionID: "9 6"},
		"a kernel with a slash":  {ID: "rhel", VersionID: "9.6", Kernel: "5.14/0"},
		"a control character":    {ID: "rhel\n", VersionID: "9.6"},
		"an unbounded kernel":    {Kernel: strings.Repeat("k", 129)},
		"a quoted version":       {ID: "rhel", VersionID: `"9.6"`},
		"a version with a colon": {ID: "rhel", VersionID: "9:6"},
		"an id with a slash":     {ID: "rhel/x"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := New(nil).Project(labCatalog(), releaseBlock(t, request, "rhel-01", digest), releaseEvidence(t, request, digest, system))
			if err == nil {
				t.Fatalf("the release %+v was admitted", system)
			}
		})
	}
}

func TestReleaseObservableFieldTableMatchesProjector(t *testing.T) {
	data, err := os.ReadFile(projectionSpec)
	if err != nil {
		t.Fatal(err)
	}
	_, table, found := strings.Cut(string(data), "### Observable fields by implementation")
	if !found {
		t.Fatal("the spec has no observable-field table")
	}
	var documented []lifecycle.ProjectedPath
	for _, line := range strings.Split(table, "\n") {
		if strings.HasPrefix(line, "## ") {
			break
		}
		cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
		if len(cells) < 3 || strings.Trim(strings.TrimSpace(cells[0]), "`") != Implementation {
			continue
		}
		kind := api.Kind(strings.Trim(strings.TrimSpace(cells[1]), "`"))
		fields, held, _ := strings.Cut(cells[2], ", held by ")
		if !strings.Contains(held, "`Machine` whose `spec.os.installProfileRef`") {
			t.Errorf("the %s row names no Machine holder through spec.os.installProfileRef: %q", Implementation, cells[2])
		}
		for _, path := range strings.Split(fields, ",") {
			documented = append(documented, lifecycle.ProjectedPath{Kind: kind, Path: strings.Trim(strings.TrimSpace(path), "`")})
		}
	}
	declared := New(nil).ProjectedPaths()
	if !slices.Equal(documented, declared) {
		t.Errorf("the table lists %+v, ProjectedPaths returns %+v", documented, declared)
	}
}
