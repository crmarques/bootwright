package converge

// VerboseNoLogExtraVar gates the roles' no_log redaction. The YAML side reads it
// as `{{ bootwright_no_log | default(true) | bool }}`, so the string value
// stamped here is coerced via `| bool`: "false" surfaces the otherwise-censored
// task output, and omitting the var entirely lets every gated task default to
// true and redact (the safe default). This mirrors the extra-var idiom in
// scope_destroy.go, where executor-coupling gate variables are composed in the
// bounded-context root with named constants rather than as raw string literals
// in a cobra command, so the value is owned and unit-testable next to the
// service that runs it.
const VerboseNoLogExtraVar = "bootwright_no_log"

// ApplyVerboseExtraVar stamps the verbose-output gate onto a WorkflowPlan. When
// verbose is set it appends bootwright_no_log=false so the YAML gate evaluates
// false and surfaces output normally hidden as "censored due to no_log"
// (secrets, BMC/registry/RHSM/proxy credentials, tokens, generated Ceph keys).
// When verbose is unset it appends nothing, so the roles keep their redacting
// default. It is composed after the apply/destroy scoping extra-vars so the var
// flows to both the dry-run command preview (which reads plan.ExtraVarPairs) and
// the real run (BuildApplyRunOptions copies plan.ExtraVarPairs).
func ApplyVerboseExtraVar(plan *WorkflowPlan, verbose bool) {
	if verbose {
		plan.ExtraVarPairs = append(plan.ExtraVarPairs, VerboseNoLogExtraVar+"=false")
	}
}
