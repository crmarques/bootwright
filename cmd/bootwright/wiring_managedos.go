package main

import (
	"context"
	"net/http"
	"net/url"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/controller"
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

// localMediaDependencies passes no opener at all when files is nil, so a file
// source refuses instead of reaching a wrapper around nothing.
func localMediaDependencies(store media.Store, confirmer media.Confirmer, route controller.Route, files operatorFiles) mediaDependencies {
	var sources medialocal.Files
	if files != nil {
		sources = mediaFiles{files: files}
	}
	return mediaDependencies{Store: store, Acquirer: medialocal.New(mediaRoute(route), sources), Confirmer: confirmer}
}

// mediaFiles hands media acquisition the operator's file port.
type mediaFiles struct{ files operatorFiles }

func (m mediaFiles) Begin(ctx context.Context) (medialocal.FileSession, error) {
	session, err := m.files.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return session, nil
}

// mediaRoute keeps a direct import on the transport that has no proxy at all,
// and fails an import closed when the selected route cannot be honored.
func mediaRoute(route controller.Route) func(*http.Request) (*url.URL, error) {
	if !route.Configured() || route.Direct() {
		return nil
	}
	selector, err := route.Selector()
	if err != nil {
		return func(*http.Request) (*url.URL, error) { return nil, err }
	}
	return func(request *http.Request) (*url.URL, error) { return selector(request.URL), nil }
}
