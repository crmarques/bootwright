package managedservice

import (
	"net/netip"
	"slices"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/substrate"
)

// Definition is everything that distinguishes one managed network service from
// another. The behavior around it — selection, freezing, evidence and the
// inverse — is identical, so it is implemented once.
type Definition struct {
	Kind           api.Kind
	Implementation string
	Selects        string
	Version        string
	Slug           string
	Variable       string
	// Purpose is what an apply makes this service do; Subject is what a
	// removal takes away. A plan needs both, because a destroy that borrows
	// the apply's wording reads as though it were installing.
	Purpose string
	Subject string
	Image   string
	// Extend adds the kind's own frozen intent to a request whose shared
	// fields are already derived.
	Extend       func(catalog api.Catalog, spec api.Value, request *Request) error
	Certificate  Certificate
	PasswordFile func(username, password []byte) ([]byte, error)
}

type Certificate func(material map[string]secrets.Material, secret, contextName string, addresses []string, now time.Time, registered bool) (string, error)

func (d Definition) BlockID(service string) string {
	return reconciliation.BlockIdentity("", d.Slug+"-"+service)
}

// ContentDigest binds the plan to the exact behavior this build implements, so
// changing the request shape, the compiled image or the host layout
// invalidates a frozen plan.
func (d Definition) ContentDigest() string {
	return reconciliation.ContentDigestOf(
		"bootwright.infrastructureservices.managed-service-v1",
		d.Implementation, d.Version, d.Image, HostedRoot, ServicesArea, UnitPrefix,
	)
}

func (d Definition) port() KindPort[Request] {
	return KindPort[Request]{
		Kind: string(d.Kind), Implementation: d.Implementation, Variable: d.Variable, Version: d.Version, Noun: noun,
		Purpose: d.Purpose, Subject: d.Subject, Reservation: d.Slug, ContentDigest: d.ContentDigest(),
		Requests: d.requests, Certificate: d.Certificate, Materials: d.materials,
	}
}

func (d Definition) materials(execution lifecycle.Execution, request Request, operation string) (lifecycle.Execution, []lifecycle.MaterialFile, error) {
	materials := request.Materials(operation)
	if operation != "apply" || request.CredentialsRef == "" {
		return execution, materials, nil
	}
	execution, err := d.passwordFile(execution, request)
	if err != nil {
		return execution, nil, err
	}
	return execution, append(materials, lifecycle.MaterialFile{Name: "htpasswd", Part: secrets.ValuePart, Secret: passwordFileMaterial, Variable: "passwordFile"}), nil
}

// Capability is the shared managed service lifecycle bound to one
// definition's kind.
type Capability struct {
	Service[Request, Evidence]
	definition Definition
}

func NewCapability(definition Definition, runner Runner) Capability {
	return Capability{Service: NewService[Request, Evidence](definition.port(), runner), definition: definition}
}

func (c Capability) WithClock(clock lifecycle.Clock) Capability {
	c.Service = c.Service.WithClock(clock)
	return c
}

func (c Capability) WithInstalledHostKeys(hostKeys lifecycle.InstalledHostKey) Capability {
	c.Service = c.Service.WithInstalledHostKeys(hostKeys)
	return c
}

func (c Capability) Kind() string { return string(c.definition.Kind) }

func (Capability) PrecedesController(reconciliation.BlockDefinition) bool { return true }

// Requests derives one frozen request per managed service of this kind, in
// canonical object order. It reads no host, endpoint or Secret material.
func (c Capability) Requests(catalog api.Catalog, controllerMachine, contextName string) ([]Request, error) {
	return c.definition.requests(catalog, controllerMachine, contextName)
}

func (d Definition) requests(catalog api.Catalog, controllerMachine, contextName string) ([]Request, error) {
	if !ValidContextName(contextName) {
		return nil, serviceRequestInvalid.Fail("the lifecycle context identity is invalid", "")
	}
	var requests []Request
	for _, object := range ManagedObjects(catalog, d) {
		request, err := d.requestFor(catalog, object, controllerMachine, contextName)
		if err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	return requests, nil
}

func (d Definition) requestFor(catalog api.Catalog, object api.Object, controllerMachine, contextName string) (Request, error) {
	name := object.Name()
	if !SafeSegment(name) {
		return Request{}, serviceNameUnsafe.Fail("the managed service name is not a safe host identifier", "rename "+object.Identity())
	}
	spec := object.Spec()
	machine, found := catalog.Find(api.Machine, spec.Get("machineRef").Text())
	if !found {
		return Request{}, Refusal("api.reference", "the managed service's placement Machine is not in the selected graph", "declare "+spec.Get("machineRef").Text()+" or change the reference")
	}
	placement, err := machineref.ServicePlacementFor(machine, controllerMachine)
	if err != nil {
		return Request{}, err
	}
	image, err := ImageFor(spec, object.Identity(), d.Image)
	if err != nil {
		return Request{}, err
	}
	var source *ImageSource
	if !spec.Get("image", "local").Present() {
		image, source = MirroredImage(catalog, image, ServiceConsumer(object))
	}
	egress, err := EgressFor(catalog, machine)
	if err != nil {
		return Request{}, err
	}
	endpoints, err := EndpointsFor(spec, machine)
	if err != nil {
		return Request{}, err
	}
	bind, err := BindAddress(spec, object.Identity())
	if err != nil {
		return Request{}, err
	}
	port, err := Port(spec, object.Identity())
	if err != nil {
		return Request{}, err
	}
	request := Request{
		BindAddress:    bind,
		ContentRoot:    ContentRoot(contextName, d.Slug, name),
		Egress:         egress,
		Endpoints:      endpoints,
		Identity:       Identity{Block: d.BlockID(name), Context: contextName, Service: name},
		Image:          image,
		ImageSource:    source,
		Implementation: d.Implementation,
		Kind:           string(d.Kind),
		Placement:      placement,
		Port:           port,
		Unit:           UnitName(contextName, d.Slug, name),
		Version:        d.Version,
	}
	if d.Extend != nil {
		if err := d.Extend(catalog, spec, &request); err != nil {
			return Request{}, err
		}
	}
	return request, nil
}

func (r Request) Frame() Frame {
	targets := r.ProbeTargets()
	sockets := make([]string, 0, len(targets))
	for _, address := range targets {
		sockets = append(sockets, infrastructureservices.HostPort(address, r.Port))
	}
	frame := Frame{
		Identity: r.Identity, Placement: r.Placement, Image: r.Image, ImageSource: r.ImageSource,
		Unit: r.Unit, ContentRoot: r.ContentRoot, BindAddress: r.BindAddress, CredentialsRef: r.CredentialsRef,
		Served: r.ServedAddresses(), Probed: targets, Sockets: sockets, Ports: []int{r.Port},
		Secrets: r.SecretReferences(), Reservations: r.ReservationKeys(),
	}
	if r.TLS != nil {
		frame.TLSSecret = r.TLS.Secret
	}
	return frame
}

// PlacementRequirements names, in name order, each provider the service's block
// waits for: one whose managed bridge carries a probed address on the placement
// Machine, whose socket or answer cannot exist before then, and, on a Machine
// reached over SSH, one hosted there, whose block installs the container runtime.
func PlacementRequirements(catalog api.Catalog, placement machineref.Placement, addresses []string) []reconciliation.ObjectRef {
	machine := placement.Machine
	var providers []string
	for _, address := range addresses {
		providers = append(providers, substrate.BridgeProviders(catalog, machine, address)...)
	}
	if !placement.Local() {
		providers = append(providers, substrate.ProvidersHostedOn(catalog, machine)...)
	}
	slices.Sort(providers)
	var references []reconciliation.ObjectRef
	for _, provider := range slices.Compact(providers) {
		references = append(references, reconciliation.ObjectRef{Kind: string(api.InfraProvider), Object: provider})
	}
	if machineref.ManagesFirewall(catalog, machine) && slices.ContainsFunc(addresses, func(address string) bool {
		parsed, err := netip.ParseAddr(address)
		return err != nil || !parsed.IsLoopback()
	}) {
		references = append(references, reconciliation.ObjectRef{Kind: string(api.Machine), Object: machine, Family: machineref.FirewallFamily})
	}
	if placement.Installed() {
		references = append(references, reconciliation.ObjectRef{Kind: string(api.Machine), Object: machine, Family: machineref.InstallFamily})
	}
	if machineref.ProvesHardware(catalog, placement) {
		references = append(references, reconciliation.ObjectRef{Kind: string(api.Machine), Object: machine, Family: machineref.HardwareFamily})
	}
	return references
}
