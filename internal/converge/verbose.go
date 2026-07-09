package converge

const VerboseNoLogExtraVar = "bootwright_no_log"

func ApplyVerboseExtraVar(plan *WorkflowPlan, verbose bool) {
	if verbose {
		plan.ExtraVarPairs = append(plan.ExtraVarPairs, VerboseNoLogExtraVar+"=false")
	}
}
