package managedservice

import (
	"bytes"
	"encoding/json"
	"slices"
)

const maxEvidenceBytes = 64 << 10

// Answer is what one listener proved when it was probed. Its meaning belongs
// to the kind: an HTTP status line, a resolved address, or a time reply.
type Answer struct {
	Address string `json:"address"`
	Answer  string `json:"answer"`
	Port    int    `json:"port"`
}

// Evidence is the bounded result an adapter returns for one managed service.
// It never carries secret material, a material digest, or adapter prose.
type Evidence struct {
	Absent        bool     `json:"absent"`
	Answers       []Answer `json:"answers"`
	Container     string   `json:"container"`
	ContentRoot   bool     `json:"contentRoot"`
	Postcondition bool     `json:"postcondition"`
	Request       string   `json:"request"`
	Unit          string   `json:"unit"`
}

func DecodeEvidence(data []byte) (Evidence, error) {
	if len(data) == 0 || len(data) > maxEvidenceBytes {
		return Evidence{}, Refusal("lifecycle.state", "the managed service adapter returned no bounded evidence", "")
	}
	var evidence Evidence
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&evidence); err != nil || len(bytes.Trim(data[decoder.InputOffset():], " \t\r\n")) != 0 {
		return Evidence{}, Refusal("lifecycle.state", "the managed service adapter returned malformed evidence", "")
	}
	return evidence, nil
}

// ValidatePresence accepts an apply only with positive evidence for the exact
// frozen request: the unit active, the container built from the pinned image,
// the owned root present, and every probe target answering.
func ValidatePresence(data []byte, request Request, digest string) error {
	evidence, err := DecodeEvidence(data)
	if err != nil {
		return err
	}
	if evidence.Request != digest {
		return Refusal("lifecycle.state", "the managed service evidence names another request", "")
	}
	if !evidence.Postcondition || evidence.Absent {
		return Refusal("lifecycle.state", "the managed service did not reach its postcondition", "")
	}
	if evidence.Unit != "active" {
		return Refusal("lifecycle.state", "the managed service unit is not active", "")
	}
	if evidence.Container != request.Image {
		return Refusal("lifecycle.state", "the managed service runs an image the plan did not freeze", "")
	}
	if !evidence.ContentRoot {
		return Refusal("lifecycle.state", "the managed service owns no content root", "")
	}
	answered := make([]string, 0, len(evidence.Answers))
	for _, answer := range evidence.Answers {
		if answer.Port != request.Port || answer.Answer == "" {
			return Refusal("lifecycle.state", "a managed service listener did not answer its declared socket", "")
		}
		answered = append(answered, answer.Address)
	}
	slices.Sort(answered)
	if !slices.Equal(slices.Compact(answered), request.ProbeTargets()) {
		return Refusal("lifecycle.state", "the managed service did not answer on every declared address", "")
	}
	return nil
}

// ValidatePartial accepts evidence only when it positively proves this
// context's own service is part way realized: something the frozen request
// names is present while the whole of it, its listeners' answers included, is
// not. The unit, container and content root all carry the context in their
// names and are claimed by its host reservation, so their presence is never
// another context's work, and a service all present whose listener stays
// silent is this context's own service not yet ready. A partial realization is
// converged by repeating the operation, so it leaves the block failed rather
// than unproved.
func ValidatePartial(data []byte, request Request, digest string) error {
	evidence, err := DecodeEvidence(data)
	if err != nil {
		return err
	}
	if evidence.Request != digest {
		return Refusal("lifecycle.state", "the managed service evidence names another request", "")
	}
	if evidence.Absent {
		return Refusal("lifecycle.state", "the managed service evidence proves a settled state, not a partial one", "")
	}
	if evidence.Postcondition && (evidence.Unit != "active" || !evidence.ContentRoot) {
		return Refusal("lifecycle.state", "the managed service evidence claims a postcondition it does not report", "")
	}
	if evidence.Unit == "" && evidence.Container == "" && !evidence.ContentRoot {
		return Refusal("lifecycle.state", "the managed service evidence reports nothing this context owns", "")
	}
	if ValidatePresence(data, request, digest) == nil {
		return Refusal("lifecycle.state", "the managed service evidence proves it complete, not partial", "")
	}
	return nil
}

// ValidateUnremoved accepts observed evidence only when it proves a removal of
// the frozen request took nothing back: this request's presence form reporting
// the unit active, the container of the frozen image and the content root.
// The listeners' answers and the postcondition flag are the apply's proof of
// readiness, not anything a removal takes back, so neither plays any part.
func ValidateUnremoved(data []byte, request Request, digest string) error {
	evidence, err := removalEvidence(data, digest)
	if err != nil {
		return err
	}
	if !unremoved(evidence, request) {
		return Refusal("lifecycle.state", "the managed service no longer holds everything its removal takes back", "")
	}
	return nil
}

// ValidateRemovalUnfinished accepts observed evidence only when it proves a
// removal of the frozen request took back part of what it owns and not the
// rest: this request's presence form reporting a unit, a container or the
// content root, without the whole that ValidateUnremoved reads. Each carries
// the context in its name and is claimed by its host reservation, so what is
// left is this context's own and the next attempt converges it. Readiness
// plays no part here either.
func ValidateRemovalUnfinished(data []byte, request Request, digest string) error {
	evidence, err := removalEvidence(data, digest)
	if err != nil {
		return err
	}
	if evidence.Unit == "" && evidence.Container == "" && !evidence.ContentRoot {
		return Refusal("lifecycle.state", "the managed service evidence reports nothing its removal takes back", "")
	}
	if unremoved(evidence, request) {
		return Refusal("lifecycle.state", "the managed service still holds everything its removal takes back", "")
	}
	return nil
}

// removalEvidence decodes observed evidence for this request in its presence
// form, the only form that reports what a removal has still to take back.
func removalEvidence(data []byte, digest string) (Evidence, error) {
	evidence, err := DecodeEvidence(data)
	if err != nil {
		return Evidence{}, err
	}
	if evidence.Request != digest {
		return Evidence{}, Refusal("lifecycle.state", "the managed service evidence names another request", "")
	}
	if evidence.Absent {
		return Evidence{}, Refusal("lifecycle.state", "the managed service evidence is an absence form, which reports nothing left to take back", "")
	}
	return evidence, nil
}

func unremoved(evidence Evidence, request Request) bool {
	return evidence.Unit == "active" && evidence.Container == request.Image && evidence.ContentRoot
}

// ValidateAbsence accepts a removal only with positive absence of everything
// the capability owns.
func ValidateAbsence(data []byte, digest string) error {
	evidence, err := DecodeEvidence(data)
	if err != nil {
		return err
	}
	if evidence.Request != digest {
		return Refusal("lifecycle.state", "the managed service evidence names another request", "")
	}
	if !evidence.Postcondition || !evidence.Absent {
		return Refusal("lifecycle.state", "the managed service removal proved no absence", "")
	}
	if evidence.Unit != "" || evidence.Container != "" || evidence.ContentRoot || len(evidence.Answers) != 0 {
		return Refusal("lifecycle.state", "the managed service removal left an owned resource behind", "")
	}
	return nil
}
