package power

import (
	"context"
	"strings"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
)

const CheckOperation = "hardware_check"

type CheckRequest struct {
	ContextName string
	Name        string
}

type HardwareCheck struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
}

type CheckResult struct {
	Context string
	Machine string
	Checks  []HardwareCheck
}

type checkSurvey struct {
	Context      api.ContextName   `json:"context"`
	ExpectedMACs []string          `json:"expectedMACs"`
	Placement    machine.Placement `json:"placement"`
	Targets      []ReadTarget      `json:"targets"`
	Version      string            `json:"version"`
}

type checkEvidence struct {
	Request string          `json:"request"`
	Machine string          `json:"machine"`
	Checks  []HardwareCheck `json:"checks"`
}

func (s Service) Check(ctx context.Context, request CheckRequest) (*CheckResult, error) {
	if s.state == nil || s.runner == nil || s.runtime == nil {
		return nil, availability.ErrNotImplemented
	}
	name, err := machine.SelectedContext(ctx, s.selection, request.ContextName)
	if err != nil {
		return nil, err
	}
	state, err := s.state.RenderEffective(ctx, compilation.EffectiveRequest{ContextName: name})
	if err != nil {
		return nil, err
	}
	if err := undeclared(state.Effective, name, []string{request.Name}); err != nil {
		return nil, err
	}
	object, _ := state.Effective.Find(api.Machine, request.Name)
	controller, err := lifecycle.ControllerMachine(state.Effective)
	if err != nil {
		return nil, err
	}
	target, err := managedTarget(state.Effective, object, name, controller)
	if err != nil {
		return nil, err
	}
	if !target.Physical || !target.Controller.TLSVerify || target.Controller.TLSVerification == "none" {
		return nil, powerRequestInvalid.Fail("hardware check requires a physical Machine with verified management TLS", "select a physical Machine whose BMC has verified TLS")
	}
	surveys, err := readSurveysFor(state.Effective, name, []string{request.Name}, nil, nil)
	if err != nil {
		return nil, err
	}
	if len(surveys) != 1 || len(surveys[0].Targets) != 1 {
		return nil, powerRequestInvalid.Fail("the named Machine has no reachable management placement", "correct its BMC and controller placement")
	}
	survey := surveys[0]
	if pinned := s.pinned(ctx, name); pinned != nil {
		pin, err := pinned(request.Name)
		if err != nil {
			return nil, err
		}
		survey.Targets[0].pin = pin
		survey.Targets[0].MachinePort = target.MachinePort("0")
	}
	macs := []string{}
	for _, nic := range substrate.MachineNICs(object) {
		if mac := substrate.MachineMAC(object, nic); mac != "" {
			macs = append(macs, strings.ToLower(mac))
		}
	}
	return s.checkHardware(ctx, name, survey, macs)
}

func (s Service) checkHardware(ctx context.Context, name string, survey ReadSurvey, macs []string) (*CheckResult, error) {
	object := survey.Targets[0].Object
	repeat := "bootwright machine check --context " + name + " --name " + object
	if err := refuseSurveyTemplateValues(name, repeat, []ReadSurvey{survey}); err != nil {
		return nil, err
	}
	canonical, err := reconciliation.Freeze(checkSurvey{
		Context: survey.Context, ExpectedMACs: macs, Placement: survey.Placement,
		Targets: survey.Targets, Version: survey.Version,
	}, "hardware check")
	if err != nil {
		return nil, err
	}
	digest, err := reconciliation.RequestDigest(reconciliation.BlockDefinition{
		Kind: "Machine", Object: object, Implementation: ReadImplementation,
		ContentDigest: reconciliation.ContentDigestOf(ReadContentDigest(), CheckOperation), Request: canonical,
	})
	if err != nil {
		return nil, err
	}
	var evidence checkEvidence
	err = s.runtime.WithRuntime(ctx, lifecycle.RuntimeRequest{ContextName: name, Secrets: readReferences(survey)}, func(inner context.Context, runtime lifecycle.Runtime) error {
		run := readInvocation(runtime, survey, canonical, digest, nil)
		run.Operation, run.Repeat, run.Description = CheckOperation, repeat, "Check physical machine hardware"
		run.Deadline = 3 * time.Minute
		run.ReadOnly, run.Unrecorded = true, true
		result, err := s.runner.Run(inner, run)
		if err != nil {
			return err
		}
		evidence, err = reconciliation.DecodeEvidence[checkEvidence](result.Evidence, 16<<10, "hardware check")
		return err
	})
	if err != nil {
		return nil, err
	}
	if evidence.Request != digest || evidence.Machine != object || !validHardwareChecks(evidence.Checks) {
		return nil, powerEvidenceMismatch.Fail("hardware check evidence does not match the requested Machine or observations", repeat)
	}
	return &CheckResult{Context: name, Machine: object, Checks: evidence.Checks}, nil
}

func validHardwareChecks(checks []HardwareCheck) bool {
	ids := []string{"management-tls", "identity", "power", "virtual-media", "firmware", "direct-disks", "logical-volumes", "active-rehearsal"}
	if len(checks) != len(ids) {
		return false
	}
	for i, check := range checks {
		if check.ID != ids[i] || len(check.Summary) == 0 || len(check.Summary) > 512 {
			return false
		}
		switch check.Status {
		case "observed", "mismatch", "unknown", "requires-exercise":
		default:
			return false
		}
	}
	return true
}
