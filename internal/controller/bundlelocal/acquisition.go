package bundlelocal

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/crmarques/bootwright/internal/acquisition"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// acquisitionClient shares the fixed explicit-route transport with the
// publisher metadata resolvers. Each consumer supplies its narrower origin
// boundary and separately enforces response bytes and content integrity.
func acquisitionClient(ctx context.Context, egress prerequisites.SetupEgress, approved func(*url.URL) bool, timeout time.Duration) (*http.Client, func(), error) {
	if approved == nil || timeout <= 0 || timeout > 5*time.Minute {
		return nil, nil, bundleFailure("dependency acquisition transport policy is invalid")
	}
	proxy, err := explicitProxy(egress)
	if err != nil {
		return nil, nil, err
	}
	roots, err := qualifiedSystemRoots(ctx)
	if err != nil {
		return nil, nil, err
	}
	transport := &http.Transport{
		Proxy: proxy, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots},
		DialContext:         (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout: 15 * time.Second, ResponseHeaderTimeout: 30 * time.Second,
		MaxResponseHeaderBytes: 64 << 10, DisableCompression: true,
	}
	client := &http.Client{
		Transport: acquisition.GuardAcquisitionTransport(transport, approved), Timeout: timeout,
		CheckRedirect: func(request *http.Request, previous []*http.Request) error {
			if len(previous) >= 5 || !approved(request.URL) {
				return bundleFailure("dependency acquisition redirected outside its approved HTTPS boundary")
			}
			return nil
		},
	}
	return client, transport.CloseIdleConnections, nil
}

func transportFailure(subject, endpoint string, err error) error {
	var scoped *prerequisites.ScopedFailure
	if errors.As(err, &scoped) {
		return scoped
	}
	var wrapped *url.Error
	if errors.As(err, &wrapped) && len(diagnostics.Of(wrapped.Err)) != 0 {
		return wrapped.Err
	}
	var classified *diagnostics.Failure
	if errors.As(err, &classified) {
		return classified
	}
	failure := acquisition.TransportFailure(subject, endpoint, err)
	return &prerequisites.ScopedFailure{Message: failure.Message(), Correction: failure.Correction(), Routable: failure.Routable()}
}

func bundleFailure(message string) error {
	return &prerequisites.ScopedFailure{Message: message, Correction: "Restore approved dependency sources or the retained tool areas"}
}

// Proxy selection is request-local: the route reaches this transport as request
// data, and the process environment is never consulted here.
func explicitProxy(egress prerequisites.SetupEgress) (func(*http.Request) (*url.URL, error), error) {
	selector, err := controller.NewProxySelector(egress.HTTPProxy, egress.HTTPSProxy, egress.NoProxy)
	switch {
	case errors.Is(err, controller.ErrProxyBypass):
		return nil, bundleFailure("proxy bypass entry is outside the qualified host, IP or CIDR grammar")
	case errors.Is(err, controller.ErrProxyScheme):
		return nil, bundleFailure("HTTPS dependency acquisition requires an explicit HTTPS proxy endpoint")
	case err != nil:
		return nil, bundleFailure("explicit HTTPS acquisition proxy is invalid")
	}
	return func(request *http.Request) (*url.URL, error) { return selector(request.URL), nil }, nil
}
