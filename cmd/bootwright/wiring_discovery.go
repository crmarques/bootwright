package main

import (
	"crypto/rand"
	"encoding/hex"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/clients"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/managedos/diskdiscovery"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate/baremetal"
)

func discoveryNonce() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}

func discoveryPreparations(resolver lifecycle.CapabilityResolver) []lifecycle.PreparationDefinition {
	bound, found := resolver.Resolve(installation.Kind, diskdiscovery.DiscoveryImplementation)
	owner, ok := bound.(diskdiscovery.Capability)
	if !found || !ok {
		return nil
	}
	definition := lifecycle.PreparationDefinition{Automatic: owner.AutomaticPreparation, Kind: diskdiscovery.DiscoveryPreparation, Command: "machine discover", Validate: owner.ValidatePreparation, Advance: owner.AdvancePreparation,
		Bearing: func(api.Object) (lifecycle.Bearing, bool) { return lifecycle.Bearing{}, false }}
	for _, binding := range resolver.Bindings() {
		switch binding.Implementation {
		case baremetal.Implementation, diskdiscovery.DiscoveryImplementation:
			definition.Bindings = append(definition.Bindings, binding)
		case clients.Implementation, artifactserver.Implementation:
			definition.Prerequisites = append(definition.Prerequisites, binding)
		default:
			if binding.Family == managedservice.Family {
				definition.Prerequisites = append(definition.Prerequisites, binding)
			}
		}
	}
	return []lifecycle.PreparationDefinition{definition}
}
