package proxy

import (
	"cmp"
	"net/netip"
	neturl "net/url"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
)

func destinations(catalog api.Catalog, declared api.Value) ([]managedservice.Destination, error) {
	if !declared.Present() {
		return nil, nil
	}
	if declared.Len() == 0 {
		return nil, managedservice.Refusal("api.value", "an empty allowedDestinations would freeze as a proxy that admits every destination", "declare at least one spec.allowedDestinations entry, or remove the field")
	}
	entries := append(declared.Strings(), mirrorHosts(catalog)...)
	found := make([]managedservice.Destination, 0, len(entries))
	for index, entry := range entries {
		host, port, ok := api.ProxyDestination(entry)
		if !ok && index >= declared.Len() {
			return nil, managedservice.Refusal("api.value", "a mirror host the Environment declares is outside the allowed proxy destination grammar", "name the Environment's spec.mirror hosts by a lowercase DNS name or an IP address without a zone")
		}
		if !ok {
			return nil, managedservice.Refusal("api.value", "an allowed proxy destination is outside its grammar", "write each spec.allowedDestinations entry as "+api.ProxyDestinationForms)
		}
		destination := managedservice.Destination{Domain: host, Port: port}
		if _, err := netip.ParseAddr(host); err == nil {
			destination = managedservice.Destination{Address: host, Port: port}
		}
		found = append(found, destination)
	}
	slices.SortFunc(found, func(x, y managedservice.Destination) int {
		return cmp.Or(cmp.Compare(x.Address, y.Address), cmp.Compare(x.Domain, y.Domain), cmp.Compare(x.Port, y.Port))
	})
	return slices.Compact(found), nil
}

func mirrorHosts(catalog api.Catalog) []string {
	var hosts []string
	if environments := catalog.OfKind(api.Environment); len(environments) == 1 {
		if location := environments[0].Spec().Get("mirror", "artifacts", "url").Text(); location != "" {
			hosts = append(hosts, httpsHost(location))
		}
	}
	if registry, found := managedservice.ImageMirror(catalog); found {
		if location := registry.Spec().Get("url").Text(); location != "" {
			hosts = append(hosts, httpsHost("https://"+location))
		}
	}
	return slices.DeleteFunc(hosts, func(host string) bool { return host == "" })
}

func httpsHost(location string) string {
	parsed, err := neturl.Parse(location)
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	port := parsed.Port()
	if port == "" {
		port = "443"
	}
	if strings.Contains(parsed.Hostname(), ":") {
		return "[" + parsed.Hostname() + "]:" + port
	}
	return parsed.Hostname() + ":" + port
}
