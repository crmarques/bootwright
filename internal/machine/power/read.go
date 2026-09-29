package power

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

// A reading names one machine per entry, so its bound is the fleet an
// inspection may cover rather than the single result a power verb returns.
const maxReadingBytes = 64 << 10

// ReadTarget names one Machine, the controller its reading comes from, and the
// operation-scoped files that controller's account is written to. Each target
// carries its own variable names because one run reads many controllers and no
// two may resolve to the same material.
type ReadTarget struct {
	Controller       Controller `json:"controller"`
	Object           string     `json:"object"`
	PasswordVariable string     `json:"passwordVariable"`
	UserVariable     string     `json:"userVariable"`
}

// ReadSurvey is the complete frozen intent for one bounded reading: every
// Machine one placement host reaches a management controller for. It carries
// no secret value and asks for no effect, so repeating it changes nothing.
type ReadSurvey struct {
	Context   string            `json:"context"`
	Placement machine.Placement `json:"placement"`
	Targets   []ReadTarget      `json:"targets"`
	// Version is the survey shape the adapter validates before it reads, so
	// automation never answers a survey it does not understand.
	Version string `json:"version"`
}

// Canonical encodes the survey exactly as the adapter consumes it.
func (s ReadSurvey) Canonical() ([]byte, error) {
	return canonicalBytes(s, "power reading survey")
}

// ReadContentDigest binds a reading to the exact behavior this build
// implements, so evidence returned for one survey shape can never satisfy
// another.
func ReadContentDigest() string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		"bootwright.machine.power-read-v1", ReadImplementation, ReadOperation, ReadVariable,
	}, "\x00")))
	return hex.EncodeToString(digest[:])
}

// readSurveysFor groups the selected Machines by the host that reaches their
// management controllers, so one bounded run answers for every Machine behind
// the same host. A Machine this context resolves no reachable controller for
// is left out of the survey rather than refused: an inspection reports the
// readings it has, and one unresolvable controller never denies the rest.
func readSurveysFor(catalog api.Catalog, contextName string, names []string, owned map[string]machine.OwnershipState) ([]ReadSurvey, error) {
	controllerMachine, err := lifecycle.ControllerMachine(catalog)
	if err != nil {
		return nil, err
	}
	grouped := map[string]ReadSurvey{}
	for _, name := range names {
		object, ok := catalog.Find(api.Machine, name)
		if !ok {
			continue
		}
		controller, placement, _, err := controllerFor(catalog, object, contextName, controllerMachine, owned)
		if err != nil {
			continue
		}
		survey := grouped[placement.Machine]
		survey.Context, survey.Placement, survey.Version = contextName, placement, ReadImplementation
		index := strconv.Itoa(len(survey.Targets))
		survey.Targets = append(survey.Targets, ReadTarget{
			Controller: controller, Object: object.Name(),
			PasswordVariable: "controllerPassword" + index,
			UserVariable:     "controllerUser" + index,
		})
		grouped[placement.Machine] = survey
	}
	surveys := make([]ReadSurvey, 0, len(grouped))
	for _, survey := range grouped {
		surveys = append(surveys, survey)
	}
	slices.SortFunc(surveys, func(x, y ReadSurvey) int {
		return strings.Compare(x.Placement.Machine, y.Placement.Machine)
	})
	return surveys, nil
}

// readMaterials names the operation-scoped file each target's controller
// account is written to. The file name is the target's own, so two Machines
// answered by one credential still read their own copy.
func readMaterials(survey ReadSurvey) []lifecycle.MaterialFile {
	files := make([]lifecycle.MaterialFile, 0, 2*len(survey.Targets))
	for index, target := range survey.Targets {
		position := strconv.Itoa(index)
		files = append(files,
			lifecycle.MaterialFile{Name: "bmc-user-" + position, Part: secrets.UsernamePart, Secret: target.Controller.CredentialsRef, Variable: target.UserVariable},
			lifecycle.MaterialFile{Name: "bmc-password-" + position, Part: secrets.PasswordPart, Secret: target.Controller.CredentialsRef, Variable: target.PasswordVariable},
		)
	}
	return files
}

// readReferences names every declaration one survey needs bound: each
// controller's account, and the placement host's own access.
func readReferences(survey ReadSurvey) []string {
	references := survey.Placement.SecretReferences()
	for _, target := range survey.Targets {
		if !slices.Contains(references, target.Controller.CredentialsRef) {
			references = append(references, target.Controller.CredentialsRef)
		}
	}
	return references
}
