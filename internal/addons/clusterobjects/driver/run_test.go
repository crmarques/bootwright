package driver

import (
	"context"
	"slices"
	"testing"
	"time"

	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
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

// Observation and removal runs are lent no Secret placement material, so an
// additive apply that binds only its own Secrets may rely on that premise.
func TestOnlyAnApplyIsLentSecretPlacementMaterial(t *testing.T) {
	placement := co.SecretPlacement{Secret: "credentials", Namespace: "operator", Name: "credentials", Type: "Opaque", Keys: []co.KeyPart{{Key: "password", Part: "password"}}}
	for _, operation := range []string{"observe", "destroy", "apply"} {
		runner := &capture{}
		execution := lifecycle.Execution{ApplyOperation: "operation", ApplyRegistered: "2026-01-01T00:00:00Z",
			Material:   map[string]secrets.Material{"credentials": secrets.NewMaterial(map[secrets.Part][]byte{secrets.PasswordPart: []byte("password")})},
			LocateTool: func(context.Context, controller.InstalledTool) (string, error) { return "/oc", nil },
			LendProduced: func(context.Context, lifecycle.ProducedInput) (secrets.Material, error) {
				return secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("kubeconfig")}), nil
			}}
		_, err := Run(context.Background(), runner, execution, operation, Config{Placements: []co.SecretPlacement{placement}})
		lent := slices.ContainsFunc(runner.request.Materials, func(file lifecycle.MaterialFile) bool { return file.Secret == "credentials" })
		_, mapped := runner.request.Material["credentials"]
		switch {
		case operation == "destroy" && (err == nil || runner.request.Materials != nil):
			t.Fatalf("a removal run reached the runner: %v", err)
		case operation == "observe" && (err != nil || lent || mapped || runner.request.MaterialValues["secretSources"] != ""):
			t.Fatalf("the observation was lent placement material (%v): %+v", err, runner.request)
		case operation == "apply" && (err != nil || !lent || runner.request.MaterialValues["secretSources"] == ""):
			t.Fatalf("the apply was not lent its placement material (%v)", err)
		}
	}
}
