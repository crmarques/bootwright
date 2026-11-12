package installation

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const (
	InstallFamily           = machineref.InstallFamily
	EraseFamily             = "physical-erase"
	FirewallFamily          = machineref.FirewallFamily
	FirewallImplementation  = "os-host-firewall-v2"
	FirewallEvidenceVersion = "os-host-firewall-evidence-v2"
	FirewallServiceRoot     = "/etc/firewalld/services"
	firewallVariable        = "bootwright_os_firewall"
	firewallServicePrefix   = "bootwright-"
	firewallMarkPrefix      = "bootwright-host-firewall"
	maxFirewallServices     = 64
	maxFirewallNetworks     = 16
	maxFirewallAddresses    = 64
	maxFirewallPorts        = 64
	maxFirewallProtocols    = 16
	maxFirewallName         = 63
	firewallNameHashDigits  = 16
)

var (
	firewallProtocol  = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	firewallPublisher = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,62}$`)
	firewallName      = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
)

type FirewallIdentity struct {
	Block   string `json:"block"`
	Context string `json:"context"`
	Object  string `json:"object"`
}

type FirewallPort struct {
	Number    int    `json:"number"`
	Transport string `json:"transport"`
}

type FirewallService struct {
	Addresses []string       `json:"addresses,omitempty"`
	Name      string         `json:"name"`
	Networks  []string       `json:"networks"`
	Ports     []FirewallPort `json:"ports"`
	Protocols []string       `json:"protocols"`
	Publisher string         `json:"publisher"`
}

type FirewallRequest struct {
	Address     string               `json:"address"`
	Direct      bool                 `json:"direct"`
	FleetKeyRef string               `json:"fleetKeyRef"`
	Identity    FirewallIdentity     `json:"identity"`
	Placement   machineref.Placement `json:"placement"`
	Preparation bool                 `json:"preparation"`
	Services    []FirewallService    `json:"services"`
	User        string               `json:"user"`
	Version     string               `json:"version"`
}

func (r FirewallRequest) Canonical() ([]byte, error) {
	return reconciliation.Freeze(r, "host firewall")
}

func (r FirewallRequest) SecretReferences() []string {
	return lifecycle.SecretNames(append(r.Placement.SecretReferences(), r.FleetKeyRef)...)
}

func DecodeFirewallRequest(data []byte) (FirewallRequest, error) {
	request, err := reconciliation.ThawVersion[FirewallRequest](data, "host firewall", FirewallImplementation)
	if err != nil {
		return FirewallRequest{}, err
	}
	if err := validFirewallRequest(request); err != nil {
		return FirewallRequest{}, err
	}
	return request, nil
}

func FirewallContentDigest() string {
	mark := FirewallMark("context", FirewallBlockID("machine"), "operation")
	name := FirewallServiceName("context", "publisher")
	sample := sha256.Sum256([]byte(FirewallServiceContent(FirewallService{
		Name: name, Networks: []string{"192.0.2.10/32", "192.0.2.0/24"}, Ports: []FirewallPort{{Number: 443, Transport: "tcp"}, {Number: 53, Transport: "udp"}},
		Protocols: []string{"vrrp"}, Publisher: "publisher",
	}, mark)))
	digest := sha256.Sum256([]byte(strings.Join([]string{"bootwright.managedos.host-firewall-v1", FirewallImplementation, FirewallEvidenceVersion, FirewallServiceRoot,
		mark, name, hex.EncodeToString(sample[:])}, "\x00")))
	return hex.EncodeToString(digest[:])
}

func FirewallBlockID(machine string) string {
	return reconciliation.BlockIdentity("os-firewall-", machine)
}

func FirewallPreparationBlockID(machine string) string {
	return reconciliation.BlockIdentity("os-firewall-prepare-", machine)
}

func firewallRequestBlockID(request FirewallRequest) string {
	if request.Preparation {
		return FirewallPreparationBlockID(request.Identity.Object)
	}
	return FirewallBlockID(request.Identity.Object)
}

func FirewallServiceName(contextName, publisher string) string {
	raw := firewallServicePrefix + contextName + "-" + publisher
	if firewallName.MatchString(raw) {
		return reconciliation.BlockIdentity("bootwright-", contextName+"-"+publisher)
	}
	safe := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, raw)
	sum := sha256.Sum256([]byte(raw))
	head := strings.Trim(safe[:min(len(safe), maxFirewallName-firewallNameHashDigits-1)], "-")
	return head + "-" + hex.EncodeToString(sum[:])[:firewallNameHashDigits]
}

func FirewallMark(contextName, block, applyOperation string) string {
	return firewallMarkPrefix + " context=" + contextName + " block=" + block + " apply=" + applyOperation
}

func FirewallServiceContent(service FirewallService, mark string) string {
	var content strings.Builder
	content.WriteString("<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<service>\n")
	content.WriteString("  <short>" + service.Name + "</short>\n")
	content.WriteString("  <description>" + mark + "</description>\n")
	for _, port := range service.Ports {
		content.WriteString("  <port protocol=\"" + port.Transport + "\" port=\"" + strconv.Itoa(port.Number) + "\"/>\n")
	}
	for _, protocol := range service.Protocols {
		content.WriteString("  <protocol value=\"" + protocol + "\"/>\n")
	}
	content.WriteString("</service>\n")
	return content.String()
}

func FirewallRequests(catalog api.Catalog, controllerMachine, contextName string, requirements []HostPortRequirement) ([]FirewallRequest, error) {
	byMachine := map[string][]HostPortRequirement{}
	for _, requirement := range requirements {
		if err := validRequirement(requirement); err != nil {
			return nil, err
		}
		if slices.ContainsFunc(byMachine[requirement.Machine], func(other HostPortRequirement) bool { return other.Publisher == requirement.Publisher }) {
			return nil, refusal("lifecycle.state", "two host port requirements of "+requirement.Publisher+" name Machine/"+requirement.Machine, "")
		}
		byMachine[requirement.Machine] = append(byMachine[requirement.Machine], requirement)
	}
	for _, object := range catalog.OfKind(api.Machine) {
		if object.Spec().Get("firewall", "management").Text() == "managed" && object.Spec().Get("os", "provided").Bool() {
			if _, found := byMachine[object.Name()]; !found {
				byMachine[object.Name()] = nil
			}
		}
	}
	var machines []string
	for machine := range byMachine {
		if RealizesHostPorts(catalog, machine) {
			machines = append(machines, machine)
		}
	}
	if len(machines) == 0 {
		return nil, nil
	}
	slices.Sort(machines)
	installs, _, err := intentRequests(catalog, controllerMachine, contextName)
	if err != nil {
		return nil, err
	}
	var requests []FirewallRequest
	for _, machine := range machines {
		at := slices.IndexFunc(installs, func(install Request) bool { return install.Identity.Object == machine })
		install := Request{Identity: Identity{Object: machine}}
		if at >= 0 {
			install = installs[at]
		}
		request, err := firewallRequestFor(install, contextName, byMachine[machine])
		if at < 0 {
			object, _ := catalog.Find(api.Machine, machine)
			placement, placementErr := machineref.PlacementFor(object, controllerMachine)
			if placementErr != nil {
				return nil, placementErr
			}
			request.Direct, request.Placement = true, placement
			err = validFirewallRequest(request)
		}
		if err != nil {
			return nil, err
		}
		if request.Direct {
			preparation := request
			preparation.Preparation, preparation.Services = true, []FirewallService{}
			preparation.Identity.Block = FirewallPreparationBlockID(machine)
			requests = append(requests, preparation)
			if len(request.Services) == 0 {
				continue
			}
		}
		requests = append(requests, request)
	}
	return requests, nil
}

func firewallRequestFor(install Request, contextName string, requirements []HostPortRequirement) (FirewallRequest, error) {
	machine := install.Identity.Object
	if len(requirements) > maxFirewallServices {
		return FirewallRequest{}, refusal("lifecycle.state", "Machine/"+machine+" holds more than "+strconv.Itoa(maxFirewallServices)+" host port requirements", "")
	}
	slices.SortFunc(requirements, func(a, b HostPortRequirement) int { return strings.Compare(a.Publisher, b.Publisher) })
	request := FirewallRequest{
		Address: install.Address, FleetKeyRef: install.FleetKeyRef, Placement: install.Placement, User: install.User, Version: FirewallImplementation,
		Identity: FirewallIdentity{Block: FirewallBlockID(machine), Context: contextName, Object: machine},
		Services: []FirewallService{},
	}
	for _, requirement := range requirements {
		ports := make([]FirewallPort, 0, len(requirement.Ports))
		for _, port := range requirement.Ports {
			ports = append(ports, FirewallPort{Number: port.Number, Transport: port.Transport})
		}
		slices.SortFunc(ports, func(a, b FirewallPort) int {
			return cmp.Or(strings.Compare(a.Transport, b.Transport), cmp.Compare(a.Number, b.Number))
		})
		protocols := slices.Clone(requirement.Protocols)
		slices.Sort(protocols)
		request.Services = append(request.Services, FirewallService{
			Addresses: slices.Clone(requirement.Addresses), Name: FirewallServiceName(contextName, requirement.Publisher), Networks: slices.Clone(requirement.Networks),
			Ports: slices.Compact(ports), Protocols: slices.Compact(protocols), Publisher: requirement.Publisher,
		})
	}
	return request, validFirewallRequest(request)
}

func validRequirement(requirement HostPortRequirement) error {
	subject := "the host port requirement of " + requirement.Publisher + " on Machine/" + requirement.Machine
	switch {
	case requirement.Machine == "" || !firewallPublisher.MatchString(requirement.Publisher):
		return refusal("lifecycle.state", "a host port requirement names no Machine or publishing block", "")
	case len(requirement.Addresses) == 0 && len(requirement.Networks) == 0 || len(requirement.Networks) > maxFirewallNetworks || len(requirement.Addresses) > maxFirewallAddresses:
		return refusal("lifecycle.state", subject+" names no network or more than "+strconv.Itoa(maxFirewallNetworks), "")
	case len(requirement.Networks) != 0 && !singleHost(requirement.Networks[0]):
		return refusal("lifecycle.state", subject+" does not name its virtual address as a single-host prefix first", "")
	case len(requirement.Ports)+len(requirement.Protocols) == 0 || len(requirement.Ports) > maxFirewallPorts || len(requirement.Protocols) > maxFirewallProtocols:
		return refusal("lifecycle.state", subject+" admits nothing or more than its bound", "")
	}
	return nil
}

func validFirewallRequest(request FirewallRequest) error {
	invalid := func(message string) error {
		return refusal("lifecycle.state", "the host firewall request of Machine/"+request.Identity.Object+" "+message, "")
	}
	if request.Version != FirewallImplementation || request.Identity.Block != firewallRequestBlockID(request) || !api.ValidLexical("name", request.Identity.Context) {
		return invalid("names another block or version")
	}
	if request.Preparation && (!request.Direct || len(request.Services) != 0) {
		return invalid("has an invalid daemon preparation")
	}
	if request.Direct {
		placement := request.Placement
		if placement.Machine != request.Identity.Object || request.Address != "" || request.User != "" || request.FleetKeyRef != "" {
			return invalid("mixes direct placement with an installation session")
		}
		switch placement.Connection {
		case machineref.ConnectionLocal:
			if placement != (machineref.Placement{Connection: machineref.ConnectionLocal, Machine: request.Identity.Object}) {
				return invalid("has unexpected local access fields")
			}
		case machineref.ConnectionSSH:
			if placement.User != "root" || !api.ValidLexical("host", placement.Address) || placement.Port < 1 || placement.Port > 65535 || !api.ValidLexical("name", placement.PrivateKeyRef) || !api.ValidLexical("name", placement.KnownHostsRef) {
				return invalid("has no pinned root SSH placement")
			}
		default:
			return invalid("has an unsupported direct placement")
		}
	}
	if _, err := netip.ParseAddr(request.Address); !request.Direct && (err != nil || request.User == "" || request.FleetKeyRef == "") {
		return invalid("names no guest address, account or fleet key")
	}
	if !request.Direct && len(request.Services) == 0 || len(request.Services) > maxFirewallServices {
		return invalid("holds no service or more than its bound")
	}
	names := map[string]bool{}
	for _, service := range request.Services {
		if !firewallName.MatchString(service.Name) || names[service.Name] || service.Name != FirewallServiceName(request.Identity.Context, service.Publisher) {
			return invalid("holds a service whose name is not derived from its context and publisher, or a repeated one")
		}
		names[service.Name] = true
		if err := validService(service); err != nil {
			return invalid(err.Error())
		}
	}
	return nil
}

func validService(service FirewallService) error {
	if len(service.Networks) == 0 && len(service.Addresses) == 0 || len(service.Networks) > maxFirewallNetworks || len(service.Addresses) > maxFirewallAddresses || len(service.Ports)+len(service.Protocols) == 0 ||
		len(service.Ports) > maxFirewallPorts || len(service.Protocols) > maxFirewallProtocols {
		return refusal("lifecycle.state", "holds a service outside its bounds", "")
	}
	if len(service.Networks) > 0 && !singleHost(service.Networks[0]) {
		return refusal("lifecycle.state", "does not name its virtual address as a single-host prefix first", "")
	}
	if len(service.Addresses) != 0 && len(service.Networks) != 0 {
		return refusal("lifecycle.state", "a firewall service mixes exact addresses and interface networks", "")
	}
	for _, address := range service.Addresses {
		if !api.ValidLexical("host", address) {
			return refusal("lifecycle.state", "a firewall service names an invalid listener address", "")
		}
	}
	seen := map[string]bool{}
	for _, network := range service.Networks {
		prefix, err := netip.ParsePrefix(network)
		if err != nil || prefix.Masked() != prefix || prefix.String() != network || seen[network] {
			return refusal("lifecycle.state", "names a network that is not one canonical CIDR", "")
		}
		seen[network] = true
	}
	for index, port := range service.Ports {
		if port.Number < 1 || port.Number > 65535 || port.Transport != "tcp" && port.Transport != "udp" ||
			index > 0 && cmp.Or(strings.Compare(service.Ports[index-1].Transport, port.Transport), cmp.Compare(service.Ports[index-1].Number, port.Number)) >= 0 {
			return refusal("lifecycle.state", "names a port that is not a sorted TCP or UDP port", "")
		}
	}
	for index, protocol := range service.Protocols {
		if !firewallProtocol.MatchString(protocol) || index > 0 && service.Protocols[index-1] >= protocol {
			return refusal("lifecycle.state", "names a protocol that is not a sorted IP protocol name", "")
		}
	}
	return nil
}

func singleHost(network string) bool {
	prefix, err := netip.ParsePrefix(network)
	return err == nil && prefix.IsSingleIP() && prefix.String() == network
}
