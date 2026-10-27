package cli

import (
	"io"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type receiptPresentation struct {
	Operation string `json:"operation"`
	Verb      string `json:"verb"`
	State     string `json:"state"`
	Next      string `json:"next"`
}

func displayReceipt(receipt lifecycle.Receipt) receiptPresentation {
	return receiptPresentation{Operation: escapeDisplayLine(receipt.Operation), Verb: escapeDisplayLine(receipt.Verb), State: escapeDisplayLine(receipt.State), Next: escapeDisplayLine(receipt.Next)}
}

type planStepPresentation struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Kind        string   `json:"kind"`
	Object      string   `json:"object"`
	Stage       string   `json:"stage"`
	Impacts     []string `json:"impacts"`
	State       string   `json:"state"`
	Selection   string   `json:"selection"`
	WaitsOn     string   `json:"waitsOn"`
	After       []int    `json:"after"`
	Wave        int      `json:"wave"`
	Consumes    []string `json:"consumes"`
}

func displayPlanSteps(steps []lifecycle.PlanStep) []planStepPresentation {
	shown := make([]planStepPresentation, 0, len(steps))
	for _, s := range steps {
		shown = append(shown, planStepPresentation{ID: escapeDisplayLine(s.ID), Description: escapeDisplayLine(s.Description), Kind: escapeDisplayLine(s.Kind), Object: escapeDisplayLine(s.Object), Stage: escapeDisplayLine(s.Stage), Impacts: displayLines(s.Impacts), State: escapeDisplayLine(s.State), Selection: escapeDisplayLine(s.Selection), WaitsOn: escapeDisplayLine(s.WaitsOn), After: append([]int{}, s.After...), Wave: s.Wave, Consumes: displayLines(s.Consumes)})
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
	Checks           []planCheckPresentation `json:"checks,omitempty"`
	Context          resultContext           `json:"context"`
	Verb             string                  `json:"verb"`
	Steps            []planStepPresentation  `json:"steps"`
	Stages           []string                `json:"stages"`
	Until            string                  `json:"until,omitempty"`
	Waves            int                     `json:"waves"`
	Widest           int                     `json:"widest"`
	Bound            int                     `json:"bound"`
	Startable        int                     `json:"startable"`
	Deferred         int                     `json:"deferred"`
	Continuation     bool                    `json:"continuation"`
	Finalizes        bool                    `json:"finalizes"`
	AnotherBuild     *planBuildPresentation  `json:"anotherBuild,omitempty"`
	Stops            []string                `json:"stops"`
	StopsAfterOnline []string                `json:"stopsAfterOnline"`
	Authorizations   []string                `json:"authorizations,omitempty"`
	Receipt          receiptPresentation     `json:"receipt"`
}

func (planPresentation) documentedResult() {}

func displayPlan(result *lifecycle.PlanResult) planPresentation {
	shown := planPresentation{Context: resultContext{Name: escapeDisplayLine(result.Context.Name), Mode: escapeDisplayLine(result.Context.Mode)}, Verb: escapeDisplayLine(result.Verb), Steps: displayPlanSteps(result.Steps), Stages: displayLines(result.Stages), Until: escapeDisplayLine(result.Until), Waves: result.Waves, Widest: result.Widest, Bound: result.Bound, Startable: result.Startable, Deferred: result.Deferred, Continuation: result.Continuation, Finalizes: result.Finalizes, Stops: displayLines(result.Stops), StopsAfterOnline: displayLines(result.StopsAfterOnline), Authorizations: displayLines(result.Authorizations), Receipt: displayReceipt(result.Receipt)}
	for _, check := range result.Checks {
		shown.Checks = append(shown.Checks, planCheckPresentation{ID: escapeDisplayLine(check.ID), Status: escapeDisplayLine(check.Status), Summary: escapeDisplayLine(check.Summary), Remedy: escapeDisplayLine(check.Remedy)})
	}
	if b := result.AnotherBuild; b != nil {
		shown.AnotherBuild = &planBuildPresentation{RegisteredBy: escapeDisplayLine(b.RegisteredBy), Steps: append([]int{}, b.Steps...)}
	}
	return shown
}

type operationPresentation struct {
	Context   resultContext                `json:"context"`
	Verb      string                       `json:"verb"`
	Steps     []planStepPresentation       `json:"steps"`
	Blocks    []operationBlockPresentation `json:"blocks"`
	Settled   bool                         `json:"settled"`
	Recovered string                       `json:"recovered"`
	Next      string                       `json:"next"`
	Receipt   receiptPresentation          `json:"receipt"`
}

func (operationPresentation) documentedResult() {}

type operationBlockPresentation struct {
	ID          string            `json:"id"`
	Description string            `json:"description"`
	Stage       string            `json:"stage"`
	State       string            `json:"state"`
	Outcome     string            `json:"outcome"`
	Attempts    int               `json:"attempts"`
	Unresolved  *statusUnresolved `json:"unresolved,omitempty"`
}

func displayOperation(result *lifecycle.OperationResult) operationPresentation {
	shown := operationPresentation{Context: resultContext{Name: escapeDisplayLine(result.Context.Name), Mode: escapeDisplayLine(result.Context.Mode)}, Verb: escapeDisplayLine(result.Verb), Steps: displayPlanSteps(result.Steps), Blocks: []operationBlockPresentation{}, Settled: result.Settled, Recovered: escapeDisplayLine(result.Recovered), Next: escapeDisplayLine(result.NextCommand), Receipt: displayReceipt(result.Receipt)}
	for _, b := range result.Blocks {
		block := operationBlockPresentation{ID: escapeDisplayLine(b.ID), Description: escapeDisplayLine(b.Description), Stage: escapeDisplayLine(b.Stage), State: escapeDisplayLine(b.State), Outcome: escapeDisplayLine(b.Outcome), Attempts: b.Attempts}
		if b.Unresolved != nil {
			block.Unresolved = &statusUnresolved{Reason: escapeDisplayLine(b.Unresolved.Reason), Remedy: escapeDisplayLine(b.Unresolved.Remedy)}
		}
		shown.Blocks = append(shown.Blocks, block)
	}
	return shown
}

func writeStructuredOperation(out io.Writer, path string, operation *lifecycle.OperationResult, reported []diagnostic, exitCode int) error {
	return writeStructuredResult(out, path, displayOperation(operation), reported, operation.Logs, exitCode)
}
