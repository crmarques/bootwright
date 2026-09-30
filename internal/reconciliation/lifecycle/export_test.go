package lifecycle

import (
	"strings"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// NewJourneyWorkspace is the journey double workspacecontract.Verify holds:
// its one ready context, with empty operation and run areas, on host, whose
// completed setup names its approved bundle, binds no context and retains no
// dependency.
func NewJourneyWorkspace(host controller.InstalledHostIdentity) (Workspace, string, error) {
	pristine, err := reconciliation.PristineEvidence().Bytes()
	if err != nil {
		return nil, "", err
	}
	return &testWorkspace{
		area: newArea(), runArea: newArea(), evidence: pristine, revision: testRevision,
		controller: prerequisites.StorageView{Exists: true, Initialized: true, State: prerequisites.HostState{
			Host: host, Receipt: prerequisites.SetupReceipt{Status: "complete", CatalogDigest: strings.Repeat("b", 64)},
		}},
	}, testContextName, nil
}
