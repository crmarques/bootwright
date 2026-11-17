package installationtest

import (
	"context"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/managedos"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/managedos/installfixtures"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const (
	BootImageName = "rhel-9.8-x86_64-boot.iso"
	DVDImageName  = "rhel-9.8-x86_64-dvd.iso"
)

var (
	BootDigest = strings.Repeat("0123456789abcdef", 4)
	DVDDigest  = strings.Repeat("fedcba9876543210", 4)
)

type Media struct {
	Records map[string]installation.MediaRecord
	Reads   int
}

func (m *Media) MediaRecords(context.Context) (map[string]installation.MediaRecord, error) {
	m.Reads++
	return m.Records, nil
}

func LabRecords() map[string]installation.MediaRecord {
	return map[string]installation.MediaRecord{
		BootImageName: {SHA256: BootDigest, Size: 1105199104, Observed: 1105199104, Release: RHELRelease("9.8")},
		DVDImageName:  {SHA256: DVDDigest, Size: 13107200000, Observed: 13107200000, Release: RHELRelease("9.8")},
	}
}

func RHELRelease(version string) managedos.MediaRelease {
	return managedos.MediaRelease{Family: "Red Hat Enterprise Linux", Version: version, Arch: "x86_64"}
}

func LabMedia() *Media { return &Media{Records: LabRecords()} }

func OnlyRequest(t *testing.T, catalog api.Catalog) (installation.Request, installation.Requirements) {
	t.Helper()
	requests, requirements, err := installation.Requests(catalog, "controller", installfixtures.TestContext)
	if err != nil || len(requests) != 1 {
		t.Fatalf("requests = %d (%v)", len(requests), err)
	}
	return requests[0], requirements[0]
}

func PinnedRequest(t *testing.T, catalog api.Catalog) installation.Request {
	t.Helper()
	request, _ := OnlyRequest(t, catalog)
	pinned, err := installation.PinMedia(catalog, request, LabRecords())
	if err != nil {
		t.Fatalf("pinning: %v", diagnostics.Of(err))
	}
	return pinned
}

func RequestOf(t *testing.T, catalog api.Catalog, object string) installation.Request {
	t.Helper()
	requests, _, err := installation.Requests(catalog, "controller", installfixtures.TestContext)
	if err != nil {
		t.Fatalf("requests: %v", diagnostics.Of(err))
	}
	for _, request := range requests {
		if request.Identity.Object == object {
			return request
		}
	}
	t.Fatalf("no request for %s", object)
	return installation.Request{}
}

func Execution(t *testing.T, digest string) (lifecycle.Execution, installation.Request) {
	t.Helper()
	request := PinnedRequest(t, installfixtures.LabCatalog())
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	return lifecycle.Execution{
		Block: reconciliation.Block{
			BlockDefinition: reconciliation.BlockDefinition{ID: "os-install-rhel-01", Request: canonical},
			RequestDigest:   digest,
		},
		Material: installfixtures.FleetMaterial(),
	}, request
}
