package converge

const VerboseNoLogExtraVar = "bootwright_no_log"

func VerboseNoLogExtraVarPairs(verbose bool) []string {
	if !verbose {
		return nil
	}
	return []string{VerboseNoLogExtraVar + "=false"}
}

func ApplyVerboseExtraVar(plan *WorkflowPlan, verbose bool) {
	plan.ExtraVarPairs = append(plan.ExtraVarPairs, VerboseNoLogExtraVarPairs(verbose)...)
}
