package main

import (
	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/managedos/media"
	"github.com/crmarques/bootwright/internal/managedos/medialocal"
)

// mediaDependencies names the capabilities the host-wide media store consumes.
// A nil store leaves the commands unavailable rather than substituting one.
type mediaDependencies struct {
	Store     media.Store
	Acquirer  media.Acquirer
	Confirmer media.Confirmer
}

func wireMedia(deps mediaDependencies) cli.MediaService {
	if deps.Store == nil || deps.Acquirer == nil {
		return media.Service{}
	}
	return media.New(deps.Store, deps.Acquirer, deps.Confirmer, systemClock{})
}

func localMediaDependencies(store media.Store, confirmer media.Confirmer) mediaDependencies {
	return mediaDependencies{Store: store, Acquirer: medialocal.New(), Confirmer: confirmer}
}
