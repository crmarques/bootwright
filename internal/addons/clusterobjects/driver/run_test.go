package driver

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
	"github.com/crmarques/bootwright/internal/controller/evidence"
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
		}, LocateTool: func(context.Context, evidence.InstalledTool) (string, error) { return "/qualified/oc", nil }}
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
			LocateTool: func(context.Context, evidence.InstalledTool) (string, error) { return "/oc", nil },
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

func runnable() lifecycle.Execution {
	return lifecycle.Execution{ApplyOperation: "operation", ApplyRegistered: "2026-01-01T00:00:00Z",
		LocateTool: func(context.Context, evidence.InstalledTool) (string, error) { return "/oc", nil },
		LendProduced: func(context.Context, lifecycle.ProducedInput) (secrets.Material, error) {
			return secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("kubeconfig")}), nil
		}}
}

// A run refuses before any adapter starts when it is cancelled, has no
// runtime or custody, names no owning apply, or asks for an operation other
// than apply or observe, and returns the failure of locating the tool or
// lending the cluster's kubeconfig.
func TestARunRefusesWhatItCannotStartAndReturnsWhatItCouldNotLend(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	missing := runnable()
	missing.LendProduced = nil
	noOwner := runnable()
	noOwner.ApplyRegistered = ""
	failure := errors.New("unavailable")
	noTool := runnable()
	noTool.LocateTool = func(context.Context, evidence.InstalledTool) (string, error) { return "", failure }
	noKubeconfig := runnable()
	noKubeconfig.LendProduced = func(context.Context, lifecycle.ProducedInput) (secrets.Material, error) {
		return secrets.Material{}, failure
	}
	for name, test := range map[string]struct {
		ctx       context.Context
		runner    Runner
		execution lifecycle.Execution
		operation string
		check     func(error) bool
	}{
		"a cancelled run":       {cancelled, &capture{}, runnable(), "apply", func(err error) bool { return errors.Is(err, context.Canceled) }},
		"no runner":             {context.Background(), nil, runnable(), "apply", func(err error) bool { return stopCode(t, err) == clusterobjectsRuntimeUnavailable.Code }},
		"no produced custody":   {context.Background(), &capture{}, missing, "apply", func(err error) bool { return stopCode(t, err) == clusterobjectsRuntimeUnavailable.Code }},
		"no owning apply":       {context.Background(), &capture{}, noOwner, "apply", func(err error) bool { return stopCode(t, err) == clusterobjectsIdentityUnavailable.Code }},
		"an unsupported verb":   {context.Background(), &capture{}, runnable(), "destroy", func(err error) bool { return stopCode(t, err) == clusterobjectsOperationUnsupported.Code }},
		"a tool not located":    {context.Background(), &capture{}, noTool, "observe", func(err error) bool { return errors.Is(err, failure) }},
		"a kubeconfig not lent": {context.Background(), &capture{}, noKubeconfig, "observe", func(err error) bool { return errors.Is(err, failure) }},
	} {
		runner := &capture{}
		if test.runner != nil {
			test.runner = runner
		}
		if _, err := Run(test.ctx, test.runner, test.execution, test.operation, Config{}); err == nil || !test.check(err) || runner.request.Operation != "" {
			t.Errorf("%s = %v, ran %q", name, err, runner.request.Operation)
		}
	}
}

// A run carries the receipts of the previous evidence of its own request into
// the adapter and refuses previous evidence of another request.
func TestARunCarriesOnlyItsOwnPreviousReceipts(t *testing.T) {
	const digest, version = "digest", "cluster-objects-evidence-v1"
	receipt := co.Receipt{Ref: co.Ref{APIVersion: "v1", Kind: "ConfigMap", Namespace: "operators", Name: "settings", Namespaced: true}, UID: "uid", DesiredDigest: "desired", Disposition: "created", Basis: co.Created}
	previous, err := co.Canonical(Evidence{Evidence: co.Evidence{Receipts: []co.Receipt{receipt}}, Request: digest, Version: version})
	if err != nil {
		t.Fatal(err)
	}
	execution := runnable()
	execution.PreviousEvidence = previous
	execution.Block.RequestDigest = digest
	runner := &capture{}
	if _, err := Run(context.Background(), runner, execution, "observe", Config{EvidenceVersion: version}); err != nil {
		t.Fatal(err)
	}
	if carried := runner.request.MaterialValues["evidence"]; !containsAll(carried, "\"uid\"", "settings") {
		t.Fatalf("receipts carried = %q", carried)
	}
	execution.Block.RequestDigest = "another"
	runner = &capture{}
	if _, err := Run(context.Background(), runner, execution, "observe", Config{EvidenceVersion: version}); stopCode(t, err) != clusterobjectsEvidenceMismatch.Code || runner.request.Operation != "" {
		t.Fatalf("another request's previous evidence = %v, ran %q", err, runner.request.Operation)
	}
}

func containsAll(text string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(text, part) {
			return false
		}
	}
	return true
}

// An apply refuses a Secret placement whose source is not bound to the
// operation and one that does not map exactly the parts of its source, before
// any adapter starts.
func TestAnApplyRefusesAPlacementItsSecretDoesNotFit(t *testing.T) {
	placement := co.SecretPlacement{Secret: "credentials", Namespace: "operator", Name: "credentials", Type: "Opaque", Keys: []co.KeyPart{{Key: "password", Part: "password"}}}
	for name, test := range map[string]struct {
		material map[string]secrets.Material
		code     string
	}{
		"an unbound source": {nil, clusterobjectsPlacementUnbound.Code},
		"another part":      {map[string]secrets.Material{"credentials": secrets.NewMaterial(map[secrets.Part][]byte{secrets.UsernamePart: []byte("user")})}, clusterobjectsPlacementUnmapped.Code},
	} {
		execution := runnable()
		execution.Material = test.material
		runner := &capture{}
		if _, err := Run(context.Background(), runner, execution, "apply", Config{Placements: []co.SecretPlacement{placement}}); stopCode(t, err) != test.code || runner.request.Operation != "" {
			t.Errorf("%s = %v, ran %q", name, err, runner.request.Operation)
		}
	}
}
