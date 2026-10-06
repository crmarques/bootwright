// Package medialocal opens the operator-named sources `media add` imports.
package medialocal

import (
	"context"
	"crypto/tls"
	"errors"
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

// Files begins one acquisition's access to operator-named paths under the
// invoking account's credentials.
type Files interface {
	Begin(context.Context) (FileSession, error)
}

// FileSession opens one absolute path without following a link at its final
// component, and opens nothing but a regular file for reading.
type FileSession interface {
	OpenFile(string) (*os.File, error)
	Close() error
}

// deniedToRoot is what an opener's failure reports when the denied open ran
// with root's credentials.
type deniedToRoot interface{ DeniedToRoot() bool }

// Acquirer opens exactly one authorized media source. A download verifies TLS,
// refuses every redirect and carries no credential. The route is supplied by
// the caller; this adapter reads no proxy variable of its own. A file source
// is opened by files, never by this adapter, which proves the descriptor it
// receives.
type Acquirer struct {
	client *http.Client
	files  Files
}

func New(proxy func(*http.Request) (*url.URL, error), files Files) Acquirer {
	return Acquirer{files: files, client: &http.Client{
		// A media URL names exactly the bytes to import. Following a redirect
		// would import bytes from an endpoint the operator never authorized.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return failure("the media endpoint answered with a redirect", "", "name the exact image URL")
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
		return a.openFile(ctx, source.Path)
	}
	return a.openURL(ctx, source.URL)
}

func (a Acquirer) openFile(ctx context.Context, path string) (media.Acquisition, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return media.Acquisition{}, failure("the media source path cannot be resolved", path, "name a readable image file")
	}
	if a.files == nil {
		return media.Acquisition{}, failure("the media source opener is not configured", absolute, "")
	}
	file, err := a.receive(ctx, absolute)
	if err != nil {
		return media.Acquisition{}, err
	}
	if err := regularSource(file, absolute); err != nil {
		file.Close()
		return media.Acquisition{}, err
	}
	return media.Acquisition{Payload: file, Origin: "file://" + filepath.ToSlash(absolute)}, nil
}

// receive takes the source's descriptor from the invoking account's opener and
// ends that session; the descriptor outlives it.
func (a Acquirer) receive(ctx context.Context, absolute string) (*os.File, error) {
	session, err := a.files.Begin(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, failure("the media source cannot be opened under the invoking account", absolute, "")
	}
	file, err := session.OpenFile(absolute)
	session.Close()
	if err == nil {
		return file, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	var denied deniedToRoot
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil, failure("the media source does not exist", absolute, "name an existing image file")
	case errors.Is(err, os.ErrPermission) && errors.As(err, &denied) && denied.DeniedToRoot():
		return nil, failure("root cannot read the media source (a network home with root squash?)", absolute, "copy it to a local directory and name the copy")
	case errors.Is(err, os.ErrPermission):
		return nil, failure("the invoking account cannot read the media source (permission denied)", absolute, "give the invoking account read access to it, or copy it to a directory that account can read")
	}
	return nil, failure("the media source file cannot be opened", absolute, "name a readable image file")
}

// regularSource proves the received descriptor names a regular file, whatever
// the opener that issued it claims.
func regularSource(file *os.File, absolute string) error {
	info, err := file.Stat()
	if err != nil {
		return failure("the media source file cannot be opened", absolute, "name a readable image file")
	}
	mode := info.Mode()
	if mode.IsRegular() {
		return nil
	}
	if mode&os.ModeSymlink != 0 {
		return failure("the media source is a symbolic link", absolute, "name the image file itself rather than a link to it")
	}
	message := "the media source is not a regular file"
	switch {
	case mode&os.ModeNamedPipe != 0:
		message += " (a FIFO)"
	case mode&os.ModeCharDevice != 0:
		message += " (a character device)"
	case mode&os.ModeDevice != 0:
		message += " (a block device)"
	case mode.IsDir():
		message += " (a directory)"
	case mode&os.ModeSocket != 0:
		message += " (a socket)"
	}
	return failure(message, absolute, "name a regular image file")
}

func (a Acquirer) openURL(ctx context.Context, raw string) (media.Acquisition, error) {
	endpoint, err := url.Parse(raw)
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.Fragment != "" {
		return media.Acquisition{}, failure("the media URL is not an absolute credential-free endpoint", "", "name an http or https URL without user information")
	}
	if endpoint.Scheme != "http" && endpoint.Scheme != "https" {
		return media.Acquisition{}, failure("the media URL scheme is not supported", "", "name an http or https URL")
	}
	if a.client == nil {
		return media.Acquisition{}, failure("the media download client is not configured", "", "")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return media.Acquisition{}, failure("the media request could not be prepared", "", "")
	}
	response, err := a.client.Do(request)
	if err != nil {
		return media.Acquisition{}, failure("the media endpoint "+endpoint.Host+" could not be read", "", "verify the endpoint, its certificate and this host's route to it")
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return media.Acquisition{}, failure("the media endpoint "+endpoint.Host+" did not serve the image", "", "verify the image URL")
	}
	return media.Acquisition{Payload: response.Body, Origin: endpoint.String()}, nil
}

// failure names the absolute path of a file source, and nothing for a URL.
func failure(message, source, remediation string) error {
	return diagnostics.NewFailureWithRemediation("media.store", message, source, remediation)
}
