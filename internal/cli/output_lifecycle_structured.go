package cli

import (
	"io"
	"slices"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type receiptPresentation struct {
	Operation string `json:"operation"`
	Verb      string `json:"verb"`
	State     string `json:"state"`
	Next      string `json:"next"`
	Stop      string `json:"stop,omitempty"`
}

func displayReceipt(receipt lifecycle.Receipt) receiptPresentation {
	return receiptPresentation{Operation: escapeDisplayLine(receipt.Operation), Verb: escapeDisplayLine(receipt.Verb), State: escapeDisplayLine(receipt.State), Next: escapeDisplayLine(receipt.Next), Stop: escapeDisplayLine(receipt.Stop)}
}

type planStepPresentation struct {
	ID              string         `json:"id"`
	Description     string         `json:"description"`
	Kind            string         `json:"kind"`
	Object          string         `json:"object"`
	Stage           string         `json:"stage"`
	Impacts         []string       `json:"impacts"`
	State           string         `json:"state"`
	Selection       string         `json:"selection"`
	WaitsOn         string         `json:"waitsOn"`
	After           []int          `json:"after"`
	PreviousFailure *statusFailure `json:"previousFailure,omitempty"`
	Wave            int            `json:"wave"`
	Consumes        []string       `json:"consumes"`
	Retains         []string       `json:"retains,omitempty"`
	LeavesWith      []string       `json:"leavesWith,omitempty"`
	Nothing         string         `json:"nothing,omitempty"`
}

func displayPlanSteps(steps []lifecycle.PlanStep) []planStepPresentation {
	shown := make([]planStepPresentation, 0, len(steps))
	for _, s := range steps {
		shown = append(shown, planStepPresentation{ID: escapeDisplayLine(s.ID), Description: escapeDisplayLine(s.Description), Kind: escapeDisplayLine(s.Kind), Object: escapeDisplayLine(s.Object), Stage: escapeDisplayLine(s.Stage), Impacts: displayLines(s.Impacts), State: escapeDisplayLine(s.State), Selection: escapeDisplayLine(s.Selection), WaitsOn: escapeDisplayLine(s.WaitsOn), After: append([]int{}, s.After...), Wave: s.Wave, PreviousFailure: displayFailure(s.PreviousFailure), Consumes: displayLines(s.Consumes),
			Retains: displayLines(s.Retains), LeavesWith: displayLines(s.LeavesWith), Nothing: escapeDisplayLine(s.Nothing)})
	}
	return shown
}

type planBuildPresentation struct {
	RegisteredBy string `json:"registeredBy"`
	Steps        []int  `json:"steps"`
}

type planCheckPresentation struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
	Remedy  string `json:"remedy"`
}

type planPresentation struct {
	KeepGoing          bool                    `json:"keepGoing,omitempty"`
	Checks             []planCheckPresentation `json:"checks,omitempty"`
	Context            resultContext           `json:"context"`
	Verb               string                  `json:"verb"`
	Steps              []planStepPresentation  `json:"steps"`
	Stages             []string                `json:"stages"`
	Clusters           []string                `json:"clusters,omitempty"`
	Machines           []string                `json:"machines,omitempty"`
	Until              string                  `json:"until,omitempty"`
	Waves              int                     `json:"waves"`
	Widest             int                     `json:"widest"`
	Bound              int                     `json:"bound"`
	Startable          int                     `json:"startable"`
	Later              int                     `json:"later,omitempty"`
	Blocked            int                     `json:"blocked,omitempty"`
	Excluded           int                     `json:"excluded,omitempty"`
	Deferred           int                     `json:"deferred"`
	Continuation       bool                    `json:"continuation"`
	Settled            bool                    `json:"settled,omitempty"`
	Finalizes          bool                    `json:"finalizes"`
	AddsTo             string                  `json:"addsTo,omitempty"`
	Restores           string                  `json:"restores,omitempty"`
	TakesBack          []string                `json:"takesBack,omitempty"`
	Scope              *scopePresentation      `json:"scope,omitempty"`
	AnotherBuild       *planBuildPresentation  `json:"anotherBuild,omitempty"`
	Stops              []string                `json:"stops"`
	StopsAfterOnline   []string                `json:"stopsAfterOnline"`
	AuthorizationSteps []int                   `json:"authorizationSteps,omitempty"`
	Authorizations     []string                `json:"authorizations,omitempty"`
	Receipt            receiptPresentation     `json:"receipt"`
}

func (planPresentation) documentedResult() {}

func displayPlan(result *lifecycle.PlanResult) planPresentation {
	shown := planPresentation{KeepGoing: result.KeepGoing, Context: resultContext{Name: escapeDisplayLine(result.Context.Name), Mode: escapeDisplayLine(result.Context.Mode)}, Verb: escapeDisplayLine(result.Verb), Steps: displayPlanSteps(result.Steps), Stages: displayLines(result.Stages), Until: escapeDisplayLine(result.Until), Waves: result.Waves, Widest: result.Widest, Bound: result.Bound, Startable: result.Startable, Deferred: result.Deferred, Continuation: result.Continuation, Finalizes: result.Finalizes, Settled: result.Settled, Stops: displayLines(result.Stops), StopsAfterOnline: displayLines(result.StopsAfterOnline), Authorizations: displayLines(result.Authorizations), Receipt: displayReceipt(result.Receipt)}
	for _, check := range result.Checks {
		shown.Checks = append(shown.Checks, planCheckPresentation{ID: escapeDisplayLine(check.ID), Status: escapeDisplayLine(check.Status), Summary: escapeDisplayLine(check.Summary), Remedy: escapeDisplayLine(check.Remedy)})
	}
	shown.Clusters, shown.Machines = displayLines(result.Clusters), displayLines(result.Machines)
	shown.Later, shown.Blocked, shown.Excluded = result.Later, result.Blocked, result.Excluded
	shown.AuthorizationSteps = slices.Clone(result.AuthorizationSteps)
	shown.AddsTo, shown.TakesBack, shown.Scope = escapeDisplayLine(result.AddsTo), displayLines(result.TakesBack), displayScope(result.Scope)
	shown.Restores = escapeDisplayLine(result.Restores)
	if b := result.AnotherBuild; b != nil {
		shown.AnotherBuild = &planBuildPresentation{RegisteredBy: escapeDisplayLine(b.RegisteredBy), Steps: append([]int{}, b.Steps...)}
	}
	return shown
}

type operationPresentation struct {
	KeepGoing         bool                         `json:"keepGoing,omitempty"`
	Remaining         []string                     `json:"remaining,omitempty"`
	RetiringWithHosts []string                     `json:"retiringWithHosts,omitempty"`
	Context           resultContext                `json:"context"`
	Verb              string                       `json:"verb"`
	Steps             []planStepPresentation       `json:"steps"`
	Blocks            []operationBlockPresentation `json:"blocks"`
	Settled           bool                         `json:"settled"`
	Recovered         string                       `json:"recovered"`
	Next              string                       `json:"next"`
	Scope             *scopePresentation           `json:"scope,omitempty"`
	Receipt           receiptPresentation          `json:"receipt"`
}

func (operationPresentation) documentedResult() {}

type scopePresentation struct {
	Floor            string   `json:"floor"`
	RemovedSteps     []string `json:"removedSteps"`
	KeptSteps        []string `json:"keptSteps"`
	Residue          []string `json:"residue"`
	KeptPowerTargets []string `json:"keptPowerTargets"`
}

func displayScope(scope *lifecycle.StagedScope) *scopePresentation {
	if scope == nil {
		return nil
	}
	return &scopePresentation{Floor: escapeDisplayLine(scope.Floor), RemovedSteps: displayLines(scope.Removed), KeptSteps: displayLines(scope.Kept), Residue: displayLines(scope.Residue), KeptPowerTargets: displayLines(scope.KeptPowerTargets)}
}

type operationBlockPresentation struct {
	ID          string            `json:"id"`
	Description string            `json:"description"`
	Stage       string            `json:"stage"`
	State       string            `json:"state"`
	Outcome     string            `json:"outcome"`
	Attempts    int               `json:"attempts"`
	Unresolved  *statusUnresolved `json:"unresolved,omitempty"`
	Failure     *statusFailure    `json:"failure,omitempty"`
	WaitsFor    []string          `json:"waitsFor,omitempty"`
}

func displayOperation(result *lifecycle.OperationResult) operationPresentation {
	shown := operationPresentation{KeepGoing: result.KeepGoing, RetiringWithHosts: displayLines(result.RetiringWithHosts), Context: resultContext{Name: escapeDisplayLine(result.Context.Name), Mode: escapeDisplayLine(result.Context.Mode)}, Verb: escapeDisplayLine(result.Verb), Steps: displayPlanSteps(result.Steps), Blocks: []operationBlockPresentation{}, Settled: result.Settled, Recovered: escapeDisplayLine(result.Recovered), Next: escapeDisplayLine(result.NextCommand), Scope: displayScope(result.Scope), Receipt: displayReceipt(result.Receipt)}
	for _, b := range result.Blocks {
		block := operationBlockPresentation{ID: escapeDisplayLine(b.ID), Description: escapeDisplayLine(b.Description), Stage: escapeDisplayLine(b.Stage), State: escapeDisplayLine(b.State), Outcome: escapeDisplayLine(b.Outcome), Attempts: b.Attempts}
		if b.Unresolved != nil {
			block.Unresolved = &statusUnresolved{Reason: escapeDisplayLine(b.Unresolved.Reason), Remedy: escapeDisplayLine(b.Unresolved.Remedy)}
		}
		block.Failure = displayFailure(b.Failure)
		block.WaitsFor = displayLines(b.WaitsFor)
		shown.Blocks = append(shown.Blocks, block)
		if result.KeepGoing && b.State != "done" {
			shown.Remaining = append(shown.Remaining, block.ID)
		}
	}
	return shown
}

func writeStructuredOperation(out io.Writer, path string, operation *lifecycle.OperationResult, reported []diagnostic, exitCode int) error {
	return writeEnvelope(out, commandEnvelope{SchemaVersion: "v1alpha1", Command: escapeDisplayLine(path), OK: exitCode == 0, ExitCode: exitCode, Result: displayOperation(operation),
		Diagnostics: displayDiagnosticsInOrder(append(slices.Clone(operation.Warnings), reported...)), Logs: displayLines(operation.Logs)})
}
