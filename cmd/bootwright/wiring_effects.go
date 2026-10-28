package main

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	"github.com/crmarques/bootwright/internal/containercluster"
	"github.com/crmarques/bootwright/internal/desiredstate/customplaybooks"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/managedos"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/storage"
	"github.com/crmarques/bootwright/internal/substrate"
)

func effectBearing(object api.Object) (lifecycle.Bearing, bool) {
	if reason, bears := infrastructureservices.EffectBearing(object); bears {
		return lifecycle.Bearing{Reason: reason, Remedy: infrastructureservices.UnclaimedRemedy(object), Shared: true, Machine: infrastructureservices.Placement(object)}, true
	}
	for _, predicate := range domainEffects() {
		if reason, bears := predicate(object); bears {
			return lifecycle.Bearing{Reason: reason}, true
		}
	}
	return lifecycle.Bearing{}, false
}

func domainEffects() []func(api.Object) (string, bool) {
	return []func(api.Object) (string, bool){
		substrate.EffectBearing, managedos.EffectBearing, containercluster.EffectBearing,
		storage.EffectBearing, addons.EffectBearing, customplaybooks.EffectBearing,
	}
}
