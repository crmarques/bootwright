package proxy

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
)

// Implementation is the frozen identity of this capability. A plan records it,
// and a continuation refuses when the executable no longer offers it.
const Implementation = "proxy-squid-v1"

// requestVersion is the frozen request shape this build writes and reads.
const requestVersion = "proxy-squid-v2"

// Kind is the API kind this capability realizes.
const Kind = string(api.Proxy)

// defaultImage is the compiled proxy image, pinned by content digest. An
// authored spec.image replaces it; neither may use a floating tag.
const defaultImage = "docker.io/ubuntu/squid@sha256:6a097f68bae708cedbabd6188d68c7e2e7a38cedd05a176e1cc0ba29e3bbe029"

// Definition binds the shared managed-service behavior to squid. The clients
// it answers are derived from the selected graph, never authored, so a proxy
// can never be planned open to the world.
func Definition() managedservice.Definition {
	return managedservice.Definition{
		Kind:           api.Proxy,
		Implementation: Implementation,
		Version:        requestVersion,
		Slug:           "proxy",
		Variable:       "bootwright_proxy",
		Purpose:        "proxy egress",
		Subject:        "proxy",
		Image:          defaultImage,
		Extend: func(catalog api.Catalog, _ api.Value, request *managedservice.Request) error {
			request.Clients = managedservice.Clients(catalog)
			return nil
		},
	}
}
