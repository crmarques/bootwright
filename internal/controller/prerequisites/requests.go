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
