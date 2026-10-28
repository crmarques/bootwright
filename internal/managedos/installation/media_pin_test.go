package installation

import (
	"context"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const (
	bootImageName = "rhel-9.8-x86_64-boot.iso"
	dvdImageName  = "rhel-9.8-x86_64-dvd.iso"
)

var (
	bootDigest = strings.Repeat("0123456789abcdef", 4)
	dvdDigest  = strings.Repeat("fedcba9876543210", 4)
)

// fixtureMedia is a media store reader answering fixed records.
type fixtureMedia struct {
	records map[string]MediaRecord
	reads   int
}

func (m *fixtureMedia) MediaRecords(context.Context) (map[string]MediaRecord, error) {
	m.reads++
	return m.records, nil
}

// labRecords are the store records of the two images the lab fixture names.
func labRecords() map[string]MediaRecord {
	return map[string]MediaRecord{
		bootImageName: {SHA256: bootDigest, Size: 1105199104, Observed: 1105199104},
		dvdImageName:  {SHA256: dvdDigest, Size: 13107200000, Observed: 13107200000},
	}
}

func labMedia() *fixtureMedia { return &fixtureMedia{records: labRecords()} }

// pinnedRequest is the only request a catalog derives, frozen at the lab
// fixture's store records as a plan freezes it.
func pinnedRequest(t *testing.T, catalog api.Catalog) Request {
	t.Helper()
	request, _ := onlyRequest(t, catalog)
	pinned, err := pinMedia(catalog, request, labRecords())
	if err != nil {
		t.Fatalf("pinning: %v", diagnostics.Of(err))
	}
	return pinned
}

func planWith(media MediaRecords, catalog api.Catalog) (lifecycle.CapabilityPlan, error) {
	return New(nil).WithMedia(media).Plan(context.Background(), lifecycle.PlanInput{
		Verb: reconciliation.Apply, State: compilation.NewState(catalog, catalog, nil),
		Controller: "controller", Context: lifecycle.ContextIdentity{Name: testContext},
	})
}

func plannedRequest(t *testing.T, media MediaRecords, catalog api.Catalog) Request {
	t.Helper()
	plan, err := planWith(media, catalog)
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("plan = %+v (%v)", plan, diagnostics.Of(err))
	}
	request, err := DecodeRequest(plan.Definitions[0].Request)
	if err != nil {
		t.Fatalf("decoding: %v", diagnostics.Of(err))
	}
	return request
}

func onlyDiagnostic(t *testing.T, err error) diagnostics.Diagnostic {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) != 1 {
		t.Fatalf("diagnostics = %+v (%v)", reported, err)
	}
	return reported[0]
}

func withoutRecord(name string) *fixtureMedia {
	records := labRecords()
	delete(records, name)
	return &fixtureMedia{records: records}
}

func checksummedImage(checksum string) api.Object {
	return api.NewObject(api.MachineImage, "rhel-9-8-boot", api.Value{}, api.MapValue(
		text("bootMedia", "local-media:"+bootImageName), text("checksum", checksum),
	))
}

func TestPlanFreezesTheStoreRecordsSizeAndDigest(t *testing.T) {
	media := labMedia()
	request := plannedRequest(t, media, labCatalog())
	if request.BootMedia.SHA256 != bootDigest || request.BootMedia.Size != 1105199104 {
		t.Fatalf("boot media = %+v", request.BootMedia)
	}
	if request.TreeMedia == nil || request.TreeMedia.SHA256 != dvdDigest || request.TreeMedia.Size != 13107200000 {
		t.Fatalf("tree media = %+v", request.TreeMedia)
	}
	if media.reads != 1 {
		t.Fatalf("the plan read the store %d times, want once", media.reads)
	}
}

func TestPlanRefusesAnImageMissingFromTheStore(t *testing.T) {
	for name, test := range map[string]struct {
		image, owner, field string
	}{
		"boot": {bootImageName, "MachineImage/rhel-9-8-boot", "spec.bootMedia"},
		"tree": {dvdImageName, "MachineInstallProfile/rhel-9-8", "spec.installer.anaconda.packageSource.hostedTree.fromMedia"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := planWith(withoutRecord(test.image), labCatalog())
			reported := onlyDiagnostic(t, err)
			if reported.Code != "lifecycle.state" ||
				reported.Message != "the host media store holds no image "+test.image+", which "+test.owner+" names in "+test.field {
				t.Fatalf("diagnostic = %+v", reported)
			}
			if !strings.Contains(reported.Remediation, "bootwright media add --name "+test.image+" ") {
				t.Fatalf("remediation = %q", reported.Remediation)
			}
		})
	}
}

func TestPlanRefusesADeclaredChecksumThatDiffers(t *testing.T) {
	declared := strings.Repeat("a", 64)
	_, err := planWith(labMedia(), labCatalog(checksummedImage(declared)))
	reported := onlyDiagnostic(t, err)
	for _, part := range []string{bootDigest, declared, "spec.checksum", "MachineImage/rhel-9-8-boot"} {
		if !strings.Contains(reported.Message, part) {
			t.Fatalf("the message %q does not name %s", reported.Message, part)
		}
	}
	if !strings.Contains(reported.Remediation, "bootwright media add --name "+bootImageName+" --from-file <path> --sha256 "+declared+", or --from-url <url> --sha256 "+declared) {
		t.Fatalf("remediation = %q", reported.Remediation)
	}
}

func TestADeclaredChecksumInAnotherSpellingThatMatchesPins(t *testing.T) {
	request := plannedRequest(t, labMedia(), labCatalog(checksummedImage("SHA256:"+strings.ToUpper(bootDigest))))
	if request.BootMedia.SHA256 != bootDigest {
		t.Fatalf("boot media = %+v", request.BootMedia)
	}
}

func TestPlanRefusesAnImageWhoseBytesNoLongerHaveTheirRecordedSize(t *testing.T) {
	records := labRecords()
	shortened := records[dvdImageName]
	shortened.Observed--
	records[dvdImageName] = shortened
	_, err := planWith(&fixtureMedia{records: records}, labCatalog())
	reported := onlyDiagnostic(t, err)
	if reported.Message != "the host media store's image "+dvdImageName+" no longer has the size its record names" ||
		!strings.Contains(reported.Remediation, "bootwright media add --name "+dvdImageName+" --from-file <path>, or --from-url <url> --sha256 <digest>") {
		t.Fatalf("diagnostic = %+v", reported)
	}
}

func TestPlanRefusesAnImageTheStoreCannotRead(t *testing.T) {
	for name, record := range map[string]MediaRecord{
		"failed":      {Failure: "its record is malformed, not canonical or names another image"},
		"failed size": {SHA256: dvdDigest, Size: 13107200000, Failure: "its file type, owner, permissions or links are unsafe"},
		"no digest":   {Size: 13107200000, Observed: 13107200000},
		"bad digest":  {SHA256: strings.ToUpper(dvdDigest), Size: 13107200000, Observed: 13107200000},
		"empty":       {SHA256: dvdDigest},
	} {
		t.Run(name, func(t *testing.T) {
			records := labRecords()
			records[dvdImageName] = record
			_, err := planWith(&fixtureMedia{records: records}, labCatalog())
			reported := onlyDiagnostic(t, err)
			if reported.Code != "lifecycle.state" || !strings.Contains(reported.Message, dvdImageName) ||
				strings.Contains(reported.Message, "no longer has the size") ||
				(record.Failure != "" && !strings.HasSuffix(reported.Message, ": "+record.Failure)) {
				t.Fatalf("diagnostic = %+v", reported)
			}
			for _, part := range []string{"bootwright media list", "bootwright media delete --name " + dvdImageName,
				"bootwright media add --name " + dvdImageName + " --from-file <path>"} {
				if !strings.Contains(reported.Remediation, part) {
					t.Fatalf("remediation %q does not name %s", reported.Remediation, part)
				}
			}
		})
	}
}

func TestPlanWithoutAMediaReaderRefusesAnInstallation(t *testing.T) {
	_, err := planWith(nil, labCatalog())
	if reported := onlyDiagnostic(t, err); reported.Message != "this executable has no media store reader, so it cannot pin installation media" {
		t.Fatalf("diagnostic = %+v", reported)
	}
	provided := guest()
	provided = provided.WithSpec(provided.Spec().With("os", api.MapValue(field("provided", api.BoolValue(true)))))
	plan, err := planWith(nil, labCatalog(provided))
	if err != nil || len(plan.Definitions) != 0 {
		t.Fatalf("a plan installing nothing = %+v (%v)", plan, diagnostics.Of(err))
	}
}
