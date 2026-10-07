package ntpserver

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
)

const Implementation = "ntp-server-chrony-v1"

const Kind = string(api.NTPServer)

const defaultImage = "docker.io/dockurr/chrony@sha256:f584829f268b26b26b351000b60aa7c3507933873b6a6d77ffe9dcabcfdc7025"

// Definition binds the shared managed-service behavior to chrony. The daemon
// serves time without disciplining the host clock, so it coexists with the
// controller's own time service instead of competing with it.
func Definition() managedservice.Definition {
	return managedservice.Definition{
		Kind:           api.NTPServer,
		Implementation: Implementation,
		Version:        managedservice.RequestVersion,
		Slug:           "ntp",
		Variable:       managedservice.Variable,
		Purpose:        "serve time",
		Subject:        "NTP server",
		Image:          defaultImage,
		Extend: func(catalog api.Catalog, spec api.Value, request *managedservice.Request) error {
			request.Clients = managedservice.Clients(catalog)
			request.Sources = spec.Get("upstreamSources").Strings()
			return nil
		},
	}
}
