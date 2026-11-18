package managedservice

import (
	"errors"
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

const maxEvidenceBytes = 64 << 10

const noun = "managed service"

// Answer is what one listener proved when it was probed. Its meaning belongs
// to the kind: an HTTP status line, a resolved address, or a time reply.
type Answer struct {
	Address     string `json:"address"`
	Answer      string `json:"answer"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Port        int    `json:"port"`
}

// ErrCertificateMismatch marks a listener presenting another certificate than
// the operation bound, which is a definite failure rather than an unknown.
var ErrCertificateMismatch = errors.New("managed service certificate mismatch")

const AnyCertificate = "*"

// Evidence is the bounded result an adapter returns for one managed service.
// It never carries secret material, a material digest, or adapter prose.
type Evidence struct {
	Absent            bool     `json:"absent"`
	Answers           []Answer `json:"answers"`
	Container         string   `json:"container"`
	ContentRoot       bool     `json:"contentRoot"`
	Postcondition     bool     `json:"postcondition"`
	Request           string   `json:"request"`
	StartedAfterFiles bool     `json:"startedAfterFiles"`
	Unit              string   `json:"unit"`
}

func DecodeEvidence(data []byte) (Evidence, error) {
	return decodeEvidence[Evidence](noun, data)
}

func decodeEvidence[E any](noun string, data []byte) (E, error) {
	return reconciliation.DecodeEvidence[E](data, maxEvidenceBytes, noun)
}

func (e Evidence) Observed() Observed {
	return Observed{
		Absent: e.Absent, Answered: len(e.Answers) != 0, Container: e.Container, ContentRoot: e.ContentRoot,
		Postcondition: e.Postcondition, Request: e.Request, StartedAfterFiles: e.StartedAfterFiles, Unit: e.Unit,
	}
}

// Ready proves every probe target answered on the declared socket, and with
// the certificate the operation bound when the request serves one.
func (e Evidence) Ready(request Request, fingerprint string) error {
	answered := make([]string, 0, len(e.Answers))
	for _, answer := range e.Answers {
		if answer.Port != request.Port || answer.Answer == "" {
			return servicePostconditionUnmet.Fail("a managed service listener did not answer its declared socket", "")
		}
		if request.TLS != nil && fingerprint != AnyCertificate && (fingerprint == "" || answer.Fingerprint != fingerprint) {
			return errors.Join(ErrCertificateMismatch, serviceCertificateMismatch.Fail("the managed service presented a different certificate than the operation bound",
				"restore the certificate of Secret "+request.TLS.Secret+" this operation bound, or destroy this apply and apply again with the new one"))
		}
		if request.TLS == nil && answer.Fingerprint != "" {
			return serviceEvidenceMismatch.Fail("the managed service evidence names a certificate its request serves none of", "")
		}
		answered = append(answered, answer.Address)
	}
	slices.Sort(answered)
	if !slices.Equal(slices.Compact(answered), request.ProbeTargets()) {
		return servicePostconditionUnmet.Fail("the managed service did not answer on every declared address", "")
	}
	return nil
}

func ValidatePresence(data []byte, request Request, digest, fingerprint string) error {
	return Presence[Request, Evidence](noun, data, request, digest, fingerprint)
}

func ValidatePartial(data []byte, request Request, digest, fingerprint string) error {
	return Partial[Request, Evidence](noun, data, request, digest, fingerprint)
}

func ValidateUnremoved(data []byte, request Request, digest string) error {
	return Unremoved[Request, Evidence](noun, data, request, digest)
}

func ValidateRemovalUnfinished(data []byte, request Request, digest string) error {
	return RemovalUnfinished[Request, Evidence](noun, data, request, digest)
}

func ValidateAbsence(data []byte, digest string) error {
	return Absence[Request, Evidence](noun, data, digest)
}

func refuse(stop diagnostics.Stop, noun, message string) error {
	return stop.Fail("the "+noun+" "+message, "")
}

// Presence accepts an apply only with positive evidence for the exact frozen
// request: the unit active, the container built from the pinned image and
// started no earlier than every file it runs from was last written, the owned
// root present, and every probe target answering. A daemon keeps what it read
// at its start, so one started before a file was published runs a
// configuration the frozen request no longer describes.
func Presence[R Frozen, E Reported[R]](noun string, data []byte, request R, digest, fingerprint string) error {
	evidence, err := decodeEvidence[E](noun, data)
	if err != nil {
		return err
	}
	observed := evidence.Observed()
	switch {
	case observed.Request != digest:
		return refuse(serviceEvidenceMismatch, noun, "evidence names another request")
	case !observed.Postcondition || observed.Absent:
		return refuse(servicePostconditionUnmet, noun, "did not reach its postcondition")
	case observed.Unit != "active":
		return refuse(servicePostconditionUnmet, noun, "unit is not active")
	case observed.Container != request.Frame().Image:
		return refuse(servicePostconditionUnmet, noun, "runs an image the plan did not freeze")
	case !observed.ContentRoot:
		return refuse(servicePostconditionUnmet, noun, "owns no content root")
	case !observed.StartedAfterFiles:
		return refuse(servicePostconditionUnmet, noun, "started before a file it runs from was last published")
	}
	return evidence.Ready(request, fingerprint)
}

// Partial accepts evidence only when it positively proves this context's own
// service is part way realized: something the frozen request names is present
// while the whole of it, its listeners' answers and its start after its files
// included, is not. The unit, container and content root all carry the
// context in their names and are claimed by its host reservation, so their
// presence is never another context's work, a service all present whose
// listener stays silent is this context's own service not yet ready, and one
// that started before a file it runs from is its own service not yet
// restarted. Evidence claiming the postcondition without the unit active, the
// content root or that start contradicts itself. A partial realization is
// converged by repeating the operation, so it leaves the block failed rather
// than unproved.
func Partial[R Frozen, E Reported[R]](noun string, data []byte, request R, digest, fingerprint string) error {
	evidence, err := decodeEvidence[E](noun, data)
	if err != nil {
		return err
	}
	observed := evidence.Observed()
	switch {
	case observed.Request != digest:
		return refuse(serviceEvidenceMismatch, noun, "evidence names another request")
	case observed.Absent:
		return refuse(servicePostconditionUnmet, noun, "evidence proves a settled state, not a partial one")
	case observed.Postcondition && (observed.Unit != "active" || !observed.ContentRoot || !observed.StartedAfterFiles):
		return refuse(serviceEvidenceMismatch, noun, "evidence claims a postcondition it does not report")
	case observed.Unit == "" && observed.Container == "" && !observed.ContentRoot:
		return refuse(servicePostconditionUnmet, noun, "evidence reports nothing this context owns")
	case Presence[R, E](noun, data, request, digest, fingerprint) == nil:
		return refuse(servicePostconditionUnmet, noun, "evidence proves it complete, not partial")
	}
	return nil
}

// Unremoved accepts observed evidence only when it proves a removal of the
// frozen request took nothing back: this request's presence form reporting the
// unit active, the container of the frozen image and the content root. The
// listeners' answers, the start after its files and the postcondition flag are
// the apply's proof, not anything a removal takes back, so none plays any part.
func Unremoved[R Frozen, E Reported[R]](noun string, data []byte, request R, digest string) error {
	observed, err := removalEvidence[E](noun, data, digest)
	if err != nil {
		return err
	}
	if !unremoved(observed, request.Frame()) {
		return refuse(servicePostconditionUnmet, noun, "no longer holds everything its removal takes back")
	}
	return nil
}

// RemovalUnfinished accepts observed evidence only when it proves a removal of
// the frozen request took back part of what it owns and not the rest: this
// request's presence form reporting a unit, a container or the content root,
// without the whole that Unremoved reads. Each carries the context in its name
// and is claimed by its host reservation, so what is left is this context's
// own and the next attempt converges it. Readiness plays no part here either.
func RemovalUnfinished[R Frozen, E Reported[R]](noun string, data []byte, request R, digest string) error {
	observed, err := removalEvidence[E](noun, data, digest)
	if err != nil {
		return err
	}
	if observed.Unit == "" && observed.Container == "" && !observed.ContentRoot {
		return refuse(servicePostconditionUnmet, noun, "evidence reports nothing its removal takes back")
	}
	if unremoved(observed, request.Frame()) {
		return refuse(servicePostconditionUnmet, noun, "still holds everything its removal takes back")
	}
	return nil
}

// removalEvidence decodes observed evidence for this request in its presence
// form, the only form that reports what a removal has still to take back.
func removalEvidence[E interface{ Observed() Observed }](noun string, data []byte, digest string) (Observed, error) {
	evidence, err := decodeEvidence[E](noun, data)
	if err != nil {
		return Observed{}, err
	}
	observed := evidence.Observed()
	if observed.Request != digest {
		return Observed{}, refuse(serviceEvidenceMismatch, noun, "evidence names another request")
	}
	if observed.Absent {
		return Observed{}, refuse(serviceEvidenceMismatch, noun, "evidence is an absence form, which reports nothing left to take back")
	}
	return observed, nil
}

func unremoved(observed Observed, frame Frame) bool {
	return observed.Unit == "active" && observed.Container == frame.Image && observed.ContentRoot
}

// Absence accepts a removal only with positive absence of everything the
// capability owns.
func Absence[R Frozen, E Reported[R]](noun string, data []byte, digest string) error {
	evidence, err := decodeEvidence[E](noun, data)
	if err != nil {
		return err
	}
	observed := evidence.Observed()
	switch {
	case observed.Request != digest:
		return refuse(serviceEvidenceMismatch, noun, "evidence names another request")
	case !observed.Postcondition || !observed.Absent:
		return refuse(servicePostconditionUnmet, noun, "removal proved no absence")
	case observed.Unit != "" || observed.Container != "" || observed.ContentRoot || observed.StartedAfterFiles || observed.Answered:
		return refuse(servicePostconditionUnmet, noun, "removal left an owned resource behind")
	}
	return nil
}
