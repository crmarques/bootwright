package artifactserver

import (
	"time"

	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

// Capability is the managed service lifecycle bound to the artifact server,
// whose request and evidence differ from the network services' only in
// carrying several listeners.
type Capability struct {
	managedservice.Service[Request, Evidence]
}

type Runner = managedservice.Runner

const noun = "artifact-server"

func New(runner Runner, clock lifecycle.Clock) Capability {
	return Capability{managedservice.NewService[Request, Evidence](port(), runner).WithClock(clock)}
}

func (c Capability) WithInstalledHostKeys(hostKeys lifecycle.InstalledHostKey) Capability {
	c.Service = c.Service.WithInstalledHostKeys(hostKeys)
	return c
}

func port() managedservice.KindPort[Request] {
	return managedservice.KindPort[Request]{
		Kind: Kind, Implementation: Implementation, Variable: "bootwright_artifact_server", Version: requestVersion, Noun: noun,
		Purpose: "serve artifacts", Subject: "artifact server", Reservation: serviceSlug, ContentDigest: ContentDigest(),
		Steps: [][2]string{
			{"pull-image", "acquire the pinned server image"},
			{"publish-configuration", "publish the server configuration and unit"},
			{"start-service", "start the managed service"},
			{"verify-readiness", "verify every listener answers"},
		},
		Requests: Requests,
		Bound: func(material map[string]secrets.Material, secret, contextName string) error {
			_, err := BoundServingMaterial(material, secret, contextName)
			return err
		},
		Certificate: ServingFingerprint,
		Materials: func(execution lifecycle.Execution, request Request, operation string) (lifecycle.Execution, []lifecycle.MaterialFile, error) {
			return execution, materials(request, operation), nil
		},
	}
}

// ServingFingerprint proves the bound serving material of Secret secret and
// names the fingerprint of the certificate it serves.
func ServingFingerprint(material map[string]secrets.Material, secret, contextName string, addresses []string, now time.Time, registered bool) (string, error) {
	bound, err := BoundServingMaterial(material, secret, contextName)
	if err != nil {
		return "", err
	}
	if !registered {
		return "", ProveServingCertificate(bound, secret, contextName, addresses, now)
	}
	served, err := ValidateServingCertificate(bound, secret, contextName, addresses, now)
	return served.Fingerprint, err
}

func (r Request) Frame() managedservice.Frame {
	targets := r.ProbeTargets()
	sockets := make([]string, 0, len(targets))
	for _, target := range targets {
		sockets = append(sockets, infrastructureservices.HostPort(target.Address, target.Port))
	}
	frame := managedservice.Frame{
		Identity: r.Identity, Placement: r.Placement, Image: r.Image, ImageSource: r.ImageSource,
		Unit: r.Unit, ContentRoot: r.ContentRoot, BindAddress: r.BindAddress,
		Served: r.servedAddresses("https"), Probed: probedAddresses(r), Sockets: sockets, Ports: listenerPorts(r),
		Secrets: r.secretReferences(), Reservations: r.reservationKeys(),
	}
	if r.TLS != nil {
		frame.TLSSecret = r.TLS.Secret
	}
	return frame
}

// materials lists this capability's own bound parts. The placement's identity
// and host key are added by the attempt, so they are not repeated here.
// Anything not listed never leaves bounded memory.
func materials(request Request, operation string) []lifecycle.MaterialFile {
	var files []lifecycle.MaterialFile
	if request.TLS != nil {
		files = append(files,
			lifecycle.MaterialFile{Name: "tls.crt", Part: secrets.CertificatePart, Secret: request.TLS.Secret, Variable: "certificate"},
			lifecycle.MaterialFile{Name: "tls.key", Part: secrets.PrivateKeyPart, Secret: request.TLS.Secret, Variable: "privateKey"},
		)
	}
	if operation != "apply" {
		return files
	}
	return append(files, request.ImageSource.Materials()...)
}

// Unsupported refuses every selected object this capability cannot realize,
// with its reason and remedy, so the operation refuses before registration
// instead of part way through.
func (Capability) Unsupported(state *compilation.State) []lifecycle.Refusal {
	if state == nil {
		return nil
	}
	return Refusals(state.Effective())
}

func (Capability) PrecedesController(definition reconciliation.BlockDefinition) bool {
	request, err := DecodeRequest(definition.Request)
	return err == nil && (request.Placement.Connection == connectionSSH || request.Placement.Connection == connectionLocal || request.Placement.Installed())
}
