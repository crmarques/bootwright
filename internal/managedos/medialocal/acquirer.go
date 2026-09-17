// Package medialocal opens the operator-named sources `media add` imports.
package medialocal

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/managedos/media"
)

const (
	dialTimeout     = 30 * time.Second
	responseTimeout = 60 * time.Second
	transferTimeout = 6 * time.Hour
)

// Acquirer opens exactly one authorized media source. A download verifies TLS,
// refuses every redirect and carries no credential. The route is supplied by
// the caller; this adapter reads no proxy variable of its own.
type Acquirer struct{ client *http.Client }

func New(proxy func(*http.Request) (*url.URL, error)) Acquirer {
	return Acquirer{client: &http.Client{
		// A media URL names exactly the bytes to import. Following a redirect
		// would import bytes from an endpoint the operator never authorized.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return failure("the media endpoint answered with a redirect", "name the exact image URL")
		},
		Timeout: transferTimeout,
		Transport: &http.Transport{
			Proxy:                 proxy,
			DialContext:           (&net.Dialer{Timeout: dialTimeout}).DialContext,
			TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
			TLSHandshakeTimeout:   dialTimeout,
			ResponseHeaderTimeout: responseTimeout,
			DisableCompression:    true,
		},
	}}
}

func (a Acquirer) Open(ctx context.Context, source media.Source) (media.Acquisition, error) {
	if err := ctx.Err(); err != nil {
		return media.Acquisition{}, err
	}
	if source.Path != "" {
		return openFile(source.Path)
	}
	return a.openURL(ctx, source.URL)
}

func openFile(path string) (media.Acquisition, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return media.Acquisition{}, failure("the media source path cannot be resolved", "name a readable image file")
	}
	file, err := os.Open(absolute)
	if err != nil {
		return media.Acquisition{}, failure("the media source file cannot be opened", "name a readable image file")
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return media.Acquisition{}, failure("the media source is not a regular file", "name a readable image file")
	}
	return media.Acquisition{Payload: file, Origin: "file://" + filepath.ToSlash(absolute)}, nil
}

func (a Acquirer) openURL(ctx context.Context, raw string) (media.Acquisition, error) {
	endpoint, err := url.Parse(raw)
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.Fragment != "" {
		return media.Acquisition{}, failure("the media URL is not an absolute credential-free endpoint", "name an http or https URL without user information")
	}
	if endpoint.Scheme != "http" && endpoint.Scheme != "https" {
		return media.Acquisition{}, failure("the media URL scheme is not supported", "name an http or https URL")
	}
	if a.client == nil {
		return media.Acquisition{}, failure("the media download client is not configured", "")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return media.Acquisition{}, failure("the media request could not be prepared", "")
	}
	response, err := a.client.Do(request)
	if err != nil {
		return media.Acquisition{}, failure("the media endpoint "+endpoint.Host+" could not be read", "verify the endpoint, its certificate and this host's route to it")
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return media.Acquisition{}, failure("the media endpoint "+endpoint.Host+" did not serve the image", "verify the image URL")
	}
	return media.Acquisition{Payload: response.Body, Origin: endpoint.String()}, nil
}

func failure(message, remediation string) error {
	return diagnostics.NewFailureWithRemediation("media.store", message, "", remediation)
}
