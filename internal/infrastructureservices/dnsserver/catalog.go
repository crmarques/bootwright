package dnsserver

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
)

const Implementation = "dns-server-dnsmasq-v1"

const Kind = string(api.DNSServer)

const defaultImage = "docker.io/4km3/dnsmasq@sha256:52e25fb2601156ab66f6a0872c180b285df7cafaa41267d8d65689f066490641"

// Definition binds the shared managed-service behavior to dnsmasq. The records
// it answers are derived from the retained Machines, and the forwarders are
// the authored ones; with none declared the resolver answers only its own
// records rather than silently reaching an ambient upstream.
func Definition() managedservice.Definition {
	return managedservice.Definition{
		Kind:           api.DNSServer,
		Implementation: Implementation,
		Version:        Implementation,
		Slug:           "dns",
		Variable:       "bootwright_dns_server",
		Purpose:        "resolve names",
		Subject:        "DNS server",
		Image:          defaultImage,
		Extend: func(catalog api.Catalog, spec api.Value, request *managedservice.Request) error {
			request.Records = managedservice.MachineRecords(catalog)
			request.Forwarders = spec.Get("forwarders").Strings()
			request.IngressHosts = spec.Get("additionalIngressHosts").Strings()
			return nil
		},
	}
}
