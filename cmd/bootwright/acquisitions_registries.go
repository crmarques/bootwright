package main

import (
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/cli"
)

type registryHost struct{ kind, host string }

var quayDeliveryHosts = []registryHost{
	{"registry blob host", "cdn.quay.io"}, {"registry blob host", "cdn01.quay.io"}, {"registry blob host", "cdn02.quay.io"}, {"registry blob host", "cdn03.quay.io"},
	{"registry blob host", "cdn04.quay.io"}, {"registry blob host", "cdn05.quay.io"}, {"registry blob host", "cdn06.quay.io"},
}

var registryCompanions = map[string][]registryHost{
	"docker.io": {{"registry endpoint", "registry-1.docker.io"}, {"registry token service", "auth.docker.io"}, {"registry blob host", "production.cloudfront.docker.com"}},
	"quay.io":   quayDeliveryHosts,
	"registry.access.redhat.com": append(slices.Clone(quayDeliveryHosts),
		registryHost{"registry signature store", "access.redhat.com"}),
	"registry.redhat.io": quayDeliveryHosts,
	"icr.io":             {{"registry blob host", "dd2.icr.io"}},
}

var imagePullKinds = []string{"service image", "emulated BMC image", "release", "mirror release", "storage image", "add-on catalog image", "default catalog image"}

func withRegistryHosts(rows []cli.Acquisition) []cli.Acquisition {
	type key struct{ kind, host string }
	companions := map[key]*cli.Acquisition{}
	consumers := map[key][]string{}
	var order []key
	for _, row := range rows {
		if !slices.Contains(imagePullKinds, row.Kind) {
			continue
		}
		registry, _, _ := strings.Cut(strings.TrimPrefix(row.Origin, "https://"), "/")
		for _, companion := range registryCompanions[registry] {
			name := key{companion.kind, companion.host}
			existing, seen := companions[name]
			if !seen {
				existing = &cli.Acquisition{Kind: companion.kind, Artifact: companion.host, Origin: companion.host, Class: originClass("https://" + companion.host)}
				companions[name] = existing
				order = append(order, name)
			}
			existing.Machines = append(existing.Machines, row.Machines...)
			consumers[name] = append(consumers[name], row.Consumer)
		}
	}
	for _, name := range order {
		slices.Sort(consumers[name])
		companions[name].Consumer = strings.Join(slices.Compact(consumers[name]), ", ")
		rows = append(rows, *companions[name])
	}
	return rows
}
