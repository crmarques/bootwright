package contexts

import (
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

type InitRequest struct {
	Name              string
	ConfigurationFile string
	InputDirectory    string
}

type UpdateRequest struct {
	ConfigurationFile string
	Name              string
	InputDirectory    string
	SkipConfirmation  bool
}

type UseRequest struct{ Name string }

type ListRequest struct{}

type CurrentRequest struct{ Short bool }

type DeleteRequest struct {
	Name             string
	Purge            bool
	AllowOrphans     bool
	SkipConfirmation bool
}

type Summary struct {
	Name       string
	Mode       Mode
	Current    bool
	Configured bool
}

type AdmissionResult struct {
	Context      Summary
	Counts       compilation.Counts
	FilesCopied  int
	InputChanged bool
	Diagnostics  []diagnostics.Diagnostic
}

type UseResult struct{ Context Summary }

type ListResult struct{ Contexts []Summary }

type CurrentResult struct{ Context Summary }

type DeleteResult struct {
	Name             string
	Outcome          string
	CurrentCleared   bool
	OrphansAbandoned bool
	// ReleasedReservations are the host resource keys the deleted context
	// reserved, which another context may now reserve.
	ReleasedReservations []string
}
