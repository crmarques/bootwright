package artifactserver

import (
	"bytes"
	"encoding/json"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
)

const requestVersion = "artifact-server-nginx-v2"

// Request is the complete frozen intent for one managed artifact server. Its
// fields are declared in the canonical key order the plan digest requires, and
// it carries no secret value: TLS names a declaration and its non-secret
// fingerprint, never material or a material digest.
type Request struct {
	BindAddress string     `json:"bindAddress"`
	ContentRoot string     `json:"contentRoot"`
	Egress      Egress     `json:"egress"`
	Endpoints   []Endpoint `json:"endpoints"`
	Identity    Identity   `json:"identity"`
	Image       string     `json:"image"`
	Listeners   []Listener `json:"listeners"`
	Placement   Placement  `json:"placement"`
	TLS         *TLS       `json:"tls,omitempty"`
	Unit        string     `json:"unit"`
	Version     string     `json:"version"`
}

// Identity and Egress are the shared managed-service values and Placement is
// the engine's own, so a frozen artifact-server request keeps the exact shape
// it always had while the derivation is implemented once.
type (
	Identity  = managedservice.Identity
	Placement = machineref.Placement
	Egress    = managedservice.Egress
)

type Listener struct {
	Name     string `json:"name"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
}

type Endpoint struct {
	Address  string `json:"address"`
	Listener string `json:"listener"`
	Name     string `json:"name"`
}

type TLS struct {
	Fingerprint string `json:"fingerprint"`
	MinVersion  string `json:"minVersion"`
	Secret      string `json:"secret"`
}

const (
	connectionLocal = machineref.ConnectionLocal
	connectionSSH   = machineref.ConnectionSSH
)

// Canonical encodes the request exactly as the plan digest and the adapter
// both consume it. It refuses anything a reader could interpret differently.
func (r Request) Canonical() ([]byte, error) {
	data, err := json.Marshal(r)
	if err != nil {
		return nil, failure("lifecycle.state", "the artifact-server request cannot be encoded", "")
	}
	var probe map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&probe); err != nil {
		return nil, failure("lifecycle.state", "the artifact-server request cannot be decoded", "")
	}
	canonical, err := json.Marshal(probe)
	if err != nil || !bytes.Equal(data, canonical) {
		return nil, failure("lifecycle.state", "the artifact-server request is not canonically ordered", "")
	}
	return data, nil
}

func DecodeRequest(data []byte) (Request, error) {
	var request Request
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return Request{}, failure("lifecycle.state", "the frozen artifact-server request is malformed", "")
	}
	if len(bytes.Trim(data[decoder.InputOffset():], " \t\r\n")) != 0 {
		return Request{}, failure("lifecycle.state", "the frozen artifact-server request contains trailing data", "")
	}
	if request.Version != requestVersion {
		return Request{}, failure("lifecycle.state", "the frozen artifact-server request has an unsupported version", "")
	}
	canonical, err := request.Canonical()
	if err != nil {
		return Request{}, err
	}
	if !bytes.Equal(canonical, data) {
		return Request{}, failure("lifecycle.state", "the frozen artifact-server request is not canonical", "")
	}
	return request, nil
}

func (r Request) listener(name string) (Listener, bool) {
	for _, listener := range r.Listeners {
		if listener.Name == name {
			return listener, true
		}
	}
	return Listener{}, false
}

// servedAddresses lists every address an HTTPS endpoint answers on, which is
// exactly what the serving certificate must cover.
func (r Request) servedAddresses(protocol string) []string {
	var addresses []string
	for _, endpoint := range r.Endpoints {
		listener, ok := r.listener(endpoint.Listener)
		if !ok || listener.Protocol != protocol {
			continue
		}
		if !slices.Contains(addresses, endpoint.Address) {
			addresses = append(addresses, endpoint.Address)
		}
	}
	slices.Sort(addresses)
	return addresses
}

func (r Request) usesTLS() bool {
	return slices.ContainsFunc(r.Listeners, func(listener Listener) bool { return listener.Protocol == "https" })
}

// reservationKeys are the exclusive host resources this request claims. A
// wildcard bind claims every endpoint address at that port as well, because
// the socket it opens conflicts with each of them.
func (r Request) reservationKeys() []string {
	keys := []string{"unit:" + r.Unit, "path:" + r.ContentRoot}
	for _, listener := range r.Listeners {
		port := formatPort(listener.Port)
		keys = append(keys, "socket:"+r.BindAddress+":"+port)
		if r.BindAddress != "0.0.0.0" && r.BindAddress != "::" {
			continue
		}
		for _, endpoint := range r.Endpoints {
			if endpoint.Listener == listener.Name {
				keys = append(keys, "socket:"+endpoint.Address+":"+port)
			}
		}
	}
	slices.Sort(keys)
	return slices.Compact(keys)
}

func formatPort(value int) string {
	if value <= 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

func safeSegment(value string) bool {
	if value == "" || len(value) > 63 || strings.ContainsAny(value, "/\x00 ") {
		return false
	}
	for index, c := range value {
		alphanumeric := c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
		if !alphanumeric && !(c == '-' && index != 0 && index != len(value)-1) {
			return false
		}
	}
	return true
}

// secretReferences names every declaration this request's execution needs
// bound, so the operation freezes them before it registers.
func (r Request) secretReferences() []string {
	var references []string
	if r.TLS != nil {
		references = append(references, r.TLS.Secret)
	}
	references = append(references, r.Placement.SecretReferences()...)
	slices.Sort(references)
	return slices.Compact(references)
}
