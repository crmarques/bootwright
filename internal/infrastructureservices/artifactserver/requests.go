package artifactserver

import (
	machineref "github.com/crmarques/bootwright/internal/machine"
	"slices"

	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/reconciliation"
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
	return reconciliation.Freeze(r, "artifact-server")
}

func DecodeRequest(data []byte) (Request, error) {
	return reconciliation.ThawVersion[Request](data, "artifact-server", requestVersion)
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
// wildcard bind claims every endpoint address at a listener's port as well,
// because the socket it opens conflicts with each of them.
func (r Request) reservationKeys() []string {
	var sockets []string
	for _, listener := range r.Listeners {
		var endpoints []managedservice.Endpoint
		for _, endpoint := range r.Endpoints {
			if endpoint.Listener == listener.Name {
				endpoints = append(endpoints, managedservice.Endpoint{Address: endpoint.Address, Name: endpoint.Name})
			}
		}
		sockets = append(sockets, managedservice.SocketKeys(r.BindAddress, listener.Port, endpoints)...)
	}
	return managedservice.ReservationKeys(r.Unit, r.ContentRoot, sockets)
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
