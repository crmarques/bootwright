package bundlelocal

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

type sourceFetcher func(context.Context, prerequisites.DependencySource, prerequisites.SetupEgress) ([]byte, error)

func fetchSource(ctx context.Context, source prerequisites.DependencySource, egress prerequisites.SetupEgress) ([]byte, error) {
	if source.Bytes <= 0 || source.Bytes > maxMemberBytes {
		return nil, bundleFailure("dependency source exceeds its qualified acquisition limit")
	}
	origin, err := url.Parse(source.URL)
	if err != nil || !approvedOrigin(origin) {
		return nil, bundleFailure("dependency source is outside the approved HTTPS origins")
	}
	client, closeClient, err := acquisitionClient(ctx, egress, approvedOrigin, 5*time.Minute)
	if err != nil {
		return nil, err
	}
	defer closeClient()
	return receiveSource(ctx, source, client.Do)
}

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
		Transport: transport, Timeout: timeout,
		CheckRedirect: func(request *http.Request, previous []*http.Request) error {
			if len(previous) >= 5 || !approved(request.URL) {
				return bundleFailure("dependency acquisition redirected outside its approved HTTPS boundary")
			}
			return nil
		},
	}
	return client, transport.CloseIdleConnections, nil
}

func receiveSource(ctx context.Context, source prerequisites.DependencySource, send func(*http.Request) (*http.Response, error)) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URL, nil)
	if err != nil {
		return nil, bundleFailure("dependency acquisition request is invalid")
	}
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("User-Agent", "Bootwright-Controller/1")
	response, err := send(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, transportFailure("approved dependency source", source.URL, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.ContentLength > source.Bytes || response.Header.Get("Content-Encoding") != "" {
		return nil, bundleFailure("dependency acquisition returned an unapproved status or representation")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, source.Bytes+1))
	if err != nil || !approvedBytes(source, data) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, bundleFailure("acquired dependency does not match its approved size and SHA-256")
	}
	return data, nil
}

// transportFailure publishes only the endpoint host and one fixed condition:
// transport, library and operating-system text is never a public result. Its
// remedy is rendered by the scope that met it, which alone knows how its
// operator selects a route and which command settles it.
func transportFailure(subject, endpoint string, err error) error {
	// A refusal the transport only wrapped, such as a redirect outside the
	// approved boundary, keeps its own error and so the scope that renders it;
	// only the wrapper that carries the request URL is dropped.
	var wrapped *url.Error
	if errors.As(err, &wrapped) && len(diagnostics.Of(wrapped.Err)) != 0 {
		return wrapped.Err
	}
	var classified *diagnostics.Failure
	if errors.As(err, &classified) {
		return classified
	}
	host := "its approved publisher"
	if parsed, parseErr := url.Parse(endpoint); parseErr == nil && parsed.Hostname() != "" {
		host = parsed.Hostname()
	}
	failure := &prerequisites.ScopedFailure{Correction: "Restore this host's access to " + host, Routable: true}
	condition := "could not be reached from this host"
	var resolution *net.DNSError
	var verification *tls.CertificateVerificationError
	var expired interface{ Timeout() bool }
	switch {
	case errors.As(err, &resolution):
		condition = "could not be resolved by this host's configured resolver"
		if resolution.IsTimeout {
			condition = "was not resolved before this host's resolver timed out"
		}
		failure.Correction = "Make " + host + " resolvable"
	case errors.As(err, &verification), errors.As(err, new(x509.UnknownAuthorityError)):
		condition = "presented a certificate the qualified system trust store does not accept"
		failure.Correction, failure.Routable = "Install the required certificate authority in the system trust store", false
	case errors.As(err, &expired) && expired.Timeout():
		condition = "did not answer within its bounded acquisition timeout"
	}
	failure.Message = subject + " " + host + " " + condition
	return failure
}

func approvedOrigin(value *url.URL) bool {
	if value == nil || value.Scheme != "https" || value.User != nil || value.Fragment != "" || value.Opaque != "" || value.Port() != "" && value.Port() != "443" {
		return false
	}
	switch value.Hostname() {
	case "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com", "files.pythonhosted.org", "cdn-ubi.redhat.com", "dl.fedoraproject.org":
		return true
	}
	return false
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
