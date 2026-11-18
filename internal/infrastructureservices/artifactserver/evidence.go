package artifactserver

import (
	"errors"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
)

// Evidence is the only result shape the adapter may return. Go validates it
// strictly: an adapter cannot widen a postcondition, invent a listener or
// report a certificate the operation did not bind.
type Evidence struct {
	StartedAfterFiles bool               `json:"startedAfterFiles"`
	Absent            bool               `json:"absent"`
	Container         string             `json:"container"`
	ContentRoot       bool               `json:"contentRoot"`
	Listeners         []ListenerEvidence `json:"listeners"`
	Postcondition     bool               `json:"postcondition"`
	Request           string             `json:"request"`
	Unit              string             `json:"unit"`
}

type ListenerEvidence struct {
	Address     string `json:"address"`
	Fingerprint string `json:"fingerprint"`
	Name        string `json:"name"`
	Port        int    `json:"port"`
	Protocol    string `json:"protocol"`
	Status      string `json:"status"`
}

// listenerPorts are the ports the server binds, each once and in order, which
// the adapter names a foreign listener by.
func listenerPorts(request Request) []int {
	ports := []int{}
	for _, listener := range request.Listeners {
		ports = append(ports, listener.Port)
	}
	slices.Sort(ports)
	return slices.Compact(ports)
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

// probedAddresses is every address readiness probes, whatever the listener.
func probedAddresses(r Request) []string {
	var addresses []string
	for _, target := range r.ProbeTargets() {
		addresses = append(addresses, target.Address)
	}
	return addresses
}

func (e Evidence) Observed() managedservice.Observed {
	return managedservice.Observed{
		Absent: e.Absent, Answered: len(e.Listeners) != 0, Container: e.Container, ContentRoot: e.ContentRoot,
		Postcondition: e.Postcondition, Request: e.Request, StartedAfterFiles: e.StartedAfterFiles, Unit: e.Unit,
	}
}

// Ready proves every frozen listener answered with an HTTP status line, and an
// HTTPS one presenting the bound certificate.
func (e Evidence) Ready(request Request, fingerprint string) error {
	targets := request.ProbeTargets()
	if len(e.Listeners) != len(targets) {
		return artifactListenersMismatch.Fail("the artifact-server evidence does not cover every listener", "")
	}
	observed := slices.Clone(e.Listeners)
	slices.SortFunc(observed, func(x, y ListenerEvidence) int {
		if order := strings.Compare(x.Name, y.Name); order != 0 {
			return order
		}
		return strings.Compare(x.Address, y.Address)
	})
	for index, target := range targets {
		listener := observed[index]
		if listener.Name != target.Name || listener.Address != target.Address || listener.Port != target.Port || listener.Protocol != target.Protocol {
			return artifactListenersMismatch.Fail("the artifact-server evidence does not match its frozen listeners", "")
		}
		if !strings.HasPrefix(listener.Status, "HTTP/1.") {
			return artifactListenerUnanswered.Fail("an artifact-server listener did not answer with an HTTP status line", "")
		}
		if target.Protocol == "https" && (len(listener.Fingerprint) != 64 || strings.Trim(listener.Fingerprint, "0123456789abcdef") != "") {
			return artifactCertificateEvidenceInvalid.Fail("an artifact-server listener supplied no usable certificate fingerprint", "")
		}
		if target.Protocol == "https" && listener.Fingerprint != fingerprint {
			return errors.Join(managedservice.ErrCertificateMismatch, artifactCertificateMismatch.Fail("an artifact-server listener presented a different certificate than the operation bound", ""))
		}
		if target.Protocol == "http" && listener.Fingerprint != "" {
			return artifactCertificateEvidenceInvalid.Fail("an HTTP artifact-server listener reported a certificate", "")
		}
	}
	return nil
}

func ValidatePresence(data []byte, request Request, digest, fingerprint string) error {
	return managedservice.Presence[Request, Evidence](noun, data, request, digest, fingerprint)
}

func ValidateAbsence(data []byte, digest string) error {
	return managedservice.Absence[Request, Evidence](noun, data, digest)
}

func ValidatePartial(data []byte, request Request, digest, fingerprint string) error {
	return managedservice.Partial[Request, Evidence](noun, data, request, digest, fingerprint)
}

func ValidateUnremoved(data []byte, request Request, digest string) error {
	return managedservice.Unremoved[Request, Evidence](noun, data, request, digest)
}

func ValidateRemovalUnfinished(data []byte, request Request, digest string) error {
	return managedservice.RemovalUnfinished[Request, Evidence](noun, data, request, digest)
}
