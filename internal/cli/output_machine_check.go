package cli

type machineCheckPresentation struct {
	Context string                        `json:"context"`
	Machine string                        `json:"machine"`
	Checks  []machineHardwarePresentation `json:"checks"`
}

type machineHardwarePresentation struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
}

func (machineCheckPresentation) documentedResult() {}

func renderMachineCheck() commandRenderer {
	return commandRenderer{human: func(r *Runner, c renderCall) (bool, error) {
		result := c.result.machineCheck
		if result == nil {
			return false, nil
		}
		if selectedJSON(c.command) {
			presentation := machineCheckPresentation{Context: result.Context, Machine: result.Machine}
			for _, check := range result.Checks {
				presentation.Checks = append(presentation.Checks, machineHardwarePresentation{
					ID: check.ID, Status: check.Status, Summary: escapeDisplayLine(check.Summary),
				})
			}
			return true, writeEnvelope(r.config.Out, commandEnvelope{
				SchemaVersion: "v1alpha1", Command: c.path, OK: true, ExitCode: 0,
				Result: presentation, Diagnostics: []diagnostic{}, Logs: []string{},
			})
		}
		var text display
		var rows [][]string
		for _, check := range result.Checks {
			rows = append(rows, []string{check.ID, check.Status, escapeDisplayLine(check.Summary)})
		}
		text.resourceTable([]string{"CHECK", "STATUS", "OBSERVATION"}, rows, tableStyle{Kind: "machine", Identity: 0, States: []int{1}})
		return true, text.writeTo(r.config.Out)
	}}
}
