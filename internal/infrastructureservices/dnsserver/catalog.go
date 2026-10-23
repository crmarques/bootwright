package dnsserver

import (
	"fmt"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/storage"
)

const Implementation = "dns-server-dnsmasq-v1"

const Kind = string(api.DNSServer)

const defaultImage = "docker.io/4km3/dnsmasq@sha256:52e25fb2601156ab66f6a0872c180b285df7cafaa41267d8d65689f066490641"

// Definition binds the shared managed-service behavior to dnsmasq. The records
// it answers are derived from the retained Machines and the container clusters
// they install, plus attached export gateways. Forwarders are authored; with none declared
// the resolver answers only its own records rather than silently reaching an
// ambient upstream.
func Definition() managedservice.Definition {
	return managedservice.Definition{
		Kind:           api.DNSServer,
		Implementation: Implementation,
		Version:        managedservice.RequestVersion,
		Slug:           "dns",
		Variable:       managedservice.Variable,
		Purpose:        "resolve names",
		Subject:        "DNS server",
		Image:          defaultImage,
		Extend: func(catalog api.Catalog, spec api.Value, request *managedservice.Request) error {
			request.Records = append(managedservice.MachineRecords(catalog), managedservice.ClusterRecords(catalog)...)
			records, err := exportRecords(catalog, request.Records)
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

func exportRecords(catalog api.Catalog, records []managedservice.Record) ([]managedservice.Record, error) {
	for _, export := range catalog.OfKind(api.StorageExport) {
		ref := export.Spec().Get("dataFoundation", "objectGatewayRef").Text()
		if ref == "" {
			continue
		}
		gateway, found := catalog.Find(api.StorageObjectGateway, ref)
		if !found {
			return nil, fmt.Errorf("%s has no retained gateway", export.Identity())
		}
		name := gateway.Spec().Get("endpoint", "fqdn").Text()
		if name == "" {
			return nil, fmt.Errorf("%s has no gateway endpoint FQDN", export.Identity())
		}
		addresses, err := storage.GatewayDNSAddresses(catalog, gateway)
		if err != nil {
			return nil, err
		}
		duplicate := false
		for _, record := range records {
			if record.Name != name {
				continue
			}
			existing := slices.Clone(record.Addresses)
			slices.Sort(existing)
			if record.Subtree || !slices.Equal(existing, addresses) {
				return nil, fmt.Errorf("gateway DNS record %s conflicts with another retained record", name)
			}
			duplicate = true
		}
		if !duplicate {
			records = append(records, managedservice.Record{Name: name, Addresses: addresses})
		}
	}
	return records, nil
}
