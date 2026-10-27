package dnsserver

import (
	"fmt"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
)

const Implementation = "dns-server-dnsmasq-v1"

const Kind = string(api.DNSServer)

const defaultImage = "docker.io/4km3/dnsmasq@sha256:52e25fb2601156ab66f6a0872c180b285df7cafaa41267d8d65689f066490641"

type GatewayRecords func(api.Catalog) ([]managedservice.Record, error)

// Definition binds the shared managed-service behavior to dnsmasq. It answers
// for retained Machines, their container clusters and the storage-derived
// gateway records. Forwarders are authored; with none declared the resolver
// answers only its own records rather than silently reaching an upstream.
func Definition(gateways GatewayRecords) managedservice.Definition {
	return managedservice.Definition{
		Kind:           api.DNSServer,
		Implementation: Implementation,
		Selects:        "dnsmasq",
		Version:        managedservice.RequestVersion,
		Slug:           "dns",
		Variable:       managedservice.Variable,
		Purpose:        "resolve names",
		Subject:        "DNS server",
		Image:          defaultImage,
		Extend: func(catalog api.Catalog, spec api.Value, request *managedservice.Request) error {
			request.Records = append(managedservice.MachineRecords(catalog), managedservice.ClusterRecords(catalog)...)
			records, err := exportRecords(catalog, request.Records, gateways)
			if err != nil {
				return err
			}
			request.Records = records
			request.Forwarders = spec.Get("forwarders").Strings()
			request.IngressHosts = spec.Get("additionalIngressHosts").Strings()
			return nil
		},
	}
}

func exportRecords(catalog api.Catalog, records []managedservice.Record, gateways GatewayRecords) ([]managedservice.Record, error) {
	if gateways == nil {
		return records, nil
	}
	contributed, err := gateways(catalog)
	if err != nil {
		return nil, err
	}
	for _, gateway := range contributed {
		addresses := slices.Clone(gateway.Addresses)
		slices.Sort(addresses)
		duplicate := false
		for _, record := range records {
			if record.Name != gateway.Name {
				continue
			}
			existing := slices.Clone(record.Addresses)
			slices.Sort(existing)
			if record.Subtree || !slices.Equal(existing, addresses) {
				return nil, fmt.Errorf("gateway DNS record %s conflicts with another retained record", gateway.Name)
			}
			duplicate = true
		}
		if !duplicate {
			records = append(records, managedservice.Record{Name: gateway.Name, Addresses: addresses})
		}
	}
	return records, nil
}
