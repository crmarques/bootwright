package physicalerase

import (
	"context"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/managedos"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/managedos/installationtest"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// The physical erase boots its image only to clear a disk, so it installs no
// release and the release refusals stay the installation's.
func TestTheEraseDoesNotRefuseTheImageRelease(t *testing.T) {
	records := installationtest.LabRecords()
	for name, record := range records {
		record.Release = managedos.MediaRelease{}
		records[name] = record
	}
	input := planInput(reconciliation.Apply)
	catalog := labCatalog(metalProvider(), eraseMachine(), sshKeySecret(api.MapValue(field("generated", api.MapValue()))))
	input.State = compilation.NewState(catalog, catalog, nil)
	erase, err := New(nil).WithMedia(&installationtest.Media{Records: records}).Plan(context.Background(), input)
	if err != nil || len(erase.Definitions) == 0 {
		t.Fatalf("an erase of images with no release = %+v (%v)", erase, diagnostics.Of(err))
	}
	if _, err := installation.New(nil).WithMedia(&installationtest.Media{Records: records}).Plan(context.Background(), input); err == nil {
		t.Fatal("the installation beside it planned images with no release")
	}
}
