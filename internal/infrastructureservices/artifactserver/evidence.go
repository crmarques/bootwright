package artifactserver

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
)

// Evidence is the only result shape the adapter may return. Go validates it
// strictly: an adapter cannot widen a postcondition, invent a listener or
// report a certificate the operation did not bind.
type Evidence struct {
	Absent        bool               `json:"absent"`
	Container     string             `json:"container"`
	ContentRoot   bool               `json:"contentRoot"`
	Listeners     []ListenerEvidence `json:"listeners"`
	Postcondition bool               `json:"postcondition"`
	Request       string             `json:"request"`
	Unit          string             `json:"unit"`
}

type ListenerEvidence struct {
	Address     string `json:"address"`
	Fingerprint string `json:"fingerprint"`
	Name        string `json:"name"`
	Port        int    `json:"port"`
	Protocol    string `json:"protocol"`
	Status      string `json:"status"`
}

// ProbeTarget is one socket readiness must positively answer on. A wildcard
// bind is proved through every endpoint address it serves, because that is
// what a consumer will actually connect to.
type ProbeTarget struct {
	Address  string
	Name     string
	Port     int
	Protocol string
}

func (r Request) ProbeTargets() []ProbeTarget {
	var targets []ProbeTarget
	wildcard := r.BindAddress == "0.0.0.0" || r.BindAddress == "::"
	for _, listener := range r.Listeners {
		addresses := []string{r.BindAddress}
		if wildcard {
			addresses = nil
			for _, endpoint := range r.Endpoints {
				if endpoint.Listener == listener.Name && !slices.Contains(addresses, endpoint.Address) {
					addresses = append(addresses, endpoint.Address)
				}
			}
			slices.Sort(addresses)
		}
		for _, address := range addresses {
			targets = append(targets, ProbeTarget{Address: address, Name: listener.Name, Port: listener.Port, Protocol: listener.Protocol})
		}
	}
	slices.SortFunc(targets, func(x, y ProbeTarget) int {
		if order := strings.Compare(x.Name, y.Name); order != 0 {
			return order
		}
		return strings.Compare(x.Address, y.Address)
	})
	return targets
}

func decodeEvidence(data []byte) (Evidence, error) {
	if len(data) == 0 || len(data) > maxEvidenceBytes {
		return Evidence{}, refusal("lifecycle.state", "the artifact-server adapter returned no bounded evidence", "")
	}
	var evidence Evidence
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&evidence); err != nil {
		return Evidence{}, refusal("lifecycle.state", "the artifact-server adapter returned malformed evidence", "")
	}
	if decoder.More() {
		return Evidence{}, refusal("lifecycle.state", "the artifact-server adapter returned trailing evidence", "")
	}
	return evidence, nil
}

// ValidatePresence accepts evidence only when it proves the exact frozen
// request is running and serving the bound certificate on every target.
func ValidatePresence(data []byte, request Request, digest, fingerprint string) error {
	evidence, err := decodeEvidence(data)
	if err != nil {
		return err
	}
	if evidence.Request != digest {
		return refusal("lifecycle.state", "the artifact-server evidence names another request", "")
	}
	if !evidence.Postcondition || evidence.Absent {
		return refusal("lifecycle.state", "the artifact-server adapter did not prove its postcondition", "")
	}
	if evidence.Unit != "active" {
		return refusal("lifecycle.state", "the artifact-server unit is not active", "")
	}
	if evidence.Container != request.Image {
		return refusal("lifecycle.state", "the running artifact-server container is not the frozen image", "")
	}
	if !evidence.ContentRoot {
		return refusal("lifecycle.state", "the artifact-server content root is missing", "")
	}
	targets := request.ProbeTargets()
	if len(evidence.Listeners) != len(targets) {
		return refusal("lifecycle.state", "the artifact-server evidence does not cover every listener", "")
	}
	observed := slices.Clone(evidence.Listeners)
	slices.SortFunc(observed, func(x, y ListenerEvidence) int {
		if order := strings.Compare(x.Name, y.Name); order != 0 {
			return order
		}
		return strings.Compare(x.Address, y.Address)
	})
	for index, target := range targets {
		listener := observed[index]
		if listener.Name != target.Name || listener.Address != target.Address || listener.Port != target.Port || listener.Protocol != target.Protocol {
			return refusal("lifecycle.state", "the artifact-server evidence does not match its frozen listeners", "")
		}
		if !strings.HasPrefix(listener.Status, "HTTP/1.") {
			return refusal("lifecycle.state", "an artifact-server listener did not answer with an HTTP status line", "")
		}
		if target.Protocol == "https" && listener.Fingerprint != fingerprint {
			return refusal("lifecycle.state", "an artifact-server listener presented a different certificate than the operation bound", "")
		}
		if target.Protocol == "http" && listener.Fingerprint != "" {
			return refusal("lifecycle.state", "an HTTP artifact-server listener reported a certificate", "")
		}
	}
	return nil
}

// ValidateAbsence accepts evidence only when it positively proves that every
// owned resource is gone, never merely that it was not observed.
func ValidateAbsence(data []byte, digest string) error {
	evidence, err := decodeEvidence(data)
	if err != nil {
		return err
	}
	if evidence.Request != digest {
		return refusal("lifecycle.state", "the artifact-server evidence names another request", "")
	}
	if !evidence.Postcondition || !evidence.Absent {
		return refusal("lifecycle.state", "the artifact-server adapter did not prove removal", "")
	}
	if evidence.Unit != "" || evidence.Container != "" || evidence.ContentRoot || len(evidence.Listeners) != 0 {
		return refusal("lifecycle.state", "the artifact-server removal evidence still reports an owned resource", "")
	}
	return nil
}

// ValidatePartial accepts evidence only when it positively proves this
// context's own server is part way realized: the unit, container or content
// root it names is present while the whole is not. Each carries the context in
// its name and is claimed by its host reservation, so their presence is never
// another context's work, and repeating the operation converges them.
func ValidatePartial(data []byte, digest string) error {
	evidence, err := decodeEvidence(data)
	if err != nil {
		return err
	}
	if evidence.Request != digest {
		return refusal("lifecycle.state", "the artifact-server evidence names another request", "")
	}
	if evidence.Postcondition || evidence.Absent {
		return refusal("lifecycle.state", "the artifact-server evidence proves a settled state, not a partial one", "")
	}
	if evidence.Unit == "" && evidence.Container == "" && !evidence.ContentRoot {
		return refusal("lifecycle.state", "the artifact-server evidence reports nothing this context owns", "")
	}
	return nil
}

// ValidateUnremoved accepts observed evidence only when it proves a removal of
// the frozen request took nothing back: this request's presence form reporting
// the unit active, the container of the frozen image and the content root.
// The listeners, their status lines and presented certificates, and the
// postcondition flag are the apply's proof of readiness, not anything a
// removal takes back, so none of them plays any part.
func ValidateUnremoved(data []byte, request Request, digest string) error {
	evidence, err := removalEvidence(data, digest)
	if err != nil {
		return err
	}
	if !unremoved(evidence, request) {
		return refusal("lifecycle.state", "the artifact server no longer holds everything its removal takes back", "")
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
		return refusal("lifecycle.state", "the artifact-server evidence reports nothing its removal takes back", "")
	}
	if unremoved(evidence, request) {
		return refusal("lifecycle.state", "the artifact server still holds everything its removal takes back", "")
	}
	return nil
}

// removalEvidence decodes observed evidence for this request in its presence
// form, the only form that reports what a removal has still to take back.
func removalEvidence(data []byte, digest string) (Evidence, error) {
	evidence, err := decodeEvidence(data)
	if err != nil {
		return Evidence{}, err
	}
	if evidence.Request != digest {
		return Evidence{}, refusal("lifecycle.state", "the artifact-server evidence names another request", "")
	}
	if evidence.Absent {
		return Evidence{}, refusal("lifecycle.state", "the artifact-server evidence is an absence form, which reports nothing left to take back", "")
	}
	return evidence, nil
}

func unremoved(evidence Evidence, request Request) bool {
	return evidence.Unit == "active" && evidence.Container == request.Image && evidence.ContentRoot
}

const maxEvidenceBytes = 64 << 10
