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
	// Presented reports that the update's plan, with these diagnostics, was
	// shown before its confirmation.
	Presented bool
}

// UpdatePlan is what an input update publishes: the admitted input directory,
// the files it copies, the compilation counts and every warning, those of
// admission and that of changed input over a completed apply.
type UpdatePlan struct {
	Context        string
	InputDirectory string
	FilesCopied    int
	Counts         compilation.Counts
	Diagnostics    []diagnostics.Diagnostic
}

// Abandonment is what a deletion abandons of the objects its context owns.
type Abandonment string

const (
	// AbandonsNone is a deletion whose context owns nothing.
	AbandonsNone Abandonment = "none"
	// AbandonsOwned is an acknowledged deletion of a context whose evidence
	// still attributes objects to it, which bootwright status lists.
	AbandonsOwned Abandonment = "owned"
	// AbandonsUnlisted is an acknowledged deletion of a context whose objects
	// cannot be listed, for the plan's Reason.
	AbandonsUnlisted Abandonment = "unlisted"
)

// DeletionPlan is what a deletion removes and abandons: the context in its
// mode, its selected input revision, the host reservations it releases, and
// the objects it abandons, with why they cannot be listed when they cannot.
// Lost reports a context whose directory, and the keyring in it, is gone
// already.
type DeletionPlan struct {
	Context      string
	Mode         Mode
	Revision     string
	Reservations []string
	Abandons     Abandonment
	Reason       string
	Lost         bool
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
