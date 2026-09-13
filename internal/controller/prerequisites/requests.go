package prerequisites

type CheckRequest struct{ ContextName string }

type SetupRequest struct {
	ContextName      string
	DryRun           bool
	SkipConfirmation bool
}

type Check struct {
	ID       string
	Required string
	Observed string
	Status   string
}

// Summary is what the operator reads beside a check: the observation when it
// holds, and the requirement beside the observation when it does not, because
// the difference is the actionable part.
func (c Check) Summary() string {
	if c.Status == "ready" {
		return c.Observed
	}
	return "required " + c.Required + "; observed " + c.Observed
}

type Report struct {
	ContextName   string
	Machine       string
	Platform      Platform
	DryRun        bool
	Outcome       string
	Checks        []Check
	Actions       []string
	Dependencies  []string
	PlanPresented bool
	// ProgressPresented records that progress rows were already streamed, so a
	// failure report adds only its outcome rather than repeating the headline.
	ProgressPresented bool
	Progress          []ActionProgress
}
