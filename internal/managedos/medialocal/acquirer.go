// Package medialocal opens the operator-named sources `media add` imports.
package medialocal

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
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

// Acquirer opens exactly one authorized media source. A download is HTTPS
// only, verifies TLS, refuses every redirect and carries no credential. The
// route is supplied by the caller; this adapter reads no proxy variable of its
// own. A file source is opened by files, never by this adapter, which proves
// the descriptor it receives.
type Acquirer struct {
	client *http.Client
	proxy  func(*http.Request) (*url.URL, error)
	files  Files
}

func New(proxy func(*http.Request) (*url.URL, error), files Files) Acquirer {
	return Acquirer{files: files, proxy: proxy, client: &http.Client{
		// A media URL names exactly the bytes to import. Following a redirect
		// would import bytes from an endpoint the operator never authorized.
		CheckRedirect: refuseRedirect,
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

// Origin reports the credential-free origin a record carries for a source: a
// file's absolute path as a file:// URL, or a URL's scheme, host and path
// without its query, which may carry a signature. It opens nothing.
func (a Acquirer) Origin(source media.Source) (string, error) {
	if source.Path != "" {
		absolute, err := filepath.Abs(source.Path)
		if err != nil {
			return "", failure("the media source path cannot be resolved", source.Path, "name a readable image file")
		}
		return "file://" + filepath.ToSlash(absolute), nil
	}
	endpoint, err := mediaURL(source.URL)
	if err != nil {
		return "", err
	}
	return withoutQuery(endpoint), nil
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
	return media.Acquisition{Payload: file}, nil
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

// mediaURL admits one absolute HTTPS URL with a host and no userinfo or
// fragment.
func mediaURL(raw string) (*url.URL, error) {
	endpoint, err := url.Parse(raw)
	if err != nil || endpoint.Opaque != "" || endpoint.Hostname() == "" || endpoint.User != nil || endpoint.Fragment != "" {
		return nil, failure("the media URL is not an absolute credential-free endpoint", "", "name the image's https:// URL without user information or a fragment")
	}
	if endpoint.Scheme != "https" {
		return nil, failure("the media URL is not HTTPS", "", "name the image's https:// URL")
	}
	return endpoint, nil
}

// withoutQuery is a URL's scheme, host and escaped path, so neither its query
// nor its userinfo reaches a record or a diagnostic.
func withoutQuery(endpoint *url.URL) string {
	return (&url.URL{Scheme: endpoint.Scheme, Host: endpoint.Host, Path: endpoint.Path, RawPath: endpoint.RawPath}).String()
}

// openURL requests the image under its transfer deadline. Every failure ends
// that deadline's context, and a served image's Close does.
func (a Acquirer) openURL(ctx context.Context, raw string) (media.Acquisition, error) {
	endpoint, err := mediaURL(raw)
	if err != nil {
		return media.Acquisition{}, err
	}
	if a.client == nil {
		return media.Acquisition{}, failure("the media download client is not configured", "", "")
	}
	transfer, cancel := context.WithTimeout(ctx, transferTimeout)
	request, err := http.NewRequestWithContext(transfer, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		cancel()
		return media.Acquisition{}, failure("the media request could not be prepared", "", "")
	}
	via, err := a.route(request)
	if err != nil {
		cancel()
		return media.Acquisition{}, err
	}
	response, err := a.client.Do(request)
	if err != nil {
		failed := transportFailure(ctx, transfer, endpoint.Host, via, err)
		cancel()
		return media.Acquisition{}, failed
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		cancel()
		return media.Acquisition{}, statusFailure(endpoint.Host, response.StatusCode)
	}
	return media.Acquisition{Payload: &download{body: response.Body, parent: ctx, transfer: transfer, cancel: cancel, host: endpoint.Host}}, nil
}

// route reports the proxy the supplied route selects for a request, or nil
// for a direct download.
func (a Acquirer) route(request *http.Request) (*url.URL, error) {
	if a.proxy == nil {
		return nil, nil
	}
	via, err := a.proxy(request)
	if err != nil {
		return nil, failure("the download route this host selected cannot be used", "",
			"correct the HTTPS_PROXY and NO_PROXY values this command was invoked with, then repeat it")
	}
	return via, nil
}

// refuseRedirect names the redirect a media endpoint answered: its status and
// its target without the query, which may carry a signature. The endpoint
// chose the target, so only its first MaxMediaOrigin bytes are named.
func refuseRedirect(request *http.Request, via []*http.Request) error {
	host, status := "", "a redirect"
	if len(via) != 0 {
		host = via[0].URL.Host
	}
	if request.Response != nil {
		status = "HTTP " + strconv.Itoa(request.Response.StatusCode)
	}
	target := withoutQuery(request.URL)
	if len(target) > media.MaxMediaOrigin {
		target = target[:media.MaxMediaOrigin] + "..."
	}
	return failure("the media endpoint "+host+" answered "+status+" redirecting to "+target+
		", and a media download follows no redirect", "", "name the exact image URL")
}

// transportFailure names the one condition a failed request met. The
// transport's own text is never published: it carries the full URL and its
// query.
func transportFailure(parent, transfer context.Context, host string, via *url.URL, err error) error {
	if parent.Err() != nil {
		return parent.Err()
	}
	var wrapped *url.Error
	if errors.As(err, &wrapped) && len(diagnostics.Of(wrapped.Err)) != 0 {
		return wrapped.Err
	}
	if errors.Is(transfer.Err(), context.DeadlineExceeded) {
		return deadlineFailure(host)
	}
	subject, route := "the media endpoint "+host, "directly"
	remedy := "verify that this host reaches " + host + " over HTTPS, or route the download through a proxy with HTTPS_PROXY"
	if via != nil {
		route = "through the proxy " + via.Host
		remedy = "verify that the proxy " + via.Host + " admits this host and reaches " + host + " over HTTPS"
	}
	var resolution *net.DNSError
	if errors.As(err, &resolution) {
		condition := "could not be resolved by this host's resolver"
		if resolution.IsTimeout {
			condition = "was not resolved before this host's resolver timed out"
		}
		remedy = "make " + resolution.Name + " resolvable from this host, or route the download through a proxy with HTTPS_PROXY"
		if via != nil {
			remedy = "make " + resolution.Name + " resolvable from this host, or name a proxy in HTTPS_PROXY that this host resolves"
		}
		return failure(subject+" could not be reached: the name "+resolution.Name+" "+condition, "", remedy)
	}
	if reason, refused := untrusted(err); refused {
		if via != nil && via.Scheme == "https" {
			subject += " or the proxy " + via.Host
		}
		return failure(subject+" presented a certificate this host's system trust store does not accept"+reason, "",
			"install the issuing certificate authority in this host's system trust store, or name an endpoint whose certificate this host trusts")
	}
	if timedOut(err) {
		return failure(subject+", reached "+route+", did not answer within its "+strconv.Itoa(int(dialTimeout/time.Second))+
			"-second connection timeout and "+strconv.Itoa(int(responseTimeout/time.Second))+"-second response timeout", "", remedy)
	}
	return failure(subject+" could not be reached from this host "+route, "", remedy)
}

// untrusted reports whether this host's trust store refused the certificate
// presented, and why when the verifier says.
func untrusted(err error) (string, bool) {
	var authority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	var verification *tls.CertificateVerificationError
	switch {
	case errors.As(err, &authority):
		return " (signed by an unknown authority)", true
	case errors.As(err, &hostname):
		return " (not valid for the host)", true
	case errors.As(err, &invalid) && invalid.Reason == x509.Expired:
		return " (expired or not yet valid)", true
	case errors.As(err, &invalid), errors.As(err, &verification):
		return "", true
	}
	return "", false
}

// timedOut reports whether any error in the chain is a timeout: a wrapper may
// report none for a timeout it wraps further down.
func timedOut(err error) bool {
	for ; err != nil; err = errors.Unwrap(err) {
		if expired, ok := err.(interface{ Timeout() bool }); ok && expired.Timeout() {
			return true
		}
	}
	return false
}

func statusFailure(host string, code int) error {
	status := strconv.Itoa(code)
	if text := http.StatusText(code); text != "" {
		status += " " + text
	}
	remedy := "name a URL that answers 200 with the image"
	switch {
	case code == http.StatusNotFound || code == http.StatusGone:
		remedy = "verify the image URL"
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		remedy = "name a URL that serves the image without credentials, such as a pre-signed one"
	case code == http.StatusProxyAuthRequired:
		remedy = "media downloads never authenticate to a proxy, so route through one that admits this host without credentials"
	case code >= 500 && code <= 599:
		remedy = "repeat the command later, or name the image on another mirror"
	}
	return failure("the media endpoint "+host+" answered HTTP "+status+", so it served no image", "", remedy)
}

func deadlineFailure(host string) error {
	return failure("the download from "+host+" did not finish within its "+strconv.Itoa(int(transferTimeout/time.Hour))+"-hour transfer deadline", "",
		"copy the image locally and add it with --from-file, or repeat the command over a faster route")
}

// download is one served image. It keeps the caller's context and the
// transfer's own until Close, so a read the transfer deadline ended names that
// deadline rather than the source, and Close ends the transfer.
type download struct {
	body     media.Payload
	parent   context.Context
	transfer context.Context
	cancel   context.CancelFunc
	host     string
}

func (d *download) Read(buffer []byte) (int, error) {
	n, err := d.body.Read(buffer)
	if err != nil && errors.Is(d.transfer.Err(), context.DeadlineExceeded) && d.parent.Err() == nil {
		return n, deadlineFailure(d.host)
	}
	return n, err
}

func (d *download) Close() error {
	err := d.body.Close()
	d.cancel()
	return err
}

// failure names the absolute path of a file source, and nothing for a URL.
func failure(message, source, remediation string) error {
	return diagnostics.NewFailureWithRemediation("media.store", message, source, remediation)
}
