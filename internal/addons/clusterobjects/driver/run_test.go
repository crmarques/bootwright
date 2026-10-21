package driver

import (
	"context"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

type capture struct{ request lifecycle.RunRequest }

func (c *capture) Run(_ context.Context, r lifecycle.RunRequest) (lifecycle.RunResult, error) {
	c.request = r
	return lifecycle.RunResult{}, nil
}
func TestEveryCapabilityDeadlineCoversItsFrozenBudgets(t *testing.T) {
	for _, budget := range []int64{1, 60, 21000} {
		runner := &capture{}
		execution := lifecycle.Execution{ApplyOperation: "operation", ApplyRegistered: "2026-01-01T00:00:00Z", LendProduced: func(context.Context, lifecycle.ProducedInput) (secrets.Material, error) {
			return secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("private")}), nil
		}, LocateTool: func(context.Context, controller.InstalledTool) (string, error) { return "/qualified/oc", nil }}
		_, err := Run(context.Background(), runner, execution, "apply", Config{BudgetSeconds: budget})
		if err != nil {
			t.Fatal(err)
		}
		if got := runner.request.Deadline; got != time.Duration(budget)*time.Second+10*time.Minute || got > lifecycle.MaxDeadline {
			t.Fatal(got)
		}
	}
}
