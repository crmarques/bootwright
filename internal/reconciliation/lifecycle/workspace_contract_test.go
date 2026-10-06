package lifecycle_test

import (
	"testing"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle/workspacecontract"
)

func TestJourneyWorkspaceHonoursTheWorkspaceContract(t *testing.T) {
	workspacecontract.Verify(t, func(t *testing.T) workspacecontract.Subject {
		host, err := controller.NewInstalledHostIdentity(controller.LinuxInstalledIdentityV1,
			"0123456789abcdef0123456789abcdef", "12345678-1234-5678-9abc-def012345678", "fedcba98-7654-3210-fedc-ba9876543210")
		if err != nil {
			t.Fatal(err)
		}
		workspace, name, err := lifecycle.NewJourneyWorkspace(host)
		if err != nil {
			t.Fatal(err)
		}
		return workspacecontract.Subject{Workspace: workspace, Context: name, Host: host, KeptRuns: lifecycle.JourneyKeptRuns}
	})
}
